package api

// Registration, login, logout.
//
// Registration creates an organisation and its first operator account together.
// It is open only while no account exists yet — a Pango node belongs to one
// organisation and the person who sets it up, and a permanently open
// registration endpoint on a box exposed to a LAN is an invitation to create an
// account nobody notices. Once the first account exists, further accounts are
// made by an authenticated operator (§11: no default outbound anything, no
// default open anything).

import (
	"net/http"

	"github.com/vul-os/pango/backend/internal/domain"
)

type registerReq struct {
	Organisation string `json:"organisation"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Name         string `json:"name"`
}

type authResp struct {
	Token string      `json:"token"`
	User  domain.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}

	n, err := s.Repo.UserCount()
	if err != nil {
		writeServerErr(w, "user count", err)
		return
	}
	if n > 0 {
		// Deliberately not "an account already exists": that is a fact about
		// the deployment, and an unauthenticated caller does not need it.
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "registration is closed on this node",
		})
		return
	}

	org, err := s.Repo.CreateOrg(req.Organisation)
	if err != nil {
		writeErr(w, err)
		return
	}
	user, err := s.Repo.CreateUser(org.ID, req.Email, req.Password, req.Name, "owner")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.issueSession(w, user, http.StatusCreated)
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	user, err := s.Repo.Authenticate(req.Email, req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.issueSession(w, user, http.StatusOK)
}

func (s *Server) issueSession(w http.ResponseWriter, user domain.User, status int) {
	token, err := s.Repo.CreateSession(user)
	if err != nil {
		writeServerErr(w, "create session", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:  SessionCookie,
		Value: token,
		Path:  "/",
		// HttpOnly: the token is never readable from JavaScript, so an XSS in
		// the frontend cannot exfiltrate a long-lived session.
		HttpOnly: true,
		// Lax rather than Strict: Strict would drop the cookie on a link
		// followed from an email, which is how a manager opens a job.
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SecureCookies,
		MaxAge:   int(sessionTTLSeconds),
	})
	// The token is also returned in the body for non-browser clients. It is
	// never logged: see writeServerErr, which logs the context and not the
	// payload.
	writeJSON(w, status, authResp{Token: token, User: user})
}

const sessionTTLSeconds = 30 * 24 * 60 * 60

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Repo.DeleteSession(tokenFrom(r)); err != nil {
		writeServerErr(w, "delete session", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SecureCookies,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	org, err := s.Repo.GetOrg(user.OrgID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "organisation": org})
}
