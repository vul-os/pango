// Package repo is the persistence layer: one file per aggregate, each owning
// its own SQL. There is no generic repository, no query builder and no god
// service (§9) — logic lives with its aggregate.
//
// The single rule every function in this package obeys: org_id is a parameter,
// it comes from the authenticated session, and it appears in the WHERE clause
// of every read and the values of every write. The legacy system took its
// organisation filter from a query parameter the frontend supplied, which meant
// tenant isolation was a client-side convention — anyone who could edit a URL
// could read another agency's portfolio. Isolation that a client can choose is
// not isolation (§11).
package repo

import (
	"database/sql"
	"errors"

	"github.com/vul-os/pango/backend/internal/store"
)

// ErrNotFound is returned when a row does not exist, or exists in a different
// organisation.
//
// Those two cases deliberately return the same error. Distinguishing them would
// turn any id-guessing loop into an existence oracle across tenancy boundaries:
// "403 forbidden" tells an attacker the id is real and belongs to somebody.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a write would violate an invariant that another
// write already established — a duplicate unit key, an illegal status
// transition.
var ErrConflict = errors.New("conflict")

// Repo owns the database handle and the journal. Safe for concurrent use.
type Repo struct {
	s *store.Store
}

// New builds a Repo over an open store.
func New(s *store.Store) *Repo { return &Repo{s: s} }

// Store exposes the underlying store for layers that legitimately need the
// clock or the node identity.
func (r *Repo) Store() *store.Store { return r.s }

// DB exposes the handle for the report layer's aggregate queries.
func (r *Repo) DB() *sql.DB { return r.s.DB() }

// nullStr converts a nullable text column to a plain string. Optional foreign
// keys (a job with no assignee, an inspection of common property with no unit)
// are NULL rather than ” so the foreign key constraint stays enforceable —
// SQLite checks FK references on ” but not on NULL.
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// nullable turns "" into a NULL argument, for the same reason.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
