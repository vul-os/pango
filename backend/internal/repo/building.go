package repo

// Buildings. The building is the unit of authority (§5): whoever manages it
// owns its jobs, its job numbering and its inspections. Every other aggregate
// in this package reaches its organisation through a building, so the org check
// here is the one the rest of the layer depends on.

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const buildingCols = `id, org_id, name, address, lat, lon, unit_scheme, hlc, deleted, created_at`

// CreateBuilding inserts a building owned by orgID.
func (r *Repo) CreateBuilding(orgID string, b domain.Building) (domain.Building, error) {
	// The caller's org_id is overwritten from the session, not merged with
	// whatever arrived in the request body.
	b.OrgID = orgID
	if b.ID == "" {
		b.ID = store.NewID()
	}
	if b.CreatedAt == "" {
		b.CreatedAt = store.Now()
	}
	if err := b.Validate(); err != nil {
		return domain.Building{}, err
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "building", b.ID, b, false)
		if err != nil {
			return err
		}
		b.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO building (`+buildingCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			b.ID, b.OrgID, b.Name, b.Address, b.Lat, b.Lon, b.UnitScheme, b.HLC, 0, b.CreatedAt)
		return err
	})
	if err != nil {
		return domain.Building{}, err
	}
	return b, nil
}

// UpdateBuilding replaces the mutable fields of a building the org owns.
func (r *Repo) UpdateBuilding(orgID string, b domain.Building) (domain.Building, error) {
	existing, err := r.GetBuilding(orgID, b.ID)
	if err != nil {
		return domain.Building{}, err
	}
	b.OrgID = orgID
	b.CreatedAt = existing.CreatedAt
	if err := b.Validate(); err != nil {
		return domain.Building{}, err
	}

	err = r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "building", b.ID, b, false)
		if err != nil {
			return err
		}
		b.HLC = hlc
		res, err := tx.Exec(
			`UPDATE building SET name = ?, address = ?, lat = ?, lon = ?, unit_scheme = ?, hlc = ?
			 WHERE id = ? AND org_id = ?`,
			b.Name, b.Address, b.Lat, b.Lon, b.UnitScheme, b.HLC, b.ID, orgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return domain.Building{}, err
	}
	return b, nil
}

// GetBuilding returns one building the org owns.
func (r *Repo) GetBuilding(orgID, id string) (domain.Building, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+buildingCols+` FROM building WHERE id = ? AND org_id = ? AND deleted = 0`, id, orgID)
	return scanBuilding(row)
}

// ListBuildings returns every live building the org owns, newest name order.
func (r *Repo) ListBuildings(orgID string) ([]domain.Building, error) {
	rows, err := r.s.DB().Query(
		`SELECT `+buildingCols+` FROM building WHERE org_id = ? AND deleted = 0 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Building{}
	for rows.Next() {
		b, err := scanBuilding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DeleteBuilding tombstones a building.
//
// It is a tombstone, not a DELETE, because a row physically removed on one node
// is simply re-created by the first peer that never saw the removal — the
// deletion has no position in the total order to win with.
func (r *Repo) DeleteBuilding(orgID, id string) error {
	b, err := r.GetBuilding(orgID, id)
	if err != nil {
		return err
	}
	b.Deleted = true
	return r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "building", id, b, true)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE building SET deleted = 1, hlc = ? WHERE id = ? AND org_id = ?`, hlc, id, orgID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

type scanner interface {
	Scan(dest ...any) error
}

func scanBuilding(sc scanner) (domain.Building, error) {
	var b domain.Building
	var lat, lon sql.NullFloat64
	var deleted int
	err := sc.Scan(&b.ID, &b.OrgID, &b.Name, &b.Address, &lat, &lon, &b.UnitScheme, &b.HLC, &deleted, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Building{}, ErrNotFound
	}
	if err != nil {
		return domain.Building{}, fmt.Errorf("scan building: %w", err)
	}
	if lat.Valid {
		v := lat.Float64
		b.Lat = &v
	}
	if lon.Valid {
		v := lon.Float64
		b.Lon = &v
	}
	b.Deleted = deleted != 0
	return b, nil
}
