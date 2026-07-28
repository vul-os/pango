// Package sync implements PropFix's leaderless, peer-to-peer replication
// (ARCHITECTURE.md §7, docs/SYNC.md). There is no central server: every node
// serves sync requests over its own HTTP port and can dial peers. A round is
// stateless and symmetric — push what the peer lacks, then pull what we
// lack — so any topology works (pair, hub-and-spoke, mesh), only one side of
// a pair needs to be reachable, and any node can relay any other node's ops
// because ops are self-ordering and idempotent to apply.
//
// Every sync request is authenticated by a mutual Ed25519 signature over a
// canonical request envelope (transport_auth.go). PropFix has no node
// identifier separate from its identity key — ARCHITECTURE.md §7 is explicit
// that "a node's id is its public key" — so the transport identifies a peer
// by the same key that signs its requests and mints its ops, rather than by a
// decoupled node id the way some sibling products do.
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vul-os/propfix/backend/internal/store"
)

// Batch bounds how many ops travel per request and per push/pull round leg
// (docs/SYNC.md §8, docs/WRAP.md §11.1.1 uses the same cap for symmetry).
const Batch = 2000

// Engine wires a Store to the peer transport: HTTP push/pull rounds and the
// folder (file) transport, both driving the same idempotent apply path
// (ops.go, apply.go).
type Engine struct {
	s *store.Store

	// SecretFn returns the current pairing secret, or "" to disable pairing
	// entirely (no new peer can ever enrol). Read on every request, so
	// rotating the secret takes effect immediately without a restart.
	SecretFn func() string
	// AllowSecretFallback lets a caller authenticate with the bare secret and
	// no signed envelope at all. Default false: a request with no signature
	// is rejected outright, key authentication is mandatory, and the mesh
	// fails closed (docs/SYNC.md §8).
	AllowSecretFallback bool
	// OrgIDFn resolves which organisation owns a newly TOFU-enrolled peer
	// row. If nil, a node holding exactly one organisation uses it
	// automatically; a node with zero or several fails enrolment closed
	// rather than guessing which portfolio a strange key belongs to.
	OrgIDFn func() (string, error)
	// FolderFn, if set, returns the shared folder path for the file
	// transport (folder.go). Empty return disables it. Read on every round.
	FolderFn func() string

	client *http.Client
	nonces *nonceCache
	mu     sync.Mutex // serialises outbound rounds and folder sync
}

// New builds an Engine over an open store. Callers wire SecretFn (and
// optionally OrgIDFn, AllowSecretFallback, FolderFn) before serving traffic.
func New(s *store.Store) *Engine {
	return &Engine{
		s:      s,
		client: &http.Client{Timeout: 20 * time.Second},
		nonces: newNonceCache(),
	}
}

// NodeID is this node's identity on the wire: its Ed25519 public key, hex
// encoded. There is no separate node identifier to keep in sync with it.
func (e *Engine) NodeID() string { return e.s.PublicKeyHex() }

type opsMsg struct {
	Ops []store.Op `json:"ops"`
	// PubKey + Sig sign the batch (Ed25519 over the marshaled ops) as
	// defence in depth, so a relayed batch stays attributable and
	// tamper-evident on its own even though the request envelope
	// (transport_auth.go) already binds the whole body to the caller's key.
	PubKey string `json:"pubkey,omitempty"`
	Sig    string `json:"sig,omitempty"`
}

type pullReq struct {
	Vector map[string]string `json:"vector"`
}

type vectorResp struct {
	NodeID string            `json:"node_id"`
	PubKey string            `json:"pubkey"`
	Vector map[string]string `json:"vector"`
}

// Handler returns the /api/sync/* routes. Every route but the unauthenticated
// liveness ping is wrapped in guard, which enforces mutual key
// authentication (transport_auth.go).
func (e *Engine) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/sync/ping", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "propfix")
	})
	mux.HandleFunc("GET /api/sync/vector", e.guard(e.handleVector))
	mux.HandleFunc("POST /api/sync/ops", e.guard(e.handleOps))
	mux.HandleFunc("POST /api/sync/pull", e.guard(e.handlePull))

	return mux
}

func (e *Engine) handleVector(w http.ResponseWriter, r *http.Request) {
	vec, err := e.s.Vector()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, vectorResp{NodeID: e.NodeID(), PubKey: e.s.PublicKeyHex(), Vector: vec})
}

func (e *Engine) handleOps(w http.ResponseWriter, r *http.Request) {
	var msg opsMsg
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(msg.Ops) > Batch {
		http.Error(w, "batch exceeds the maximum of 2000 ops", http.StatusBadRequest)
		return
	}
	// The batch signature, if present, must verify — tamper-evidence
	// independent of the transport envelope that already covers this body.
	if msg.Sig != "" || msg.PubKey != "" {
		body, _ := json.Marshal(msg.Ops)
		if !store.VerifySig(msg.PubKey, body, msg.Sig) {
			http.Error(w, "op batch signature invalid", http.StatusBadRequest)
			return
		}
	}
	applied, err := e.ApplyOps(msg.Ops)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"applied": applied})
}

func (e *Engine) handlePull(w http.ResponseWriter, r *http.Request) {
	var req pullReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Vector == nil {
		req.Vector = map[string]string{}
	}
	ops, err := e.OpsAfter(req.Vector, Batch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, opsMsg{Ops: ops, PubKey: e.s.PublicKeyHex(), Sig: signBatch(e.s, ops)})
}

func signBatch(s *store.Store, ops []store.Op) string {
	body, _ := json.Marshal(ops)
	return s.Sign(body)
}

// Result reports the outcome of syncing one peer.
type Result struct {
	PeerURL string `json:"peer_url"`
	OK      bool   `json:"ok"`
	Pushed  int    `json:"pushed"`
	Pulled  int    `json:"pulled"`
	Error   string `json:"error,omitempty"`
}

// SyncPeer runs one stateless round against a peer's base URL: push what it
// lacks, then pull what we lack. Only one side of the pair needs to be
// reachable — the caller always dials out, so a tablet behind CGNAT syncs by
// dialing the office, never the other way round.
func (e *Engine) SyncPeer(ctx context.Context, baseURL string) Result {
	res := Result{PeerURL: baseURL}
	base := strings.TrimRight(baseURL, "/")

	peerVec, err := e.fetchVector(ctx, base)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	window := peerVec
	for {
		ops, err := e.OpsAfter(window, Batch)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if len(ops) == 0 {
			break
		}
		for _, op := range ops {
			if op.HLC > window[op.Author] {
				window[op.Author] = op.HLC
			}
		}
		if err := e.postOps(ctx, base, ops); err != nil {
			res.Error = err.Error()
			return res
		}
		res.Pushed += len(ops)
		if len(ops) < Batch {
			break
		}
	}

	for {
		myVec, err := e.s.Vector()
		if err != nil {
			res.Error = err.Error()
			return res
		}
		ops, err := e.pull(ctx, base, myVec)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if len(ops) == 0 {
			break
		}
		n, err := e.ApplyOps(ops)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Pulled += n
		if len(ops) < Batch {
			break
		}
	}

	res.OK = true
	return res
}

func (e *Engine) fetchVector(ctx context.Context, base string) (map[string]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/sync/vector", nil)
	e.signRequest(req, nil)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vector: HTTP %d (%s)", resp.StatusCode, statusText(resp))
	}
	var body vectorResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Vector == nil {
		body.Vector = map[string]string{}
	}
	return body.Vector, nil
}

func (e *Engine) postOps(ctx context.Context, base string, ops []store.Op) error {
	body, _ := json.Marshal(ops)
	buf, _ := json.Marshal(opsMsg{
		Ops:    ops,
		PubKey: e.s.PublicKeyHex(),
		Sig:    e.s.Sign(body),
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/sync/ops", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	e.signRequest(req, buf)
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("push: HTTP %d (%s)", resp.StatusCode, statusText(resp))
	}
	return nil
}

func (e *Engine) pull(ctx context.Context, base string, vec map[string]string) ([]store.Op, error) {
	buf, _ := json.Marshal(pullReq{Vector: vec})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/api/sync/pull", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	e.signRequest(req, buf)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pull: HTTP %d (%s)", resp.StatusCode, statusText(resp))
	}
	var msg opsMsg
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, err
	}
	if msg.Sig != "" || msg.PubKey != "" {
		body, _ := json.Marshal(msg.Ops)
		if !store.VerifySig(msg.PubKey, body, msg.Sig) {
			return nil, fmt.Errorf("pull: response batch signature invalid")
		}
	}
	return msg.Ops, nil
}

func statusText(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return strings.TrimSpace(string(b))
}

// SyncAll runs a round against every URL in peers, plus the folder transport
// if FolderFn is configured, and returns one Result per peer.
func (e *Engine) SyncAll(ctx context.Context, peers []string) []Result {
	e.mu.Lock()
	defer e.mu.Unlock()

	var results []Result
	for _, url := range peers {
		if strings.TrimSpace(url) == "" {
			continue
		}
		results = append(results, e.SyncPeer(ctx, url))
	}
	if e.FolderFn != nil {
		if dir := e.FolderFn(); dir != "" {
			e.FolderSync(dir)
		}
	}
	return results
}

// RunBackground syncs peers (resolved fresh from peersFn on every tick, so
// enrolling or disabling a peer takes effect on the next interval) until ctx
// is cancelled.
func (e *Engine) RunBackground(ctx context.Context, interval time.Duration, peersFn func() []string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.SyncAll(ctx, peersFn())
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
