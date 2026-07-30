package repo

// Parties: staff, contractors and tenants in one table (§4.2).
//
// A tenant here is a participant, not an account (§4.3). There is no password
// column and no login path: a tenant reports a leak and is told it is fixed,
// and requiring them to hold a key or install something to do that would mean
// most of them simply phone instead — which is exactly the failure mode the
// product exists to remove.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const partyCols = `id, org_id, kind, name, email, phone, pubkey, hlc, deleted, created_at`

// CreateParty inserts a party owned by orgID.
func (r *Repo) CreateParty(orgID string, p domain.Party) (domain.Party, error) {
	p.OrgID = orgID
	if p.ID == "" {
		p.ID = store.NewID()
	}
	if p.Kind == "" {
		p.Kind = domain.PartyStaff
	}
	if p.CreatedAt == "" {
		p.CreatedAt = store.Now()
	}
	if err := p.Validate(); err != nil {
		return domain.Party{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "party", p.ID, p, false)
		if err != nil {
			return err
		}
		p.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO party (`+partyCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.OrgID, p.Kind, p.Name, p.Email, p.Phone, p.PubKey, p.HLC, 0, p.CreatedAt)
		return err
	})
	if err != nil {
		return domain.Party{}, err
	}
	return p, nil
}

// GetParty returns one party the org owns.
func (r *Repo) GetParty(orgID, id string) (domain.Party, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+partyCols+` FROM party WHERE id = ? AND org_id = ? AND deleted = 0`, id, orgID)
	return scanParty(row)
}

// ListParties returns the org's parties, optionally of one kind.
func (r *Repo) ListParties(orgID, kind string) ([]domain.Party, error) {
	q := `SELECT ` + partyCols + ` FROM party WHERE org_id = ? AND deleted = 0`
	args := []any{orgID}
	if kind != "" {
		q += " AND kind = ?"
		args = append(args, kind)
	}
	q += " ORDER BY name"

	rows, err := r.s.DB().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Party{}
	for rows.Next() {
		p, err := scanParty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanParty(sc scanner) (domain.Party, error) {
	var p domain.Party
	var deleted int
	err := sc.Scan(&p.ID, &p.OrgID, &p.Kind, &p.Name, &p.Email, &p.Phone, &p.PubKey,
		&p.HLC, &deleted, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Party{}, ErrNotFound
	}
	if err != nil {
		return domain.Party{}, fmt.Errorf("scan party: %w", err)
	}
	p.Deleted = deleted != 0
	return p, nil
}
