package sync

// Mutual Ed25519 transport authentication for the node<->node sync mesh
// (docs/SYNC.md §8).
//
// Every sync request carries a signature over a canonical envelope — method,
// path, body hash, timestamp and nonce — made with the caller's node identity
// key. Because a PropFix node's id IS its public key (ARCHITECTURE.md §7),
// there is no separate "node id" to look up: the header that carries the
// caller's claimed identity is the same key the signature is verified
// against, and enrolment is a row in the `peer` table keyed by that pubkey.
//
// The shared secret is retained for exactly one role: PAIRING BOOTSTRAP. A
// node presenting a key with no enrolled peer row proves it knows the secret,
// which authorises recording (TOFU) its key. From then on it authenticates by
// key alone, and the secret is no longer consulted for it. An optional
// AllowSecretFallback (default off) additionally accepts a request that
// carries no signed envelope at all, gated purely on the secret; with the
// default off, an unsigned request is rejected outright and the mesh fails
// closed.
//
// Revocation: deleting a peer row (repo.DeletePeer, out of this package's
// ownership) removes it from the enrolled set, so the key no longer
// authenticates. Full revocation is deletion plus rotating the pairing
// secret, since the secret alone would otherwise let the same key re-enrol.
//
// DUPLICATION: FlowStock has a file of the same name and purpose. This one is
// NOT a copy of it — it is the redesign (one identity value instead of a
// node-id/pubkey pair, secret as bootstrap only, TOFU writing a deletable peer
// row). See docs/SYNC.md §11 ("Duplicated sync substrate"). Do not "align" it
// back to the sibling's shape; the convergence runs the other way.

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vul-os/propfix/backend/internal/store"
)

const (
	hdrKey       = "X-PF-Key"
	hdrTimestamp = "X-PF-Timestamp"
	hdrNonce     = "X-PF-Nonce"
	hdrSig       = "X-PF-Sig"

	// authSkew is the tolerated clock skew for a request timestamp (±300s,
	// docs/SYNC.md §8).
	authSkew = 300 * time.Second
	// maxSyncBody caps a sync request body (it must be read fully to hash
	// it before the handler runs).
	maxSyncBody = 64 << 20
)

// nonceCache remembers recently seen nonces per key, TTL'd at twice the
// freshness window — past that point a replay would be rejected as stale
// anyway, so the cache never needs to remember longer than that.
type nonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newNonceCache() *nonceCache { return &nonceCache{seen: map[string]time.Time{}} }

func (c *nonceCache) checkAndAdd(key string, ttl time.Duration) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, exp := range c.seen { // lazy prune
		if now.After(exp) {
			delete(c.seen, k)
		}
	}
	if exp, ok := c.seen[key]; ok && now.Before(exp) {
		return false
	}
	c.seen[key] = now.Add(ttl)
	return true
}

func bodyHashHex(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// sigBase is the canonical string signed by the caller and verified by the
// responder. Every field is bound in, so a signature cannot be replayed
// against a different method, path or body, or outside its freshness window.
func sigBase(method, path, bodyHash, ts, nonce string) []byte {
	return []byte(method + "\n" + path + "\n" + bodyHash + "\n" + ts + "\n" + nonce)
}

// bearerOK reports whether the request presents the current pairing secret
// with the required "Bearer " scheme.
func (e *Engine) bearerOK(r *http.Request) bool {
	secret := ""
	if e.SecretFn != nil {
		secret = e.SecretFn()
	}
	if secret == "" {
		return false
	}
	presented, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) == 1
}

// peerEnrolled reports whether pubkeyHex has an enabled row in `peer`. It
// reads the table directly rather than through repo/peer.go, which owns the
// operator-facing CRUD for that table; this package only ever needs a
// pubkey membership check and, on first contact, a row to insert.
func (e *Engine) peerEnrolled(pubkeyHex string) (bool, error) {
	var n int
	err := e.s.DB().QueryRow(
		`SELECT COUNT(*) FROM peer WHERE pubkey = ? AND enabled = 1`, pubkeyHex).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// enrollPeer records a newly bootstrapped peer's key as an inbound-only row
// (pubkey set, no URL — this node was paired with, not told to dial anyone).
func (e *Engine) enrollPeer(pubkeyHex string) error {
	orgID, err := e.resolveOrgID()
	if err != nil {
		return err
	}
	short := pubkeyHex
	if len(short) > 12 {
		short = short[:12]
	}
	_, err = e.s.DB().Exec(
		`INSERT INTO peer (id, org_id, name, url, pubkey, enabled, last_sync_at, last_status, created_at)
		 VALUES (?, ?, ?, '', ?, 1, '', 'paired via TOFU', ?)`,
		store.NewID(), orgID, "peer-"+short, pubkeyHex, store.Now())
	return err
}

// resolveOrgID decides which organisation a freshly-paired peer belongs to.
// With OrgIDFn unset, a node holding exactly one organisation uses it; a node
// with zero or several fails enrolment rather than guessing a portfolio for a
// key it has never seen.
func (e *Engine) resolveOrgID() (string, error) {
	if e.OrgIDFn != nil {
		return e.OrgIDFn()
	}
	rows, err := e.s.DB().Query(`SELECT id FROM organisation WHERE deleted = 0`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("cannot auto-enrol a peer: %d organisations on this node", len(ids))
	}
	return ids[0], nil
}

// verifyRequest authenticates one inbound sync request against body. It
// returns ok plus, on failure, a short client-facing reason.
func (e *Engine) verifyRequest(r *http.Request, body []byte) (bool, string) {
	key := r.Header.Get(hdrKey)
	ts := r.Header.Get(hdrTimestamp)
	nonce := r.Header.Get(hdrNonce)
	sig := r.Header.Get(hdrSig)

	signed := key != "" && sig != "" && ts != "" && nonce != ""
	if !signed {
		if !e.AllowSecretFallback {
			return false, "key authentication required: no signed envelope presented"
		}
		if !e.bearerOK(r) {
			return false, "unauthorized"
		}
		return true, ""
	}

	tsec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false, "bad request timestamp"
	}
	if d := time.Since(time.Unix(tsec, 0)); d > authSkew || d < -authSkew {
		return false, "stale request timestamp (clock skew beyond ±300s)"
	}

	base := sigBase(r.Method, r.URL.Path, bodyHashHex(body), ts, nonce)
	if !store.VerifySig(key, base, sig) {
		return false, "request signature invalid"
	}
	if !e.nonces.checkAndAdd(key+"|"+nonce, 2*authSkew) {
		return false, "replayed request nonce"
	}

	enrolled, err := e.peerEnrolled(key)
	if err != nil {
		return false, "internal error"
	}
	if !enrolled {
		if !e.bearerOK(r) {
			return false, "unenrolled peer: a valid pairing secret is required to enrol a key"
		}
		if err := e.enrollPeer(key); err != nil {
			return false, "enrolment failed: " + err.Error()
		}
	}
	return true, ""
}

// guard wraps a sync handler with transport authentication. It reads the
// body (needed to hash it), verifies, then restores the body for the
// handler.
func (e *Engine) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxSyncBody))
		if err != nil {
			http.Error(w, "could not read request body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		if ok, reason := e.verifyRequest(r, body); !ok {
			http.Error(w, reason, http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next(w, r)
	}
}

// signRequest signs an outbound sync request with this node's identity key.
// body must be the exact bytes sent (nil for a bodyless GET).
//
// It also attaches the pairing secret as a bearer token, if one is
// configured. The secret is only ever consulted by the responder when this
// node's key is not yet enrolled there (TOFU bootstrap) or, with
// AllowSecretFallback, when no signed envelope is presented at all; sending
// it alongside a signature on every request is harmless once enrolled and is
// what lets the very first request pair successfully.
func (e *Engine) signRequest(req *http.Request, body []byte) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := store.NewID()
	base := sigBase(req.Method, req.URL.Path, bodyHashHex(body), ts, nonce)
	req.Header.Set(hdrKey, e.s.PublicKeyHex())
	req.Header.Set(hdrTimestamp, ts)
	req.Header.Set(hdrNonce, nonce)
	req.Header.Set(hdrSig, e.s.Sign(base))
	if e.SecretFn != nil {
		if secret := e.SecretFn(); secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}
}
