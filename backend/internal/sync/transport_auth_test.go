package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vul-os/pango/backend/internal/store"
)

// newSignedOpsRequest builds a real, correctly-signed POST /api/sync/ops
// request from caller's engine against baseURL, for tests that want to drive
// the full transport instead of calling unexported pieces directly.
func newSignedOpsRequest(t *testing.T, caller *Engine, baseURL string, ops []store.Op) *http.Request {
	t.Helper()
	buf, err := json.Marshal(opsMsg{Ops: ops})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), "POST", baseURL+"/api/sync/ops", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	caller.signRequest(req, buf)
	return req
}

// TestUnenrolledPeerRejectedByDefault: a caller with no shared secret and no
// prior enrolment gets nothing — the mesh fails closed.
func TestUnenrolledPeerRejectedByDefault(t *testing.T) {
	a := newNode(t)
	srvA := a.server(t)

	stranger := newNode(t)
	stranger.e.SecretFn = func() string { return "" } // knows no secret at all

	req := newSignedOpsRequest(t, stranger.e, srvA.URL, nil)
	resp, err := stranger.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unenrolled peer: got HTTP %d, want 401", resp.StatusCode)
	}
}

// TestUnenrolledPeerWrongSecretRejected: knowing a WRONG secret is exactly as
// useless as knowing none.
func TestUnenrolledPeerWrongSecretRejected(t *testing.T) {
	a := newNode(t)
	srvA := a.server(t)

	stranger := newNode(t)
	stranger.e.SecretFn = func() string { return "not-the-right-secret" }

	req := newSignedOpsRequest(t, stranger.e, srvA.URL, nil)
	resp, err := stranger.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-secret peer: got HTTP %d, want 401", resp.StatusCode)
	}
}

// TestEnrolledPeerAccepted is the positive control for the two rejection
// tests above: the right secret pairs successfully and a subsequent request
// succeeds by key alone (no header changes needed, but proves enrolment
// stuck).
func TestEnrolledPeerAccepted(t *testing.T) {
	a := newNode(t)
	a.e.OrgIDFn = func() (string, error) { return "test-org", nil }
	srvA := a.server(t)
	b := newNode(t) // shares testSecret via newNode's default SecretFn

	req := newSignedOpsRequest(t, b.e, srvA.URL, nil)
	resp, err := b.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readAll(resp)
		t.Fatalf("enrolled peer: got HTTP %d (%s), want 200", resp.StatusCode, body)
	}

	enrolled, err := a.e.peerEnrolled(b.e.NodeID())
	if err != nil {
		t.Fatal(err)
	}
	if !enrolled {
		t.Error("b's key was not recorded as an enrolled peer after a successful TOFU request")
	}
}

// TestReplayedNonceRejected: replaying an identical, still-fresh request must
// be rejected the second time even though the signature is perfectly valid.
func TestReplayedNonceRejected(t *testing.T) {
	a := newNode(t)
	a.e.OrgIDFn = func() (string, error) { return "test-org", nil }
	srvA := a.server(t)
	b := newNode(t)

	buf, _ := json.Marshal(opsMsg{})
	req1, _ := http.NewRequestWithContext(context.Background(), "POST", srvA.URL+"/api/sync/ops", bytes.NewReader(buf))
	req1.Header.Set("Content-Type", "application/json")
	b.e.signRequest(req1, buf)

	// Clone the exact same headers onto a second request with a fresh body
	// reader (the first request consumes its own).
	req2, _ := http.NewRequestWithContext(context.Background(), "POST", srvA.URL+"/api/sync/ops", bytes.NewReader(buf))
	req2.Header = req1.Header.Clone()

	resp1, err := b.e.client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request: got HTTP %d, want 200", resp1.StatusCode)
	}

	resp2, err := b.e.client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed request: got HTTP %d, want 401", resp2.StatusCode)
	}
}

// TestStaleTimestampRejected: a request signed far outside the ±300s window
// is rejected even with an otherwise-valid signature.
func TestStaleTimestampRejected(t *testing.T) {
	a := newNode(t)
	srvA := a.server(t)
	b := newNode(t)

	buf, _ := json.Marshal(opsMsg{})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srvA.URL+"/api/sync/ops", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")

	staleTS := strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10)
	nonce := store.NewID()
	base := sigBase(req.Method, req.URL.Path, bodyHashHex(buf), staleTS, nonce)
	req.Header.Set(hdrKey, b.e.s.PublicKeyHex())
	req.Header.Set(hdrTimestamp, staleTS)
	req.Header.Set(hdrNonce, nonce)
	req.Header.Set(hdrSig, b.e.s.Sign(base))
	req.Header.Set("Authorization", "Bearer "+testSecret)

	resp, err := b.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale timestamp: got HTTP %d, want 401", resp.StatusCode)
	}
}

// TestTransportTamperDetected: flipping a byte in a signed body invalidates
// the envelope signature (it covers a hash of the body), so a MITM cannot
// alter a batch in flight without detection at the transport layer.
func TestTransportTamperDetected(t *testing.T) {
	a := newNode(t)
	srvA := a.server(t)
	b := newNode(t)

	ops := []store.Op{{HLC: "0000000000001-0000-abc", Author: "abc", OrgID: "nonexistent-org", Tbl: "job", RowID: "x", Payload: []byte(`{}`)}}
	buf, _ := json.Marshal(opsMsg{Ops: ops})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srvA.URL+"/api/sync/ops", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	b.e.signRequest(req, buf) // signs the ORIGINAL buf

	// Now tamper: send a body that differs from what was signed.
	tampered := bytes.Replace(buf, []byte("nonexistent-org"), []byte("different-org!!"), 1)
	req.Body = readNopCloser(tampered)
	req.ContentLength = int64(len(tampered))

	resp, err := b.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered body: got HTTP %d, want 401 (body hash must not match the signed envelope)", resp.StatusCode)
	}
}

// TestOpBatchSignatureTamperDetected exercises the second, independent
// signature layer: the batch itself is signed (defence in depth), so even a
// party that can forge a valid transport envelope (because it knows a
// pairing secret or holds an enrolled key) cannot silently substitute a
// different set of ops into an otherwise-authentic-looking batch that claims
// a batch signature.
func TestOpBatchSignatureTamperDetected(t *testing.T) {
	a := newNode(t)
	a.e.OrgIDFn = func() (string, error) { return "test-org", nil }
	b := newNode(t)

	realOps := []store.Op{{HLC: "0000000000001-0000-abc", Author: "abc", OrgID: "o1", Tbl: "job", RowID: "x", Payload: []byte(`{}`)}}
	signedBody, _ := json.Marshal(realOps)
	sig := b.e.s.Sign(signedBody)

	// An attacker swaps in different ops but keeps the original signature
	// and pubkey fields.
	forgedOps := []store.Op{{HLC: "0000000000002-0000-abc", Author: "abc", OrgID: "o1", Tbl: "job", RowID: "y", Payload: []byte(`{}`)}}
	msg := opsMsg{Ops: forgedOps, PubKey: b.e.s.PublicKeyHex(), Sig: sig}
	buf, _ := json.Marshal(msg)

	var decoded opsMsg
	if err := json.Unmarshal(buf, &decoded); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(decoded.Ops)
	if store.VerifySig(decoded.PubKey, body, decoded.Sig) {
		t.Fatal("batch signature verified against tampered ops — it must not")
	}

	// Drive it through the real handler too, to prove the server itself
	// rejects it (not just the raw VerifySig check above).
	srvA := a.server(t)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srvA.URL+"/api/sync/ops", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	b.e.signRequest(req, buf) // the OUTER transport envelope is honestly signed over this exact (tampered) body...
	resp, err := b.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// ...but the INNER batch signature no longer matches its ops, so the
	// handler must still reject it.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged batch: got HTTP %d, want 400", resp.StatusCode)
	}
}

func readAll(resp *http.Response) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	sb.Write(buf[:n])
	return sb.String(), err
}

func readNopCloser(b []byte) *nopReadCloser { return &nopReadCloser{bytes.NewReader(b)} }

type nopReadCloser struct{ *bytes.Reader }

func (nopReadCloser) Close() error { return nil }
