package repo

// Inspections. Linked to a building AND a unit (§4.2), which is what makes the
// ingoing/outgoing comparison possible: the pair to compare is "the ingoing and
// the outgoing inspection of this unit", and a unit-less inspection can never
// be part of one.
//
// Scheduling is a decision the building's owning organisation is the single
// writer for (§5), so it needs no coordination with any peer.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const inspectionCols = `id, org_id, building_id, unit_id, template_id, job_id, kind, status,
	scheduled_for, performed_at, inspector_party_id, notes, hlc, deleted, created_at`

// InspectionFilter narrows an inspection listing.
type InspectionFilter struct {
	BuildingID string
	UnitID     string
	JobID      string
	Kind       string
	Status     string
}

// CreateInspection inserts an inspection. unitLabel, when given, resolves
// through EnsureUnit so an inspection and a job raised against the same
// physical door land on the same unit row.
func (r *Repo) CreateInspection(orgID string, i domain.Inspection, unitLabel string) (domain.Inspection, error) {
	if _, err := r.GetBuilding(orgID, i.BuildingID); err != nil {
		return domain.Inspection{}, err
	}
	if unitLabel != "" {
		u, err := r.EnsureUnit(orgID, i.BuildingID, unitLabel)
		if err != nil {
			return domain.Inspection{}, err
		}
		i.UnitID = u.ID
	}
	if i.UnitID != "" {
		if _, err := r.GetUnit(orgID, i.UnitID); err != nil {
			return domain.Inspection{}, err
		}
	}
	if i.TemplateID != "" {
		if _, err := r.GetTemplate(orgID, i.TemplateID); err != nil {
			return domain.Inspection{}, err
		}
	}
	if i.JobID != "" {
		if _, err := r.GetJob(orgID, i.JobID); err != nil {
			return domain.Inspection{}, err
		}
	}

	i.OrgID = orgID
	if i.ID == "" {
		i.ID = store.NewID()
	}
	if i.Kind == "" {
		i.Kind = domain.InspectionRoutine
	}
	if i.Status == "" {
		i.Status = domain.InspectionScheduled
	}
	if i.CreatedAt == "" {
		i.CreatedAt = store.Now()
	}
	if err := i.Validate(); err != nil {
		return domain.Inspection{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "inspection", i.ID, i, false)
		if err != nil {
			return err
		}
		i.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO inspection (`+inspectionCols+`)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			i.ID, i.OrgID, i.BuildingID, nullable(i.UnitID), nullable(i.TemplateID), nullable(i.JobID),
			i.Kind, i.Status, i.ScheduledFor, i.PerformedAt, nullable(i.InspectorID),
			i.Notes, i.HLC, 0, i.CreatedAt)
		return err
	})
	if err != nil {
		return domain.Inspection{}, err
	}
	return i, nil
}

// SetInspectionStatus advances an inspection. Completing one stamps
// performed_at, because "when was this walked" is the question a deposit
// dispute turns on and reconstructing it from a findings timestamp months later
// is not the same answer.
//
// A completed inspection is immutable: no further status change is accepted,
// including a "completed → completed" no-op. The legacy system's
// handleCompletion() set nothing and rejected nothing, so a completed
// inspection could still be edited underneath the record that was supposed to
// be final — which defeats the entire evidentiary point of a move-out capture.
// The only way Pango corrects a completed inspection is the same way it
// corrects a finding: a new inspection, never a mutation of the old one.
func (r *Repo) SetInspectionStatus(orgID, id, status string) (domain.Inspection, error) {
	i, err := r.GetInspection(orgID, id)
	if err != nil {
		return domain.Inspection{}, err
	}
	if i.Status == domain.InspectionComplete {
		return domain.Inspection{}, fmt.Errorf("%w: inspection is complete and immutable", ErrConflict)
	}
	i.Status = status
	if status == domain.InspectionComplete && i.PerformedAt == "" {
		i.PerformedAt = store.Now()
	}
	if err := i.Validate(); err != nil {
		return domain.Inspection{}, err
	}

	err = r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "inspection", i.ID, i, false)
		if err != nil {
			return err
		}
		i.HLC = hlc
		res, err := tx.Exec(
			`UPDATE inspection SET status = ?, performed_at = ?, hlc = ?
			 WHERE id = ? AND org_id = ?`, i.Status, i.PerformedAt, i.HLC, i.ID, orgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return domain.Inspection{}, err
	}
	return i, nil
}

// GetInspection returns one inspection the org owns.
func (r *Repo) GetInspection(orgID, id string) (domain.Inspection, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+inspectionCols+` FROM inspection WHERE id = ? AND org_id = ? AND deleted = 0`,
		id, orgID)
	return scanInspection(row)
}

// ListInspections returns inspections the org owns, newest first.
func (r *Repo) ListInspections(orgID string, f InspectionFilter) ([]domain.Inspection, error) {
	q := `SELECT ` + inspectionCols + ` FROM inspection WHERE org_id = ? AND deleted = 0`
	args := []any{orgID}
	if f.BuildingID != "" {
		q += " AND building_id = ?"
		args = append(args, f.BuildingID)
	}
	if f.UnitID != "" {
		q += " AND unit_id = ?"
		args = append(args, f.UnitID)
	}
	if f.JobID != "" {
		q += " AND job_id = ?"
		args = append(args, f.JobID)
	}
	if f.Kind != "" {
		q += " AND kind = ?"
		args = append(args, f.Kind)
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, f.Status)
	}
	q += " ORDER BY created_at DESC, id DESC"

	rows, err := r.s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Inspection{}
	for rows.Next() {
		i, err := scanInspection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func scanInspection(sc scanner) (domain.Inspection, error) {
	var i domain.Inspection
	var unitID, templateID, jobID, inspector sql.NullString
	var deleted int
	err := sc.Scan(&i.ID, &i.OrgID, &i.BuildingID, &unitID, &templateID, &jobID, &i.Kind, &i.Status,
		&i.ScheduledFor, &i.PerformedAt, &inspector, &i.Notes, &i.HLC, &deleted, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Inspection{}, ErrNotFound
	}
	if err != nil {
		return domain.Inspection{}, fmt.Errorf("scan inspection: %w", err)
	}
	i.UnitID = nullStr(unitID)
	i.TemplateID = nullStr(templateID)
	i.JobID = nullStr(jobID)
	i.InspectorID = nullStr(inspector)
	i.Deleted = deleted != 0
	return i, nil
}

// MatchingIngoing returns the ingoing inspection this outgoing inspection
// should be compared against: the most recent ingoing inspection of the same
// unit that was performed at or before the outgoing one.
//
// "Most recent, not-after" is the rule rather than simply "latest ingoing on
// the unit" because a unit accumulates one ingoing/outgoing pair per tenancy
// over its life. Pairing an outgoing inspection with whichever ingoing
// inspection happens to be newest would, for a unit on its third tenant,
// compare this move-out against next year's move-in the moment that one is
// captured — silently wrong in a way nobody would notice until the numbers
// stopped making sense. Ordering on performed_at (falling back to created_at
// for an inspection that has not been marked performed) keeps each outgoing
// bound to the ingoing that actually preceded it.
func (r *Repo) MatchingIngoing(orgID string, outgoing domain.Inspection) (domain.Inspection, error) {
	if outgoing.Kind != domain.InspectionOutgoing {
		return domain.Inspection{}, fmt.Errorf("%w: not an outgoing inspection", ErrConflict)
	}
	if outgoing.UnitID == "" {
		return domain.Inspection{}, fmt.Errorf("%w: outgoing inspection has no unit", ErrConflict)
	}
	cutoff := outgoing.PerformedAt
	if cutoff == "" {
		cutoff = outgoing.CreatedAt
	}
	row := r.s.DB().QueryRow(
		`SELECT `+inspectionCols+` FROM inspection
		 WHERE org_id = ? AND unit_id = ? AND kind = ? AND deleted = 0
		   AND COALESCE(NULLIF(performed_at, ''), created_at) <= ?
		 ORDER BY COALESCE(NULLIF(performed_at, ''), created_at) DESC, id DESC
		 LIMIT 1`,
		orgID, outgoing.UnitID, domain.InspectionIngoing, cutoff)
	return scanInspection(row)
}
