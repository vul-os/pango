package repo

// Cost entries: immutable, insert-only, int64 minor units (§6).
//
// This file has an AddCost and a ListCosts and nothing else. There is no
// UpdateCost and no DeleteCost, deliberately:
//
// If two people record spend on the same job while partitioned, union merge
// means the amounts ADD, which is the arithmetically correct answer. A stored
// total, or an editable entry, would keep whichever write landed last and lose
// the other — silently, with no error, and with no way to notice until a
// landlord queries an invoice.
//
// A correction is AddCost with a negative amount. The audit trail is therefore
// complete by construction rather than by anyone remembering to be careful.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const costCols = `id, org_id, job_id, kind, description, amount_minor, currency, party_id, hlc, created_at`

// AddCost appends a cost entry to a job the org owns.
func (r *Repo) AddCost(orgID string, c domain.CostEntry) (domain.CostEntry, error) {
	if _, err := r.GetJob(orgID, c.JobID); err != nil {
		return domain.CostEntry{}, err
	}
	c.OrgID = orgID
	if c.ID == "" {
		c.ID = store.NewID()
	}
	if c.Kind == "" {
		c.Kind = domain.CostLabour
	}
	if c.Currency == "" {
		c.Currency = "ZAR"
	}
	if c.CreatedAt == "" {
		c.CreatedAt = store.Now()
	}
	if err := c.Validate(); err != nil {
		return domain.CostEntry{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "cost_entry", c.ID, c, false)
		if err != nil {
			return err
		}
		c.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO cost_entry (`+costCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.OrgID, c.JobID, c.Kind, c.Description, int64(c.AmountMinor),
			c.Currency, nullable(c.PartyID), c.HLC, c.CreatedAt)
		return err
	})
	if err != nil {
		return domain.CostEntry{}, err
	}
	return c, nil
}

// ListCosts returns a job's cost entries oldest-first.
func (r *Repo) ListCosts(orgID, jobID string) ([]domain.CostEntry, error) {
	if _, err := r.GetJob(orgID, jobID); err != nil {
		return nil, err
	}
	rows, err := r.s.DB().Query(
		`SELECT `+costCols+` FROM cost_entry WHERE org_id = ? AND job_id = ?
		 ORDER BY created_at ASC, id ASC`, orgID, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.CostEntry{}
	for rows.Next() {
		var c domain.CostEntry
		var party sql.NullString
		var amount int64
		if err := rows.Scan(&c.ID, &c.OrgID, &c.JobID, &c.Kind, &c.Description, &amount,
			&c.Currency, &party, &c.HLC, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan cost entry: %w", err)
		}
		c.AmountMinor = domain.Money(amount)
		c.PartyID = nullStr(party)
		out = append(out, c)
	}
	return out, rows.Err()
}
