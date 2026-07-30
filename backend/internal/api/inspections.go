package api

// Inspection templates, inspections and findings.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/inspect"
	"github.com/vul-os/pango/backend/internal/repo"
)

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListTemplates(orgOf(r))
	if err != nil {
		writeServerErr(w, "list templates", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type templateReq struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Items []struct {
		Section string `json:"section"`
		Label   string `json:"label"`
		Sort    int64  `json:"sort"`
	} `json:"items"`
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	t := domain.InspectionTemplate{Name: req.Name, Kind: req.Kind}
	for _, it := range req.Items {
		t.Items = append(t.Items, domain.TemplateItem{
			Section: it.Section, Label: it.Label, Sort: it.Sort,
		})
	}
	created, err := s.Repo.CreateTemplate(orgOf(r), t)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	t, err := s.Repo.GetTemplate(orgOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleListInspections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Repo.ListInspections(orgOf(r), repo.InspectionFilter{
		BuildingID: q.Get("building_id"),
		UnitID:     q.Get("unit_id"),
		JobID:      q.Get("job_id"),
		Kind:       q.Get("kind"),
		Status:     q.Get("status"),
	})
	if err != nil {
		writeServerErr(w, "list inspections", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type inspectionReq struct {
	BuildingID   string `json:"building_id"`
	UnitID       string `json:"unit_id"`
	UnitLabel    string `json:"unit_label"`
	TemplateID   string `json:"template_id"`
	JobID        string `json:"job_id"`
	Kind         string `json:"kind"`
	ScheduledFor string `json:"scheduled_for"`
	InspectorID  string `json:"inspector_party_id"`
	Notes        string `json:"notes"`
}

func (s *Server) handleCreateInspection(w http.ResponseWriter, r *http.Request) {
	var req inspectionReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	i, err := s.Repo.CreateInspection(orgOf(r), domain.Inspection{
		BuildingID: req.BuildingID, UnitID: req.UnitID, TemplateID: req.TemplateID, JobID: req.JobID,
		Kind: req.Kind, ScheduledFor: req.ScheduledFor, InspectorID: req.InspectorID,
		Notes: req.Notes,
	}, req.UnitLabel)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, i)
}

func (s *Server) handleGetInspection(w http.ResponseWriter, r *http.Request) {
	org := orgOf(r)
	id := chi.URLParam(r, "id")
	i, err := s.Repo.GetInspection(org, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	findings, err := s.Repo.ListFindings(org, id)
	if err != nil {
		writeServerErr(w, "list findings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"inspection": i, "findings": findings})
}

func (s *Server) handleSetInspectionStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	i, err := s.Repo.SetInspectionStatus(orgOf(r), chi.URLParam(r, "id"), req.Status)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}

func (s *Server) handleListFindings(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListFindings(orgOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type findingReq struct {
	ItemID    string `json:"item_id"`
	Label     string `json:"label"`
	Condition string `json:"condition"`
	Comment   string `json:"comment"`
	PhotoRefs string `json:"photo_refs"`
}

func (s *Server) handleAddFinding(w http.ResponseWriter, r *http.Request) {
	var req findingReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.Repo.AddFinding(orgOf(r), domain.Finding{
		InspectionID: chi.URLParam(r, "id"), ItemID: req.ItemID, Label: req.Label,
		Condition: req.Condition, Comment: req.Comment, PhotoRefs: req.PhotoRefs,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

// handleCompareInspection is the headline feature (docs/INSPECTIONS.md §1):
// given an outgoing inspection, find the ingoing inspection of the same unit
// it should be measured against and return a per-item condition delta.
//
// The path parameter names the OUTGOING inspection. The ingoing counterpart is
// resolved server-side (repo.MatchingIngoing) rather than accepted as a
// parameter, for the same reason org scoping is never accepted as a
// parameter (§11): letting a caller name both sides would let it pair an
// outgoing inspection with an unrelated unit's ingoing one and manufacture a
// comparison that never happened.
func (s *Server) handleCompareInspection(w http.ResponseWriter, r *http.Request) {
	org := orgOf(r)
	outgoing, err := s.Repo.GetInspection(org, chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ingoing, err := s.Repo.MatchingIngoing(org, outgoing)
	if err != nil {
		writeErr(w, err)
		return
	}

	ingoingItems, err := s.Repo.ListTemplateItems(org, ingoing.TemplateID)
	if err != nil {
		writeServerErr(w, "list ingoing template items", err)
		return
	}
	outgoingItems, err := s.Repo.ListTemplateItems(org, outgoing.TemplateID)
	if err != nil {
		writeServerErr(w, "list outgoing template items", err)
		return
	}
	ingoingFindings, err := s.Repo.LatestFindings(org, ingoing.ID)
	if err != nil {
		writeServerErr(w, "list ingoing findings", err)
		return
	}
	outgoingFindings, err := s.Repo.LatestFindings(org, outgoing.ID)
	if err != nil {
		writeServerErr(w, "list outgoing findings", err)
		return
	}

	cmp, err := inspect.Compare(ingoing, outgoing, ingoingItems, outgoingItems, ingoingFindings, outgoingFindings)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cmp)
}
