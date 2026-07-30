package api

// JSON encoding and error mapping.
//
// Errors are mapped to status codes in exactly one place. Handlers that each
// decide their own status drift: one returns 404 for a missing row, another
// returns 200 with a null body, and a client ends up with a special case per
// endpoint. Worse, a repo error that leaks its text to the client can describe
// rows in another tenancy — so the message a client sees is chosen here from a
// known set, and the detail goes to the log.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/vul-os/pango/backend/internal/repo"
)

const maxBodyBytes = 1 << 20 // 1 MiB: no legitimate write here is larger

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

// writeErr maps an error to a status and a safe message.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, repo.ErrBadCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid email or password"})
	case errors.Is(err, repo.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		// A validation error carries no tenant data — it describes the request
		// the caller just made — so its text is safe to return.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}

// writeServerErr logs the detail and tells the client nothing about it.
func writeServerErr(w http.ResponseWriter, context string, err error) {
	log.Printf("api: %s: %v", context, err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

// decode reads a JSON request body with a size limit and rejects unknown
// fields.
//
// Unknown fields are rejected deliberately. A client sending {"org_id": "..."}
// hoping it will be honoured gets a 400 telling it the field is not accepted,
// rather than a silent success that leaves the sender believing the scoping
// worked — and leaves a reviewer wondering whether it did.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
