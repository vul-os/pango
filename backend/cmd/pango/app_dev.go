//go:build !embed_frontend

package main

// Development build: the app is served from the repo-root dist/ if it exists.
//
// Returning nil when there is no build degrades to "that route is not
// registered" rather than a startup failure, so a checkout without a Node
// toolchain — and CI running Go tests — still builds and runs.

import (
	"net/http"
	"os"
	"path/filepath"
)

// newAppHandler serves the repo-root dist/ directory when present, else nil.
func newAppHandler() http.Handler {
	dir := findDist()
	if dir == "" {
		return nil
	}
	return spaHandler(http.Dir(dir))
}

// findDist walks up from the working directory looking for a dist/ directory
// containing index.html, so the server runs the same from backend/,
// backend/cmd/pango/ or the repo root.
func findDist() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "dist")
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
