package main

// Single-page-app file serving.
//
// A client-side router owns paths like /jobs/01H… that do not exist as files.
// A plain http.FileServer 404s them, which means a deep link or a page refresh
// lands the user on "404 page not found" even though the route is valid — the
// classic SPA deployment bug, and one that only shows up after someone shares
// a link.
//
// So: serve the file if it exists, otherwise serve index.html and let the
// router decide. API paths never reach here (they are matched by a more
// specific mux pattern), so a mistyped /api/… still 404s as JSON rather than
// being handed an HTML page.

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves static files from fsys, falling back to index.html so that
// client-side routes resolve on deep link and refresh.
func spaHandler(fsys http.FileSystem) http.Handler {
	files := http.FileServer(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + r.URL.Path)

		// Hashed asset bundles must 404 honestly rather than fall back to
		// index.html — an HTML body served as .js fails in a way that is far
		// harder to diagnose than a missing file.
		if strings.HasPrefix(name, "/assets/") {
			files.ServeHTTP(w, r)
			return
		}

		if f, err := fsys.Open(name); err == nil {
			defer f.Close()
			if st, err := f.Stat(); err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		} else if !isNotExist(err) {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		index, err := fsys.Open("/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()
		st, err := index.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", st.ModTime(), index.(interface {
			Seek(int64, int) (int64, error)
			Read([]byte) (int, error)
		}))
	})
}

func isNotExist(err error) bool {
	return err == fs.ErrNotExist || strings.Contains(err.Error(), "no such file") ||
		strings.Contains(err.Error(), "file does not exist")
}
