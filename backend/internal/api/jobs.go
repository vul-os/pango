package api

// Jobs and their append-only children: events, cost entries, time entries.
//
// The ledgers have POST and GET and nothing else — no PATCH, no DELETE. That is
// the HTTP-level expression of §6: a correction is a new entry with a negative
// amount, so an endpoint that could edit one would break the merge property the
// whole offline design rests on.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
)

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Repo.ListJobs(orgOf(r), repo.JobFilter{
		BuildingID: q.Get("building_id"),
		UnitID:     q.Get("unit_id"),
		Status:     q.Get("status"),
		OpenOnly:   q.Get("open") == "1",
	})
	if err != nil {
		writeServerErr(w, "list jobs", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type jobReq struct {
	BuildingID  string `json:"building_id"`
	UnitID      string `json:"unit_id"`
	UnitLabel   string `json:"unit_label"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Category    string `json:"category"`
	ReporterID  string `json:"reporter_party_id"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req jobReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	j, err := s.Repo.CreateJob(orgOf(r), domain.Job{
		BuildingID: req.BuildingID, UnitID: req.UnitID, Title: req.Title,
		Description: req.Description, Priority: req.Priority, Category: req.Category,
		ReporterID: req.ReporterID,
	}, req.UnitLabel)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	org := orgOf(r)
	id := chi.URLParam(r, "id")
	j, err := s.Repo.GetJob(org, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	// Totals are computed here rather than stored on the job (§6).
	totals, err := s.Reporter.ByJob(org, id)
	if err != nil {
		writeServerErr(w, "job totals", err)
		return
	}
	resp := map[string]any{"job": j}
	if len(totals) == 1 {
		resp["totals"] = totals[0]
	}
	writeJSON(w, http.StatusOK, resp)
}

type statusReq struct {
	Status  string `json:"status"`
	Note    string `json:"note"`
	ActorID string `json:"actor_party_id"`
}

func (s *Server) handleSetJobStatus(w http.ResponseWriter, r *http.Request) {
	var req statusReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	j, err := s.Repo.SetJobStatus(orgOf(r), chi.URLParam(r, "id"), req.Status, req.ActorID, req.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

type assignReq struct {
	PartyID string `json:"party_id"`
}

func (s *Server) handleAssignJob(w http.ResponseWriter, r *http.Request) {
	var req assignReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	j, err := s.Repo.AssignJob(orgOf(r), chi.URLParam(r, "id"), req.PartyID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// handleListEvents serves the job thread. public=1 restricts to the
// tenant-visible subset (§4.3).
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListEvents(orgOf(r), chi.URLParam(r, "id"), r.URL.Query().Get("public") == "1")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type eventReq struct {
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	ActorID    string `json:"actor_party_id"`
	Visibility string `json:"visibility"`
}

func (s *Server) handleAddEvent(w http.ResponseWriter, r *http.Request) {
	var req eventReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	e, err := s.Repo.AddEvent(orgOf(r), domain.JobEvent{
		JobID: chi.URLParam(r, "id"), Kind: req.Kind, Body: req.Body,
		ActorID: req.ActorID, Visibility: req.Visibility,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleListCosts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListCosts(orgOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type costReq struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	// AmountMinor is minor units as an integer. There is no "amount" float
	// field and there will not be one (§3).
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	PartyID     string `json:"party_id"`
}

func (s *Server) handleAddCost(w http.ResponseWriter, r *http.Request) {
	var req costReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.Repo.AddCost(orgOf(r), domain.CostEntry{
		JobID: chi.URLParam(r, "id"), Kind: req.Kind, Description: req.Description,
		AmountMinor: domain.Money(req.AmountMinor), Currency: req.Currency, PartyID: req.PartyID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListTime(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListTime(orgOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type timeReq struct {
	Minutes int64  `json:"minutes"`
	Note    string `json:"note"`
	PartyID string `json:"party_id"`
}

func (s *Server) handleAddTime(w http.ResponseWriter, r *http.Request) {
	var req timeReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.Repo.AddTime(orgOf(r), domain.TimeEntry{
		JobID: chi.URLParam(r, "id"), Minutes: req.Minutes, Note: req.Note, PartyID: req.PartyID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}
