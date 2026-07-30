package api

// Buildings and units.
//
// Note what none of these handlers do: read an organisation from the request.
// Every call passes orgOf(r), which comes from the session. A request for a
// building id belonging to another organisation returns 404 rather than 403,
// because 403 would confirm the id is real (see repo.ErrNotFound).

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vul-os/pango/backend/internal/domain"
)

func (s *Server) handleListBuildings(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListBuildings(orgOf(r))
	if err != nil {
		writeServerErr(w, "list buildings", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type buildingReq struct {
	Name       string   `json:"name"`
	Address    string   `json:"address"`
	Lat        *float64 `json:"lat"`
	Lon        *float64 `json:"lon"`
	UnitScheme string   `json:"unit_scheme"`
}

func (s *Server) handleCreateBuilding(w http.ResponseWriter, r *http.Request) {
	var req buildingReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.Repo.CreateBuilding(orgOf(r), domain.Building{
		Name: req.Name, Address: req.Address, Lat: req.Lat, Lon: req.Lon,
		UnitScheme: req.UnitScheme,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleGetBuilding(w http.ResponseWriter, r *http.Request) {
	b, err := s.Repo.GetBuilding(orgOf(r), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleUpdateBuilding(w http.ResponseWriter, r *http.Request) {
	var req buildingReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.Repo.UpdateBuilding(orgOf(r), domain.Building{
		ID: chi.URLParam(r, "id"), Name: req.Name, Address: req.Address,
		Lat: req.Lat, Lon: req.Lon, UnitScheme: req.UnitScheme,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleDeleteBuilding(w http.ResponseWriter, r *http.Request) {
	if err := s.Repo.DeleteBuilding(orgOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListUnits(w http.ResponseWriter, r *http.Request) {
	org := orgOf(r)
	buildingID := chi.URLParam(r, "id")
	if _, err := s.Repo.GetBuilding(org, buildingID); err != nil {
		writeErr(w, err)
		return
	}
	list, err := s.Repo.ListUnits(org, buildingID)
	if err != nil {
		writeServerErr(w, "list units", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type unitReq struct {
	Label string `json:"label"`
}

// handleEnsureUnit is create-or-return, not create. Posting "Flat 3A" twice, or
// "3A" after "Flat 3A", returns the same unit both times rather than a
// duplicate or a conflict (§4.1).
func (s *Server) handleEnsureUnit(w http.ResponseWriter, r *http.Request) {
	var req unitReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.Repo.EnsureUnit(orgOf(r), chi.URLParam(r, "id"), req.Label)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
