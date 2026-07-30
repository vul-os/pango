// Package api is Pango's HTTP surface: chi routes, JSON bodies, and the
// authentication middleware that decides which organisation a request is
// scoped to.
//
// The one rule this package exists to enforce (§11): the organisation a request
// operates in is read from the session, and there is no code path by which a
// client can influence it. The legacy system took an organization_id filter
// from the frontend, which meant anyone who could edit a URL could read another
// managing agent's entire portfolio. That is why orgOf(r) below reads only from
// the request context, why the context value is only ever written by the auth
// middleware, and why no handler in this package accepts an org id in a body or
// a query parameter.
package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/report"
)

// SessionCookie is the cookie the browser client uses. Tokens are also accepted
// as a bearer header so a script or a tablet app does not need a cookie jar.
const SessionCookie = "pango_session"

// ctxKey is unexported so nothing outside this package can write the
// authenticated user into a request context. That is the whole isolation
// guarantee: if any other package could set it, org scoping would be
// forgeable from inside the process.
type ctxKey int

const userKey ctxKey = 0

// Server holds the API dependencies.
type Server struct {
	Repo     *repo.Repo
	Reporter *report.Reporter
	Version  string
	// AllowedOrigins is the CORS allowlist. Empty means same-origin only,
	// which is the correct default for a binary that serves its own frontend:
	// a fresh install grants no cross-origin access to anybody (§11).
	AllowedOrigins []string
	// SecureCookies marks the session cookie Secure. Off by default because
	// the common deployment is plain HTTP on a LAN, where a Secure cookie
	// would simply never be sent and nobody could log in.
	SecureCookies bool
	// Demo marks a seeded ephemeral instance so the UI can say so (§13).
	Demo bool
}

// New builds a Server.
func New(r *repo.Repo, version string) *Server {
	return &Server{Repo: r, Reporter: report.New(r.DB()), Version: version}
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(securityHeaders)

	if len(s.AllowedOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   s.AllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}

	// Unauthenticated. Health carries no tenant data by design: it is what a
	// monitoring check hits, and a monitoring check should not need a
	// credential that could be stolen from a config file.
	r.Get("/api/health", s.handleHealth)
	r.Post("/api/auth/register", s.handleRegister)
	r.Post("/api/auth/login", s.handleLogin)
	r.Post("/api/auth/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)

		r.Get("/api/auth/me", s.handleMe)

		r.Get("/api/buildings", s.handleListBuildings)
		r.Post("/api/buildings", s.handleCreateBuilding)
		r.Get("/api/buildings/{id}", s.handleGetBuilding)
		r.Patch("/api/buildings/{id}", s.handleUpdateBuilding)
		r.Delete("/api/buildings/{id}", s.handleDeleteBuilding)
		r.Get("/api/buildings/{id}/units", s.handleListUnits)
		r.Post("/api/buildings/{id}/units", s.handleEnsureUnit)

		r.Get("/api/jobs", s.handleListJobs)
		r.Post("/api/jobs", s.handleCreateJob)
		r.Get("/api/jobs/{id}", s.handleGetJob)
		r.Post("/api/jobs/{id}/status", s.handleSetJobStatus)
		r.Post("/api/jobs/{id}/assign", s.handleAssignJob)
		r.Get("/api/jobs/{id}/events", s.handleListEvents)
		r.Post("/api/jobs/{id}/events", s.handleAddEvent)
		r.Get("/api/jobs/{id}/costs", s.handleListCosts)
		r.Post("/api/jobs/{id}/costs", s.handleAddCost)
		r.Get("/api/jobs/{id}/time", s.handleListTime)
		r.Post("/api/jobs/{id}/time", s.handleAddTime)

		r.Get("/api/parties", s.handleListParties)
		r.Post("/api/parties", s.handleCreateParty)
		r.Get("/api/peers", s.handleListPeers)
		r.Post("/api/peers", s.handleSavePeer)
		r.Delete("/api/peers/{id}", s.handleDeletePeer)

		r.Get("/api/templates", s.handleListTemplates)
		r.Post("/api/templates", s.handleCreateTemplate)
		r.Get("/api/templates/{id}", s.handleGetTemplate)

		r.Get("/api/inspections", s.handleListInspections)
		r.Post("/api/inspections", s.handleCreateInspection)
		r.Get("/api/inspections/{id}", s.handleGetInspection)
		r.Post("/api/inspections/{id}/status", s.handleSetInspectionStatus)
		r.Get("/api/inspections/{id}/findings", s.handleListFindings)
		r.Post("/api/inspections/{id}/findings", s.handleAddFinding)
		r.Get("/api/inspections/{id}/comparison", s.handleCompareInspection)

		r.Get("/api/reports/buildings", s.handleReportBuildings)
		r.Get("/api/reports/units", s.handleReportUnits)
		r.Get("/api/reports/jobs", s.handleReportJobs)
		r.Get("/api/reports/status", s.handleReportStatus)
		r.Get("/api/reports/timeline", s.handleReportTimeline)
	})

	return r
}

// securityHeaders sets the defensive headers that cost nothing and close whole
// classes of bug.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// requireAuth resolves the session and puts the user in the request context.
// Every scoped route sits behind it, so a handler that forgets to check
// authentication cannot exist: there is no way to reach one unauthenticated.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Repo.SessionUser(tokenFrom(r))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenFrom reads the session token from the Authorization header or the
// session cookie, in that order.
func tokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if after, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// userOf returns the authenticated user. It is only ever called from handlers
// behind requireAuth, where the value is guaranteed present.
func userOf(r *http.Request) domain.User {
	u, _ := r.Context().Value(userKey).(domain.User)
	return u
}

// orgOf returns the organisation this request is scoped to.
//
// It reads from the context and nowhere else. There is no variant of this
// function that takes a parameter, and there must never be one — that is the
// entire tenancy boundary in one line.
func orgOf(r *http.Request) string { return userOf(r).OrgID }

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.Version,
		"demo":    s.Demo,
		"node":    s.Repo.Store().PublicKeyHex(),
	})
}
