package repo

// Units, created on first use (§4.1).
//
// EnsureUnit is the only way a unit comes into existence, and that is the
// point. The legacy system let every job carry its own free-text spelling of
// the unit, so per-unit reporting fragmented across "Flat 3A" / "3A" /
// "flat 3a". Here the label is normalised to a key, the key is matched against
// the building's existing units, and a match returns the existing unit rather
// than making a new one. Nobody has to remember to deduplicate, because there
// is no code path that creates a duplicate.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const unitCols = `id, org_id, building_id, key, label, hlc, deleted, created_at`

// EnsureUnit returns the unit in buildingID whose normalised key matches label,
// creating it if it does not exist. The building's unit_scheme drives the
// normalisation.
//
// The first spelling seen wins the display label. Later spellings of the same
// unit resolve to the same row and leave the label alone: rewriting it on every
// write would make a unit's name flicker between "Flat 3A" and "3a" depending
// on who last touched a job, which looks like data corruption to a user.
func (r *Repo) EnsureUnit(orgID, buildingID, label string) (domain.Unit, error) {
	b, err := r.GetBuilding(orgID, buildingID)
	if err != nil {
		return domain.Unit{}, err
	}
	key, err := domain.NormaliseUnitKey(b.UnitScheme, label)
	if err != nil {
		return domain.Unit{}, err
	}

	if u, err := r.unitByKey(orgID, buildingID, key); err == nil {
		return u, nil
	} else if err != ErrNotFound {
		return domain.Unit{}, err
	}

	u := domain.Unit{
		ID:         store.NewID(),
		OrgID:      orgID,
		BuildingID: buildingID,
		Key:        key,
		Label:      label,
		CreatedAt:  store.Now(),
	}
	err = r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "unit", u.ID, u, false)
		if err != nil {
			return err
		}
		u.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO unit (`+unitCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.OrgID, u.BuildingID, u.Key, u.Label, u.HLC, 0, u.CreatedAt)
		return err
	})
	if err != nil {
		// Lost a race against a concurrent create of the same key. The unique
		// index did its job; return the winner rather than the error, because
		// from the caller's point of view "the unit exists" is exactly what
		// they asked for.
		if existing, lookupErr := r.unitByKey(orgID, buildingID, key); lookupErr == nil {
			return existing, nil
		}
		return domain.Unit{}, err
	}
	return u, nil
}

func (r *Repo) unitByKey(orgID, buildingID, key string) (domain.Unit, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+unitCols+` FROM unit
		 WHERE org_id = ? AND building_id = ? AND key = ? AND deleted = 0`,
		orgID, buildingID, key)
	return scanUnit(row)
}

// GetUnit returns one unit the org owns.
func (r *Repo) GetUnit(orgID, id string) (domain.Unit, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+unitCols+` FROM unit WHERE id = ? AND org_id = ? AND deleted = 0`, id, orgID)
	return scanUnit(row)
}

// ListUnits returns every live unit in a building the org owns.
func (r *Repo) ListUnits(orgID, buildingID string) ([]domain.Unit, error) {
	rows, err := r.s.DB().Query(
		`SELECT `+unitCols+` FROM unit
		 WHERE org_id = ? AND building_id = ? AND deleted = 0 ORDER BY key`,
		orgID, buildingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Unit{}
	for rows.Next() {
		u, err := scanUnit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func scanUnit(sc scanner) (domain.Unit, error) {
	var u domain.Unit
	var deleted int
	err := sc.Scan(&u.ID, &u.OrgID, &u.BuildingID, &u.Key, &u.Label, &u.HLC, &deleted, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Unit{}, ErrNotFound
	}
	if err != nil {
		return domain.Unit{}, fmt.Errorf("scan unit: %w", err)
	}
	u.Deleted = deleted != 0
	return u, nil
}
