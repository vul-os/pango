package repo

// Jobs, and the per-building job number sequence.
//
// The sequence is namespaced per building because the building is the authority
// (§5): its owning organisation is the single legitimate writer, so a number can
// be allocated with no coordination, no lock and no round trip. A global
// sequence would need exactly the consensus this architecture exists to avoid —
// two offline nodes would both allocate "job 481" with no way to know they'd
// collided until they compared notes.
//
// Namespacing per building removes that for the overwhelming common case (one
// contributing node per building) but does not remove it entirely: "single
// legitimate writer" is an organisational fact, not a mechanical one, and nothing
// stops an office node and a field tablet — both belonging to that one
// organisation — from each raising the FIRST job against a building neither has
// synced yet, both offline, both minting number 1. That collision is resolved at
// the point it becomes visible — when the two rows actually meet during
// sync — by store/migrations/200_maintenance.sql's trigger, not here: it
// bumps whichever of the two is causally later to a fresh number the moment
// both are present in one database, deterministically, on every node that ends
// up holding both. See docs/SYNC.md "Job numbers under divergence".

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const jobCols = `id, org_id, building_id, unit_id, number, title, description, status,
	priority, category, assignee_party_id, reporter_party_id, opened_at, closed_at,
	hlc, deleted, created_at`

// JobFilter narrows a job listing. Every field is optional; org scoping is not
// part of it, because org scoping is not something a caller gets to choose.
type JobFilter struct {
	BuildingID string
	UnitID     string
	Status     string
	OpenOnly   bool
}

// CreateJob inserts a job, allocating its building-scoped number.
//
// unitLabel, when non-empty, is resolved through EnsureUnit inside the same
// call, so a job raised against "Flat 3A" from a tablet and one raised against
// "3a" from the office land on one unit.
func (r *Repo) CreateJob(orgID string, j domain.Job, unitLabel string) (domain.Job, error) {
	if _, err := r.GetBuilding(orgID, j.BuildingID); err != nil {
		return domain.Job{}, err
	}
	if unitLabel != "" {
		u, err := r.EnsureUnit(orgID, j.BuildingID, unitLabel)
		if err != nil {
			return domain.Job{}, err
		}
		j.UnitID = u.ID
	}
	if j.UnitID != "" {
		if _, err := r.GetUnit(orgID, j.UnitID); err != nil {
			return domain.Job{}, err
		}
	}

	j.OrgID = orgID
	if j.ID == "" {
		j.ID = store.NewID()
	}
	if j.Status == "" {
		j.Status = domain.StatusReported
	}
	if j.Priority == "" {
		j.Priority = domain.PriorityNormal
	}
	if j.CreatedAt == "" {
		j.CreatedAt = store.Now()
	}
	if j.OpenedAt == "" {
		j.OpenedAt = j.CreatedAt
	}
	if err := j.Validate(); err != nil {
		return domain.Job{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		number, err := nextJobNumber(tx, j.BuildingID)
		if err != nil {
			return err
		}
		j.Number = number

		hlc, err := r.s.Journal(tx, orgID, "job", j.ID, j, false)
		if err != nil {
			return err
		}
		j.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO job (`+jobCols+`)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			j.ID, j.OrgID, j.BuildingID, nullable(j.UnitID), j.Number, j.Title, j.Description,
			j.Status, j.Priority, j.Category, nullable(j.AssigneeID), nullable(j.ReporterID),
			j.OpenedAt, j.ClosedAt, j.HLC, 0, j.CreatedAt)
		return err
	})
	if err != nil {
		return domain.Job{}, err
	}
	return j, nil
}

// nextJobNumber allocates the next number for a building inside the caller's
// transaction. Numbers start at 1 and never repeat for a building — from this
// node's own point of view. It has no way to see a number a different,
// currently-offline node has already allocated for the same building; that is
// reconciled later, only if it actually happens, by the trigger in
// store/migrations/200_maintenance.sql when the two rows meet.
func nextJobNumber(tx *sql.Tx, buildingID string) (int64, error) {
	if _, err := tx.Exec(
		`INSERT INTO job_number_seq (building_id, next) VALUES (?, 1)
		 ON CONFLICT(building_id) DO UPDATE SET next = job_number_seq.next + 1`,
		buildingID); err != nil {
		return 0, err
	}
	var n int64
	if err := tx.QueryRow(
		"SELECT next FROM job_number_seq WHERE building_id = ?", buildingID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// GetJob returns one job the org owns.
func (r *Repo) GetJob(orgID, id string) (domain.Job, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+jobCols+` FROM job WHERE id = ? AND org_id = ? AND deleted = 0`, id, orgID)
	return scanJob(row)
}

// ListJobs returns jobs the org owns, newest first, narrowed by f.
func (r *Repo) ListJobs(orgID string, f JobFilter) ([]domain.Job, error) {
	q := `SELECT ` + jobCols + ` FROM job WHERE org_id = ? AND deleted = 0`
	args := []any{orgID}
	if f.BuildingID != "" {
		q += " AND building_id = ?"
		args = append(args, f.BuildingID)
	}
	if f.UnitID != "" {
		q += " AND unit_id = ?"
		args = append(args, f.UnitID)
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.OpenOnly {
		q += " AND status NOT IN (?, ?)"
		args = append(args, domain.StatusClosed, domain.StatusCancelled)
	}
	q += " ORDER BY created_at DESC, id DESC"

	rows, err := r.s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// SetJobStatus moves a job through the status graph and records the move as a
// job event.
//
// The event is written in the same transaction as the status change. A status
// that changed with no trace of who changed it or when is the thing a manager
// cannot argue with a contractor about six weeks later, so the two facts commit
// together or not at all.
func (r *Repo) SetJobStatus(orgID, jobID, status, actorPartyID, note string) (domain.Job, error) {
	j, err := r.GetJob(orgID, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if !domain.ValidStatus(status) {
		return domain.Job{}, fmt.Errorf("%w: unknown status %q", ErrConflict, status)
	}
	if !domain.CanTransition(j.Status, status) {
		return domain.Job{}, fmt.Errorf("%w: cannot move job from %s to %s", ErrConflict, j.Status, status)
	}

	from := j.Status
	j.Status = status
	switch {
	case status == domain.StatusClosed || status == domain.StatusCancelled:
		j.ClosedAt = store.Now()
	case from == domain.StatusClosed || from == domain.StatusCancelled:
		j.ClosedAt = "" // reopened: it is open work again and must count as such
	}

	body := fmt.Sprintf("status %s → %s", from, status)
	if strings.TrimSpace(note) != "" {
		body += ": " + note
	}
	ev := domain.JobEvent{
		ID:         store.NewID(),
		OrgID:      orgID,
		JobID:      jobID,
		Kind:       "status",
		Body:       body,
		ActorID:    actorPartyID,
		Visibility: domain.VisibilityInternal,
		CreatedAt:  store.Now(),
	}

	err = r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "job", j.ID, j, false)
		if err != nil {
			return err
		}
		j.HLC = hlc
		res, err := tx.Exec(
			`UPDATE job SET status = ?, closed_at = ?, hlc = ? WHERE id = ? AND org_id = ?`,
			j.Status, j.ClosedAt, j.HLC, j.ID, orgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return insertEvent(tx, r.s, ev)
	})
	if err != nil {
		return domain.Job{}, err
	}
	return j, nil
}

// AssignJob sets a job's assignee. Assignment is one of the three decisions the
// building's owner is the single writer for (§5).
func (r *Repo) AssignJob(orgID, jobID, partyID string) (domain.Job, error) {
	j, err := r.GetJob(orgID, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if partyID != "" {
		if _, err := r.GetParty(orgID, partyID); err != nil {
			return domain.Job{}, err
		}
	}
	j.AssigneeID = partyID

	err = r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "job", j.ID, j, false)
		if err != nil {
			return err
		}
		j.HLC = hlc
		res, err := tx.Exec(
			`UPDATE job SET assignee_party_id = ?, hlc = ? WHERE id = ? AND org_id = ?`,
			nullable(partyID), j.HLC, j.ID, orgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return j, nil
}

func scanJob(sc scanner) (domain.Job, error) {
	var j domain.Job
	var unitID, assignee, reporter sql.NullString
	var deleted int
	err := sc.Scan(&j.ID, &j.OrgID, &j.BuildingID, &unitID, &j.Number, &j.Title, &j.Description,
		&j.Status, &j.Priority, &j.Category, &assignee, &reporter, &j.OpenedAt, &j.ClosedAt,
		&j.HLC, &deleted, &j.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Job{}, ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("scan job: %w", err)
	}
	j.UnitID = nullStr(unitID)
	j.AssigneeID = nullStr(assignee)
	j.ReporterID = nullStr(reporter)
	j.Deleted = deleted != 0
	return j, nil
}
