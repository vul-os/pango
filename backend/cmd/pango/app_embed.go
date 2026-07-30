//go:build embed_frontend

package main

// Embedded build: the built React app travels inside the binary.
//
// This is the other half of "one file is a complete install" (§1). site_embed.go
// carries the marketing site; this carries the application itself. Without it
// the binary serves an API and a landing page but not the product, which is a
// confusing thing to hand somebody.
//
// The build script copies the Vite output next to this file before compiling; a
// plain `go build ./...` uses app_dev.go and serves from disk instead, so the
// backend stays buildable without a Node toolchain.

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
)

//go:embed all:dist
var appFS embed.FS

// newAppHandler serves the embedded single-page app, or nil if unavailable.
func newAppHandler() http.Handler {
	sub, err := fs.Sub(appFS, "dist")
	if err != nil {
		log.Printf("pango: embedded app not found: %v", err)
		return nil
	}
	return spaHandler(http.FS(sub))
}
