package api

// Parties and peers: the people and the nodes.
//
// The peer endpoints return a peer's public key but never a private one — the
// node's own seed lives in store and is exposed only through a signer
// interface, so there is no route here that could serve it even by mistake
// (§11: secrets are never logged, never in Debug output, never on the wire).

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vul-os/pango/backend/internal/domain"
)

func (s *Server) handleListParties(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListParties(orgOf(r), r.URL.Query().Get("kind"))
	if err != nil {
		writeServerErr(w, "list parties", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type partyReq struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	PubKey string `json:"pubkey"`
}

func (s *Server) handleCreateParty(w http.ResponseWriter, r *http.Request) {
	var req partyReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.Repo.CreateParty(orgOf(r), domain.Party{
		Kind: req.Kind, Name: req.Name, Email: req.Email, Phone: req.Phone, PubKey: req.PubKey,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListPeers(orgOf(r))
	if err != nil {
		writeServerErr(w, "list peers", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type peerReq struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	PubKey  string `json:"pubkey"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) handleSavePeer(w http.ResponseWriter, r *http.Request) {
	var req peerReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.Repo.SavePeer(orgOf(r), domain.Peer{
		ID: req.ID, Name: req.Name, URL: req.URL, PubKey: req.PubKey, Enabled: req.Enabled,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePeer(w http.ResponseWriter, r *http.Request) {
	if err := s.Repo.DeletePeer(orgOf(r), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
