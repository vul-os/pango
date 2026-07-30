package repo

// Time entries: immutable, insert-only, whole minutes (§6).
//
// Same reasoning as cost.go — labour recorded on two partitioned devices must
// add, not overwrite. Minutes rather than hours because hours invite a float,
// and "1.75 hours" carries the same representation problem as "R17.50" with the
// same consequence: an invoice somebody has to defend.
//
// A correction is a negative entry.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const timeCols = `id, org_id, job_id, minutes, note, party_id, hlc, created_at`

// AddTime appends a time entry to a job the org owns.
func (r *Repo) AddTime(orgID string, t domain.TimeEntry) (domain.TimeEntry, error) {
	if _, err := r.GetJob(orgID, t.JobID); err != nil {
		return domain.TimeEntry{}, err
	}
	t.OrgID = orgID
	if t.ID == "" {
		t.ID = store.NewID()
	}
	if t.CreatedAt == "" {
		t.CreatedAt = store.Now()
	}
	if err := t.Validate(); err != nil {
		return domain.TimeEntry{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "time_entry", t.ID, t, false)
		if err != nil {
			return err
		}
		t.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO time_entry (`+timeCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.OrgID, t.JobID, t.Minutes, t.Note, nullable(t.PartyID), t.HLC, t.CreatedAt)
		return err
	})
	if err != nil {
		return domain.TimeEntry{}, err
	}
	return t, nil
}

// ListTime returns a job's time entries oldest-first.
func (r *Repo) ListTime(orgID, jobID string) ([]domain.TimeEntry, error) {
	if _, err := r.GetJob(orgID, jobID); err != nil {
		return nil, err
	}
	rows, err := r.s.DB().Query(
		`SELECT `+timeCols+` FROM time_entry WHERE org_id = ? AND job_id = ?
		 ORDER BY created_at ASC, id ASC`, orgID, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TimeEntry{}
	for rows.Next() {
		var t domain.TimeEntry
		var party sql.NullString
		if err := rows.Scan(&t.ID, &t.OrgID, &t.JobID, &t.Minutes, &t.Note, &party,
			&t.HLC, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan time entry: %w", err)
		}
		t.PartyID = nullStr(party)
		out = append(out, t)
	}
	return out, rows.Err()
}
