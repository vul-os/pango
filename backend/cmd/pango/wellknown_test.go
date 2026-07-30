package main

// Defect coverage: /.well-known/* must 404 honestly when nothing registers it,
// rather than falling through to the SPA catch-all and coming back as
// index.html with a 200. A client probing whether this node speaks a
// well-known protocol (WRAP's identity announcement is the one this binary
// ships) deserves a clean "no" — not HTML dressed as success. See the comment
// on buildMux's /.well-known/ registration in main.go.

import (
	"net/http/httptest"
	"testing"

	"github.com/vul-os/pango/backend/internal/api"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/store"
)

func TestWellKnownNotFoundWhenWrapDisabled(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := api.New(repo.New(s), "test")

	mux := buildMux(srv, false, s.PublicKeyHex())

	for _, path := range []string{
		"/.well-known/wrap/identity",
		"/.well-known/anything-else",
		"/.well-known/",
	} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != 404 {
			t.Errorf("%s: got HTTP %d, want 404 (WRAP is disabled)", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct == "text/html; charset=utf-8" {
			t.Errorf("%s: got an HTML body (the SPA index page) instead of a real 404", path)
		}
	}
}

// The identity route itself must still work when WRAP is on, and everything
// else under .well-known/ must still 404 rather than fall through.
func TestWellKnownIdentityServedWhenWrapEnabled(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	srv := api.New(repo.New(s), "test")

	mux := buildMux(srv, true, s.PublicKeyHex())

	req := httptest.NewRequest("GET", "/.well-known/wrap/identity", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("wrap identity: got HTTP %d, want 200 (WRAP is enabled)", rec.Code)
	}

	req = httptest.NewRequest("GET", "/.well-known/something-wrap-does-not-define", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("unrelated .well-known path: got HTTP %d, want 404 even with WRAP enabled", rec.Code)
	}
}
