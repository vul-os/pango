package repo

// Job events: the append-only thread on a job.
//
// One thread serves internal notes and tenant communication, gated by
// visibility (§4.3). Two separate threads would mean a manager has to read both
// to know what happened, and would eventually mean somebody replies to a tenant
// in the internal one.
//
// There is no UpdateEvent and no DeleteEvent, and that is not an omission. The
// thread is the record of what was said to a tenant and when; an editable
// thread is not a record.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const eventCols = `id, org_id, job_id, kind, body, actor_party_id, visibility, hlc, created_at`

// AddEvent appends an event to a job the org owns.
func (r *Repo) AddEvent(orgID string, e domain.JobEvent) (domain.JobEvent, error) {
	if _, err := r.GetJob(orgID, e.JobID); err != nil {
		return domain.JobEvent{}, err
	}
	e.OrgID = orgID
	if e.ID == "" {
		e.ID = store.NewID()
	}
	if e.Visibility == "" {
		e.Visibility = domain.VisibilityInternal // safe default: never leak by omission
	}
	if e.Kind == "" {
		e.Kind = "note"
	}
	if e.CreatedAt == "" {
		e.CreatedAt = store.Now()
	}
	if err := e.Validate(); err != nil {
		return domain.JobEvent{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error { return insertEvent(tx, r.s, e) })
	if err != nil {
		return domain.JobEvent{}, err
	}
	return e, nil
}

// insertEvent writes an event inside an existing transaction. Shared with
// SetJobStatus so a status change and its event commit atomically.
func insertEvent(tx *sql.Tx, s *store.Store, e domain.JobEvent) error {
	hlc, err := s.Journal(tx, e.OrgID, "job_event", e.ID, e, false)
	if err != nil {
		return err
	}
	e.HLC = hlc
	_, err = tx.Exec(
		`INSERT INTO job_event (`+eventCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.OrgID, e.JobID, e.Kind, e.Body, nullable(e.ActorID), e.Visibility, e.HLC, e.CreatedAt)
	return err
}

// ListEvents returns a job's thread oldest-first. publicOnly restricts it to
// the tenant-visible subset (§4.3).
func (r *Repo) ListEvents(orgID, jobID string, publicOnly bool) ([]domain.JobEvent, error) {
	if _, err := r.GetJob(orgID, jobID); err != nil {
		return nil, err
	}
	q := `SELECT ` + eventCols + ` FROM job_event WHERE org_id = ? AND job_id = ?`
	args := []any{orgID, jobID}
	if publicOnly {
		q += " AND visibility = ?"
		args = append(args, domain.VisibilityPublic)
	}
	q += " ORDER BY created_at ASC, id ASC"

	rows, err := r.s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.JobEvent{}
	for rows.Next() {
		var e domain.JobEvent
		var actor sql.NullString
		if err := rows.Scan(&e.ID, &e.OrgID, &e.JobID, &e.Kind, &e.Body, &actor,
			&e.Visibility, &e.HLC, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		e.ActorID = nullStr(actor)
		out = append(out, e)
	}
	return out, rows.Err()
}
