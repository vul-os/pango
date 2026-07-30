package repo

// Inspection templates: reusable checklists.
//
// A template and its items are written in one transaction because a template
// with no items is not a half-made template, it is a checklist that silently
// records nothing — an inspector opens it on site, sees an empty list, ticks
// nothing, and the resulting inspection is indistinguishable from a unit in
// perfect condition.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

const templateCols = `id, org_id, name, kind, hlc, deleted, created_at`
const templateItemCols = `id, org_id, template_id, section, label, sort, hlc, deleted, created_at`

// CreateTemplate inserts a template and its items atomically.
func (r *Repo) CreateTemplate(orgID string, t domain.InspectionTemplate) (domain.InspectionTemplate, error) {
	if strings.TrimSpace(t.Name) == "" {
		return domain.InspectionTemplate{}, errors.New("template name is required")
	}
	if len(t.Items) == 0 {
		return domain.InspectionTemplate{}, errors.New("template needs at least one item")
	}
	t.OrgID = orgID
	if t.ID == "" {
		t.ID = store.NewID()
	}
	if t.Kind == "" {
		t.Kind = "general"
	}
	if t.CreatedAt == "" {
		t.CreatedAt = store.Now()
	}

	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, orgID, "inspection_template", t.ID, t, false)
		if err != nil {
			return err
		}
		t.HLC = hlc
		if _, err := tx.Exec(
			`INSERT INTO inspection_template (`+templateCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.OrgID, t.Name, t.Kind, t.HLC, 0, t.CreatedAt); err != nil {
			return err
		}
		for i := range t.Items {
			it := &t.Items[i]
			it.OrgID = orgID
			it.TemplateID = t.ID
			if it.ID == "" {
				it.ID = store.NewID()
			}
			if strings.TrimSpace(it.Label) == "" {
				return errors.New("template item label is required")
			}
			if it.Sort == 0 {
				it.Sort = int64(i)
			}
			if it.CreatedAt == "" {
				it.CreatedAt = store.Now()
			}
			itemHLC, err := r.s.Journal(tx, orgID, "inspection_template_item", it.ID, it, false)
			if err != nil {
				return err
			}
			it.HLC = itemHLC
			if _, err := tx.Exec(
				`INSERT INTO inspection_template_item (`+templateItemCols+`)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				it.ID, it.OrgID, it.TemplateID, it.Section, it.Label, it.Sort,
				it.HLC, 0, it.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.InspectionTemplate{}, err
	}
	return t, nil
}

// GetTemplate returns a template with its items.
func (r *Repo) GetTemplate(orgID, id string) (domain.InspectionTemplate, error) {
	row := r.s.DB().QueryRow(
		`SELECT `+templateCols+` FROM inspection_template
		 WHERE id = ? AND org_id = ? AND deleted = 0`, id, orgID)
	t, err := scanTemplate(row)
	if err != nil {
		return domain.InspectionTemplate{}, err
	}
	items, err := r.ListTemplateItems(orgID, id)
	if err != nil {
		return domain.InspectionTemplate{}, err
	}
	t.Items = items
	return t, nil
}

// ListTemplates returns the org's templates without their items.
func (r *Repo) ListTemplates(orgID string) ([]domain.InspectionTemplate, error) {
	rows, err := r.s.DB().Query(
		`SELECT `+templateCols+` FROM inspection_template
		 WHERE org_id = ? AND deleted = 0 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.InspectionTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTemplateItems returns a template's items in display order.
func (r *Repo) ListTemplateItems(orgID, templateID string) ([]domain.TemplateItem, error) {
	rows, err := r.s.DB().Query(
		`SELECT `+templateItemCols+` FROM inspection_template_item
		 WHERE org_id = ? AND template_id = ? AND deleted = 0 ORDER BY sort, id`,
		orgID, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TemplateItem{}
	for rows.Next() {
		var it domain.TemplateItem
		var deleted int
		if err := rows.Scan(&it.ID, &it.OrgID, &it.TemplateID, &it.Section, &it.Label,
			&it.Sort, &it.HLC, &deleted, &it.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan template item: %w", err)
		}
		it.Deleted = deleted != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

func scanTemplate(sc scanner) (domain.InspectionTemplate, error) {
	var t domain.InspectionTemplate
	var deleted int
	err := sc.Scan(&t.ID, &t.OrgID, &t.Name, &t.Kind, &t.HLC, &deleted, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.InspectionTemplate{}, ErrNotFound
	}
	if err != nil {
		return domain.InspectionTemplate{}, fmt.Errorf("scan template: %w", err)
	}
	t.Deleted = deleted != 0
	return t, nil
}
