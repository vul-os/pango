package wrap

// TestConformanceVectors drives github.com/vul-os/wrap's conformance vector
// set (14-conformance.md §15, conformance/wrap_vectors.json) against this
// package. WRAP 14-conformance.md §15.1: the vectors are normative and take
// precedence over the prose; a conformance claim requires all vectors pass,
// with no silent skips.
//
// This package is Pango's narrow WRAP binding — object encoding, content
// addressing, signing, decode-time verification, and the one authorship rule
// (Assignment-by-issuer) it needs for its own trades/v0 mapping. It does NOT
// implement WRAP's HLC, merge algebra, lifecycle fold, or fulfilment-proof
// verification — those live in Pango's own store/domain layers using its
// own oplog CRDT, not in this package. Vectors in the groups listed in
// notCoveredGroups below are therefore marked NOT-COVERED here, not silently
// passed and not silently dropped: every vector in the file gets a subtest,
// and every subtest's outcome is one of PASS, SKIP (not-covered, by design),
// or SKIP (documented gap — the implementation disagrees with the spec; see
// the reject-oversize case, which currently complies and is kept as this
// category's tripwire rather than as a live gap).
//
// WHERE THE VECTORS COME FROM, AND WHAT HAPPENS WHEN THEY ARE ABSENT
//
// The vectors are NOT vendored here: they are loaded from a sibling checkout
// of github.com/vul-os/wrap (../../../../wrap/conformance/wrap_vectors.json
// from this package), or from WRAP_VECTORS_PATH. A pango-only checkout
// therefore has nothing to run.
//
// That absence used to be a bare t.Skip, which `go test ./...` renders as a
// plain "ok" — the conformance claim evaporated with no output at all. It now
// prints a loud banner naming every group that went unverified, and
// WRAP_VECTORS_REQUIRED=1 turns the skip into a failure so CI can insist.
//
// Vendoring a copy with a provenance hash is the better answer and is NOT done
// here, because it cannot be done honestly from a checkout that does not have
// the upstream file: a vendored copy nobody can diff against its source is
// worse than no copy. The hook is in place for whoever does it —
// WRAP_VECTORS_SHA256 pins the digest of whatever file is loaded, and the
// digest is printed on every run either way.
import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ── what this harness claims to cover ────────────────────────────────────
//
// These lists are the coverage assertion. Every id below is dispatched by name
// in runVector; if upstream renames or drops one, the harness would otherwise
// keep passing while silently verifying less. Presence is checked on load and
// the outcome is checked after the run.

// verifiedVectorIDs must be present AND end the run in PASS.
var verifiedVectorIDs = []string{
	"id-excludes-key-3",
	"sign-preimage-construction",
	"sign-verify-pass",
	"sign-verify-fail-tampered",
	"sign-verify-fail-wrong-claimed-author",
	"reject-forbidden-key",
	"reject-non-canonical-minimal",
	"reject-non-canonical-realistic",
	"reject-unsupported-version",
	"authorship-assignment-by-issuer-accepted",
	"authorship-assignment-by-performer-rejected",
	"authorship-assignment-by-third-party-rejected",
	"authorship-bid-by-any-principal-accepted",
	"tiebreak-is-pure-string-comparison",
	"forward-unknown-kind-ignored",
	"forward-unknown-field-preserved-through-reencode",
	"forward-unknown-profile-stored-not-rendered",
}

// knownGapVectorIDs must be present, and may end in PASS or GAP — GAP while
// the implementation still disagrees with the spec, PASS once it stops.
var knownGapVectorIDs = []string{
	"reject-oversize",
}

// acknowledgedVectorIDs must be present and are deliberately NOT verified.
// Listing them here is what stops "not covered" from quietly becoming "not
// present, so not our problem".
var acknowledgedVectorIDs = []string{
	"reject-bad-id",
}

// minVectorsByGroup floors the groups runVector dispatches by shape rather
// than by id (encode picks on `object` vs `value`, id falls through to a
// default), so those cannot be pinned by name.
var minVectorsByGroup = map[string]int{
	"encode": 1,
	"id":     2,
}

// notCoveredGroups are the WRAP chapters internal/wrap does not implement at
// all. The reason is printed for each one, both in the summary and in the
// absent-vectors banner, so "we pass conformance" is never readable as more
// than it is.
var notCoveredGroups = map[string]string{
	"hlc":    "internal/wrap implements no HLC mint/observe logic (that lives in Pango's internal/store.HLC, a separate package with its own tests and its own vector file at conformance/hlc_vectors.json, not exercised by this WRAP binding)",
	"merge":  "internal/wrap implements no merge/union/state-root logic; Pango's own CRDT oplog (internal/sync) is a separate algebra, not this package's WRAP object union",
	"fold":   "internal/wrap implements no §6.3 lifecycle fold (no state-from-object-set computation exists in this package)",
	"expiry": "internal/wrap has no lifecycle/fold engine to compute a derived `expired` state from (WorkOrder.Expires is readable as data, but nothing in this package computes state from it)",
	"proof":  "internal/wrap has no fulfilment-proof (handoff-code commitment) verification function; the commit hash in this vector was independently verified against a second, non-Go BLAKE3 implementation while authoring the vectors, but nothing in this package checks it",
}

// ── vector file schema ──────────────────────────────────────────────────

type vecKey struct {
	Seed string `json:"seed"`
	Pub  string `json:"pub"`
}

type typedValue struct {
	T       string          `json:"t"`
	V       json.RawMessage `json:"v"`
	Special string          `json:"special"`
}

type vecObject struct {
	Kind              string                `json:"kind"`
	KindHex           string                `json:"kind_hex"`
	Author            string                `json:"author"`
	AuthorPubHex      string                `json:"author_pub_hex"`
	TS                string                `json:"ts"`
	Fields            map[string]typedValue `json:"fields"`
	CanonicalBytesHex string                `json:"canonical_bytes_hex"`
	IDHex             string                `json:"id_hex"`
	SignatureHex      string                `json:"signature_hex"`
	EnvelopeHex       string                `json:"envelope_hex"`
}

type vectorsFile struct {
	WrapVersion int                  `json:"wrap_version"`
	Keys        map[string]vecKey    `json:"keys"`
	Objects     map[string]vecObject `json:"objects"`
	Vectors     []map[string]any     `json:"vectors"`
}

// ── typed-value convention (conformance/README.md) → Go values ──────────

func typedToValue(raw json.RawMessage) (any, error) {
	var tv typedValue
	if err := json.Unmarshal(raw, &tv); err != nil {
		return nil, err
	}
	switch tv.T {
	case "uint":
		var n uint64
		if err := json.Unmarshal(tv.V, &n); err != nil {
			return nil, err
		}
		return n, nil
	case "int":
		var n int64
		if err := json.Unmarshal(tv.V, &n); err != nil {
			return nil, err
		}
		return n, nil
	case "float64":
		switch tv.Special {
		case "nan":
			return math.NaN(), nil
		case "inf":
			return math.Inf(1), nil
		case "neg_inf":
			return math.Inf(-1), nil
		case "neg_zero":
			return math.Copysign(0, -1), nil
		case "":
			var f float64
			if err := json.Unmarshal(tv.V, &f); err != nil {
				return nil, err
			}
			return f, nil
		default:
			return nil, fmt.Errorf("unknown float64 special %q", tv.Special)
		}
	case "bool":
		var b bool
		if err := json.Unmarshal(tv.V, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "null":
		return nil, nil
	case "tstr":
		var s string
		if err := json.Unmarshal(tv.V, &s); err != nil {
			return nil, err
		}
		return s, nil
	case "bstr":
		var s string
		if err := json.Unmarshal(tv.V, &s); err != nil {
			return nil, err
		}
		if s == "" {
			return []byte{}, nil
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, err
		}
		return b, nil
	case "array":
		var arr []json.RawMessage
		if err := json.Unmarshal(tv.V, &arr); err != nil {
			return nil, err
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			v, err := typedToValue(e)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case "map":
		var m map[string]json.RawMessage
		if err := json.Unmarshal(tv.V, &m); err != nil {
			return nil, err
		}
		out := M{}
		for k, val := range m {
			ku, err := strconv.ParseUint(k, 10, 64)
			if err != nil {
				return nil, err
			}
			v, err := typedToValue(val)
			if err != nil {
				return nil, err
			}
			out[ku] = v
		}
		return out, nil
	case "refmap":
		var m map[string]string
		if err := json.Unmarshal(tv.V, &m); err != nil {
			return nil, err
		}
		out := RM{}
		for k, v := range m {
			out[k] = v
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown typed value t=%q", tv.T)
	}
}

// buildCanonicalBytes independently reconstructs an object's canonical CBOR
// from its logical (kind, author, ts, fields) description — NOT by trusting
// canonical_bytes_hex — so this actually exercises the encoder rather than
// merely re-hexing stored bytes.
func buildCanonicalBytes(vf *vectorsFile, name string) ([]byte, error) {
	o, ok := vf.Objects[name]
	if !ok {
		return nil, fmt.Errorf("no such object %q", name)
	}
	kindNum, err := strconv.ParseUint(strings.TrimPrefix(o.KindHex, "0x"), 16, 64)
	if err != nil {
		return nil, err
	}
	key, ok := vf.Keys[o.Author]
	if !ok {
		return nil, fmt.Errorf("no such key %q", o.Author)
	}
	authorPub, err := hex.DecodeString(key.Pub)
	if err != nil {
		return nil, err
	}
	m := M{keyV: uint64(0), keyKind: kindNum, keyAuthor: authorPub, keyTS: o.TS}
	for k, tv := range o.Fields {
		ku, err := strconv.ParseUint(k, 10, 64)
		if err != nil {
			return nil, err
		}
		v, err := typedToValue(tv.raw())
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", k, err)
		}
		m[ku] = v
	}
	return Encode(m)
}

// raw lets typedValue re-marshal itself back into json.RawMessage so
// buildCanonicalBytes can reuse typedToValue's json.RawMessage entry point.
func (tv typedValue) raw() json.RawMessage {
	b, _ := json.Marshal(tv)
	return b
}

func keyPriv(vf *vectorsFile, name string) (ed25519.PrivateKey, error) {
	k, ok := vf.Keys[name]
	if !ok {
		return nil, fmt.Errorf("no such key %q", name)
	}
	seed, err := hex.DecodeString(k.Seed)
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("key %q: seed is %d bytes, want %d", name, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func keyPub(vf *vectorsFile, name string) ([]byte, error) {
	k, ok := vf.Keys[name]
	if !ok {
		return nil, fmt.Errorf("no such key %q", name)
	}
	return hex.DecodeString(k.Pub)
}

// ── locating the vectors file ────────────────────────────────────────────

// sibling is the one search path other than WRAP_VECTORS_PATH: a checkout of
// github.com/vul-os/wrap next to this repository. (An absolute path to one
// developer's machine used to sit alongside it; on that machine it resolved to
// exactly this, and everywhere else it was noise.)
var sibling = filepath.Join("..", "..", "..", "..", "wrap", "conformance", "wrap_vectors.json")

func findVectorsFile(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("WRAP_VECTORS_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		t.Fatalf("WRAP_VECTORS_PATH=%s does not exist", p)
	}
	if _, err := os.Stat(sibling); err == nil {
		return sibling
	}
	reportUnverified(t)
	return ""
}

// reportUnverified prints what a missing vector file actually costs, then
// skips (or fails, under WRAP_VECTORS_REQUIRED=1).
//
// It writes to stderr rather than only t.Skip/t.Log because a skipped test
// prints nothing at all under a plain `go test ./...`, which is how this
// stopped verifying anything without anyone noticing. Everything named here
// is unverified until a wrap checkout is present.
func reportUnverified(t *testing.T) {
	t.Helper()
	abs, _ := filepath.Abs(sibling)

	var b strings.Builder
	fmt.Fprint(&b, "\n"+strings.Repeat("=", 78)+"\n")
	fmt.Fprintf(&b, "WRAP CONFORMANCE NOT RUN — internal/wrap is UNVERIFIED against the spec.\n")
	fmt.Fprint(&b, strings.Repeat("=", 78)+"\n")
	fmt.Fprintf(&b, "wrap_vectors.json was not found. Looked at:\n")
	fmt.Fprintf(&b, "  $WRAP_VECTORS_PATH        (unset)\n")
	fmt.Fprintf(&b, "  %s\n", abs)
	fmt.Fprintf(&b, "Fix: check out github.com/vul-os/wrap beside this repository, or set\n")
	fmt.Fprintf(&b, "WRAP_VECTORS_PATH. Set WRAP_VECTORS_REQUIRED=1 to make this a failure.\n\n")
	fmt.Fprintf(&b, "Not verified by this run (%d vectors by name, plus every vector in the\n",
		len(verifiedVectorIDs)+len(knownGapVectorIDs)+len(acknowledgedVectorIDs))
	fmt.Fprintf(&b, "encode and id groups):\n")
	for _, id := range verifiedVectorIDs {
		fmt.Fprintf(&b, "  [normally PASS] %s\n", id)
	}
	for _, id := range knownGapVectorIDs {
		fmt.Fprintf(&b, "  [known gap]     %s\n", id)
	}
	for _, id := range acknowledgedVectorIDs {
		fmt.Fprintf(&b, "  [not covered]   %s\n", id)
	}
	fmt.Fprintf(&b, "\nGroups this package does not implement at all, vectors present or not:\n")
	for _, g := range sortedKeys(notCoveredGroups) {
		fmt.Fprintf(&b, "  %s\n", g)
	}
	fmt.Fprint(&b, strings.Repeat("=", 78)+"\n")
	fmt.Fprint(os.Stderr, b.String())

	if os.Getenv("WRAP_VECTORS_REQUIRED") == "1" {
		t.Fatal("WRAP_VECTORS_REQUIRED=1 and wrap_vectors.json is absent: refusing to report a conformance run that did not happen")
	}
	t.Skip("wrap_vectors.json not found — see the banner above for exactly what went unverified")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func loadVectors(t *testing.T) *vectorsFile {
	t.Helper()
	path := findVectorsFile(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	// Provenance. The digest is logged unconditionally so a conformance claim
	// is attributable to an exact file, and pinned when WRAP_VECTORS_SHA256 is
	// set — which is what a future vendored copy would be checked against.
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	t.Logf("wrap_vectors.json: %s\n  sha256=%s", path, digest)
	if want := os.Getenv("WRAP_VECTORS_SHA256"); want != "" && want != digest {
		t.Fatalf("vector file provenance mismatch:\n  file   = %s\n  sha256 = %s\n  pinned = %s", path, digest, want)
	}

	var vf vectorsFile
	if err := json.Unmarshal(b, &vf); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(vf.Vectors) == 0 {
		t.Fatalf("%s parsed but contained zero vectors", path)
	}
	assertCoveragePresent(t, path, &vf)
	return &vf
}

// assertCoveragePresent fails when a vector this harness dispatches by name is
// missing from the file. Without it, an upstream rename silently converts a
// checked behaviour into an unchecked one and the suite still reports "ok".
func assertCoveragePresent(t *testing.T, path string, vf *vectorsFile) {
	t.Helper()
	present := map[string]bool{}
	perGroup := map[string]int{}
	for _, raw := range vf.Vectors {
		id, _ := raw["id"].(string)
		group, _ := raw["group"].(string)
		present[id] = true
		perGroup[group]++
	}
	var missing []string
	for _, ids := range [][]string{verifiedVectorIDs, knownGapVectorIDs, acknowledgedVectorIDs} {
		for _, id := range ids {
			if !present[id] {
				missing = append(missing, id)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%s no longer contains %d vector(s) this harness claims to cover: %v\n"+
			"Coverage has silently shrunk. Update the lists at the top of this file to match, "+
			"deliberately, rather than leaving them pointing at ids that are gone.", path, len(missing), missing)
	}
	for _, g := range sortedKeys(minVectorsByGroup) {
		if perGroup[g] < minVectorsByGroup[g] {
			t.Errorf("group %q has %d vectors, expected at least %d (this group is dispatched by shape, not by id, so a count is the only floor available)",
				g, perGroup[g], minVectorsByGroup[g])
		}
	}
}

// ── error-code classification (12-errors.md §13.1) ──────────────────────

func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrForbiddenKey):
		return "0x0101"
	case errors.Is(err, ErrNotCanonical):
		return "0x0102"
	case errors.Is(err, ErrBadSignature):
		return "0x0104"
	case errors.Is(err, ErrTooLarge):
		return "0x0105"
	case errors.Is(err, ErrUnsupportedVersion):
		return "0x0106"
	case errors.Is(err, ErrNotIssuer):
		return "0x0202"
	default:
		return "?"
	}
}

// ── summary bookkeeping ───────────────────────────────────────────────────

type vecResult struct {
	id, group, status, note string
}

// TestConformanceVectors is the single entry point: every vector in
// wrap_vectors.json gets one subtest. See the package doc comment above for
// how PASS / NOT-COVERED / documented-gap outcomes are reported.
func TestConformanceVectors(t *testing.T) {
	vf := loadVectors(t)
	var results []vecResult

	for _, raw := range vf.Vectors {
		id, _ := raw["id"].(string)
		group, _ := raw["group"].(string)
		t.Run(id, func(t *testing.T) {
			status, note := runVector(t, vf, raw)
			results = append(results, vecResult{id, group, status, note})
			switch status {
			case "PASS":
				// fall through: subtest passes normally.
			case "NOT_COVERED":
				t.Skip("NOT-COVERED: " + note)
			case "GAP":
				t.Skip("KNOWN GAP (implementation does not meet the spec here): " + note)
			default:
				t.Fatalf("unknown status %q", status)
			}
		})
	}

	assertCoverageOutcome(t, results)
	printSummary(t, results)
}

// assertCoverageOutcome checks that the vectors this harness claims to verify
// actually ended the run verified. Presence was checked on load; this is the
// other half — a vector that was present but landed in NOT_COVERED, or a
// documented gap that was quietly reclassified, fails here.
func assertCoverageOutcome(t *testing.T, results []vecResult) {
	t.Helper()
	status := map[string]string{}
	for _, r := range results {
		status[r.id] = r.status
	}
	for _, id := range verifiedVectorIDs {
		if status[id] != "PASS" {
			t.Errorf("vector %q is listed as verified but finished as %q", id, status[id])
		}
	}
	for _, id := range knownGapVectorIDs {
		if s := status[id]; s != "PASS" && s != "GAP" {
			t.Errorf("vector %q is a known gap; expected PASS (fixed) or GAP (still open), got %q", id, s)
		}
	}
	for _, id := range acknowledgedVectorIDs {
		if status[id] != "NOT_COVERED" {
			t.Errorf("vector %q is listed as deliberately not covered but finished as %q — "+
				"if it is covered now, move it to verifiedVectorIDs", id, status[id])
		}
	}
	passed := 0
	for _, r := range results {
		if r.status == "PASS" {
			passed++
		}
	}
	if passed < len(verifiedVectorIDs) {
		t.Errorf("only %d vectors passed; at least %d are named in verifiedVectorIDs", passed, len(verifiedVectorIDs))
	}
}

func printSummary(t *testing.T, results []vecResult) {
	byGroup := map[string]map[string]int{}
	var order []string
	seen := map[string]bool{}
	for _, r := range results {
		if !seen[r.group] {
			seen[r.group] = true
			order = append(order, r.group)
		}
		if byGroup[r.group] == nil {
			byGroup[r.group] = map[string]int{}
		}
		byGroup[r.group][r.status]++
	}
	t.Logf("=== WRAP conformance vector summary (%d vectors) ===", len(results))
	totals := map[string]int{}
	for _, g := range order {
		counts := byGroup[g]
		t.Logf("  %-12s pass=%d not_covered=%d gap=%d", g, counts["PASS"], counts["NOT_COVERED"], counts["GAP"])
		for k, v := range counts {
			totals[k] += v
		}
	}
	t.Logf("  %-12s pass=%d not_covered=%d gap=%d", "TOTAL", totals["PASS"], totals["NOT_COVERED"], totals["GAP"])
}

// runVector dispatches on group and returns ("PASS"|"NOT_COVERED"|"GAP", note).
func runVector(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	t.Helper()
	group, _ := v["group"].(string)
	if reason, ok := notCoveredGroups[group]; ok {
		return "NOT_COVERED", reason
	}
	switch group {
	case "encode":
		return runEncode(t, vf, v)
	case "id":
		return runID(t, vf, v)
	case "sign":
		return runSign(t, vf, v)
	case "reject":
		return runReject(t, vf, v)
	case "authorship":
		return runAuthorship(t, vf, v)
	case "tiebreak":
		return runTiebreak(t, vf, v)
	case "forward":
		return runForward(t, vf, v)
	default:
		t.Fatalf("unknown vector group %q", group)
		return "", ""
	}
}

func strField(v map[string]any, k string) string {
	s, _ := v[k].(string)
	return s
}

func expectField(v map[string]any, k string) (string, bool) {
	exp, ok := v["expect"].(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := exp[k].(string)
	return s, ok
}

func expectBool(v map[string]any, k string) (bool, bool) {
	exp, ok := v["expect"].(map[string]any)
	if !ok {
		return false, false
	}
	b, ok := exp[k].(bool)
	return b, ok
}

// ── encode ────────────────────────────────────────────────────────────────

func runEncode(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	wantHex, _ := expectField(v, "canonical_bytes_hex")
	if objName := strField(v, "object"); objName != "" {
		got, err := buildCanonicalBytes(vf, objName)
		if err != nil {
			t.Fatalf("building canonical bytes for object %q: %v", objName, err)
		}
		if hex.EncodeToString(got) != wantHex {
			t.Fatalf("object %q: canonical bytes mismatch\n got  = %s\n want = %s", objName, hex.EncodeToString(got), wantHex)
		}
		return "PASS", ""
	}
	if rawVal, ok := v["value"]; ok {
		raw, err := json.Marshal(rawVal)
		if err != nil {
			t.Fatal(err)
		}
		val, err := typedToValue(raw)
		if err != nil {
			t.Fatalf("decoding vector value: %v", err)
		}
		got, err := Encode(val)
		if err != nil {
			t.Fatalf("Encode(%#v): %v", val, err)
		}
		if hex.EncodeToString(got) != wantHex {
			t.Fatalf("value %#v: got %s, want %s", val, hex.EncodeToString(got), wantHex)
		}
		return "PASS", ""
	}
	t.Fatalf("encode vector has neither `object` nor `value`: %#v", v)
	return "", ""
}

// ── id ────────────────────────────────────────────────────────────────────

func runID(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	switch strField(v, "id") {
	case "id-excludes-key-3":
		objName := strField(v, "object")
		canon, err := hex.DecodeString(vf.Objects[objName].CanonicalBytesHex)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeCBOR(canon)
		if err != nil {
			t.Fatalf("decoding canonical_bytes: %v", err)
		}
		m, ok := decoded.(map[any]any)
		if !ok {
			t.Fatalf("canonical_bytes did not decode to a map: %T", decoded)
		}
		_, present := m[uint64(3)]
		want, _ := expectBool(v, "key_3_present_in_canonical_bytes")
		if present != want {
			t.Fatalf("key 3 present = %v, want %v", present, want)
		}
		return "PASS", ""
	default:
		objName := strField(v, "object")
		canon, err := hex.DecodeString(vf.Objects[objName].CanonicalBytesHex)
		if err != nil {
			t.Fatal(err)
		}
		gotID := ComputeID(canon)
		wantIDHex, _ := expectField(v, "id_hex")
		if hex.EncodeToString(gotID) != wantIDHex {
			t.Fatalf("ComputeID(%s.canonical_bytes) = %s, want %s", objName, hex.EncodeToString(gotID), wantIDHex)
		}
		if wantPrefix, ok := expectField(v, "multihash_prefix_hex"); ok {
			gotPrefix := hex.EncodeToString(gotID[:1])
			if gotPrefix != wantPrefix {
				t.Fatalf("id multihash prefix = %s, want %s", gotPrefix, wantPrefix)
			}
		}
		return "PASS", ""
	}
}

// ── sign ──────────────────────────────────────────────────────────────────

func runSign(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	switch strField(v, "id") {
	case "sign-preimage-construction":
		objName := strField(v, "object")
		o := vf.Objects[objName]
		canon, err := hex.DecodeString(o.CanonicalBytesHex)
		if err != nil {
			t.Fatal(err)
		}
		got := preimage(canon)
		wantPrefixHex, _ := expectField(v, "preimage_prefix_hex")
		wantPrefix, err := hex.DecodeString(wantPrefixHex)
		if err != nil {
			t.Fatal(err)
		}
		if !hexEqPrefix(got, wantPrefix) {
			t.Fatalf("preimage prefix = %s, want %s", hex.EncodeToString(got[:len(wantPrefix)]), wantPrefixHex)
		}
		wantTermHex, _ := expectField(v, "preimage_terminator_hex")
		gotTerm := hex.EncodeToString(got[len(wantPrefix) : len(wantPrefix)+1])
		if gotTerm != wantTermHex {
			t.Fatalf("preimage terminator = %s, want %s", gotTerm, wantTermHex)
		}
		pub, err := keyPub(vf, o.Author)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := hex.DecodeString(o.SignatureHex)
		if err != nil {
			t.Fatal(err)
		}
		if !ed25519.Verify(pub, got, sig) {
			t.Fatalf("signature does not verify over the reconstructed preimage")
		}
		return "PASS", ""

	case "sign-verify-pass":
		objName := strField(v, "object")
		env, err := hex.DecodeString(vf.Objects[objName].EnvelopeHex)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(env); err != nil {
			t.Fatalf("Decode of a validly signed object failed: %v", err)
		}
		return "PASS", ""

	case "sign-verify-fail-tampered":
		objName := strField(v, "object")
		env, err := hex.DecodeString(vf.Objects[objName].EnvelopeHex)
		if err != nil {
			t.Fatal(err)
		}
		if strField(v, "tamper") == "flip_last_byte_of_envelope" {
			env = append([]byte(nil), env...)
			env[len(env)-1] ^= 0xff
		}
		_, err = Decode(env)
		wantCode, _ := expectField(v, "error_code")
		if got := errorCode(err); got != wantCode {
			t.Fatalf("Decode(tampered) error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "sign-verify-fail-wrong-claimed-author":
		envHex, _ := expectField(v, "envelope_hex")
		env, err := hex.DecodeString(envHex)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(env)
		wantCode, _ := expectField(v, "error_code")
		if got := errorCode(err); got != wantCode {
			t.Fatalf("Decode error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	default:
		t.Fatalf("unhandled sign vector %q", strField(v, "id"))
		return "", ""
	}
}

func hexEqPrefix(got, wantPrefix []byte) bool {
	if len(got) < len(wantPrefix) {
		return false
	}
	for i := range wantPrefix {
		if got[i] != wantPrefix[i] {
			return false
		}
	}
	return true
}

// ── reject ────────────────────────────────────────────────────────────────

func runReject(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	id := strField(v, "id")
	wantCode, _ := expectField(v, "error_code")

	switch id {
	case "reject-forbidden-key":
		env, err := hex.DecodeString(strField(v, "envelope_hex"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(env)
		if got := errorCode(err); got != wantCode {
			t.Fatalf("error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "reject-non-canonical-minimal":
		raw, err := hex.DecodeString(strField(v, "raw_cbor_hex"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = DecodeCBOR(raw)
		if got := errorCode(err); got != wantCode {
			t.Fatalf("error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "reject-non-canonical-realistic":
		env, err := hex.DecodeString(strField(v, "envelope_hex"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(env)
		if got := errorCode(err); got != wantCode {
			t.Fatalf("error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "reject-unsupported-version":
		env, err := hex.DecodeString(strField(v, "envelope_hex"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(env)
		if got := errorCode(err); got != wantCode {
			t.Fatalf("error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "reject-oversize":
		issuerPriv, err := keyPriv(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		issuerPub, err := keyPub(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		var author [32]byte
		copy(author[:], issuerPub)
		detail := strings.Repeat("x", 70000)
		wo := WorkOrder{Profile: "delivery/v0", Title: "big", Detail: detail, Expires: 1900000000}
		obj := wo.ToObject(author, vf.Objects["wo1"].TS)
		if err := obj.Sign(issuerPriv); err != nil {
			t.Fatal(err)
		}
		env, err := obj.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		_, decErr := Decode(env)
		if decErr == nil {
			// Decode now enforces the §4.6 limit itself (object.go's
			// maxEnvelopeSize / ErrTooLarge), so for an envelope this size
			// this arm should be unreachable today; it stays as a tripwire
			// in case that check ever regresses. The currently-running
			// proof of compliance is TestDecodeSizeFloor in wrap_test.go,
			// not this vector — the upstream corpus that would carry a
			// "reject-oversize" id has no known vector by that name (see
			// this file's top-of-package comment on where vectors come
			// from), so this case has never actually executed. It stays
			// listed in knownGapVectorIDs as a presence tripwire: if such
			// a vector ever does appear upstream, the harness will run it
			// rather than silently drop it, and the case above confirms it
			// now gets the right ERR_TOO_LARGE verdict rather than
			// re-asserting a gap that no longer exists.
			return "GAP", fmt.Sprintf(
				"04-signing.md §5.4 / 03-wire-format.md §4.6 require ERR_TOO_LARGE for any envelope over 65536 bytes; this envelope is %d bytes and Decode() accepted it with no error at all.",
				len(env))
		}
		// If it ever DOES start rejecting oversize objects, confirm it's the right code.
		if got := errorCode(decErr); got != wantCode {
			return "GAP", fmt.Sprintf("oversize envelope was rejected, but with %s (%v) instead of the expected %s", got, decErr, wantCode)
		}
		return "PASS", ""

	case "reject-bad-id":
		return "NOT_COVERED", "the bare [canonical_bytes, signature] envelope this package implements carries no separate `claimed id` field to compare a recomputation against (canonical_bytes excludes key 3 by construction, per §4.3/§5.2) — ERR_BAD_ID as §5.4 step 4 describes it has no reachable code path in this binding; see conformance/README.md's note on this vector for the content-addressed-fetch reading where it would apply"

	default:
		t.Fatalf("unhandled reject vector %q", id)
		return "", ""
	}
}

// ── authorship ────────────────────────────────────────────────────────────

func runAuthorship(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	id := strField(v, "id")

	decodeObj := func(name string) *Object {
		env, err := hex.DecodeString(vf.Objects[name].EnvelopeHex)
		if err != nil {
			t.Fatal(err)
		}
		o, err := Decode(env)
		if err != nil {
			t.Fatalf("decoding object %q: %v", name, err)
		}
		return o
	}

	switch id {
	case "authorship-assignment-by-issuer-accepted":
		wo := decodeObj("wo1")
		a := decodeObj("assignment_legit")
		if err := VerifyAssignmentAuthor(a, wo); err != nil {
			t.Fatalf("legitimate assignment rejected: %v", err)
		}
		return "PASS", ""

	case "authorship-assignment-by-performer-rejected":
		wo := decodeObj("wo1")
		a := decodeObj("assignment_forged_by_performer")
		err := VerifyAssignmentAuthor(a, wo)
		wantCode, _ := expectField(v, "error_code")
		if got := errorCode(err); got != wantCode {
			t.Fatalf("VerifyAssignmentAuthor error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "authorship-assignment-by-third-party-rejected":
		wo := decodeObj("wo1")
		a := decodeObj("assignment_forged_by_third_party")
		err := VerifyAssignmentAuthor(a, wo)
		wantCode, _ := expectField(v, "error_code")
		if got := errorCode(err); got != wantCode {
			t.Fatalf("VerifyAssignmentAuthor error code = %s (%v), want %s", got, err, wantCode)
		}
		return "PASS", ""

	case "authorship-bid-by-any-principal-accepted":
		decodeObj("bid1")              // must not error
		decodeObj("bid_any_principal") // must not error either — no Bid authorship check exists
		return "PASS", ""

	default:
		t.Fatalf("unhandled authorship vector %q", id)
		return "", ""
	}
}

// ── tiebreak ──────────────────────────────────────────────────────────────

func runTiebreak(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	id := strField(v, "id")
	switch id {
	case "tiebreak-is-pure-string-comparison":
		objName := strField(v, "object")
		env, err := hex.DecodeString(vf.Objects[objName].EnvelopeHex)
		if err != nil {
			t.Fatal(err)
		}
		o, err := Decode(env)
		if err != nil {
			t.Fatal(err)
		}
		wantTS, _ := expectField(v, "ts")
		if o.TS != wantTS {
			t.Fatalf("decoded TS = %q, want %q", o.TS, wantTS)
		}
		return "PASS", ""
	default:
		return "NOT_COVERED", "internal/wrap stores `ts` as an opaque string (Object.TS) but implements no comparator, ordering, or tie-break logic over it — that lives in Pango's own store/oplog layer, a different HLC implementation not wired to this package"
	}
}

// ── forward ───────────────────────────────────────────────────────────────

func runForward(t *testing.T, vf *vectorsFile, v map[string]any) (string, string) {
	id := strField(v, "id")
	switch id {
	case "forward-unknown-kind-ignored":
		issuerPriv, err := keyPriv(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		issuerPub, err := keyPub(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		var author [32]byte
		copy(author[:], issuerPub)
		o := &Object{Kind: Kind(0x55), Author: author, TS: vf.Objects["wo1"].TS, Fields: M{6: "some future object kind's payload"}}
		if err := o.Sign(issuerPriv); err != nil {
			t.Fatal(err)
		}
		env, err := o.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(env)
		if err != nil {
			t.Fatalf("decoding an unknown-kind object must succeed, got: %v", err)
		}
		if got.Kind != Kind(0x55) {
			t.Fatalf("kind = %v, want 0x55", got.Kind)
		}
		return "PASS", ""

	case "forward-unknown-field-preserved-through-reencode":
		objName := strField(v, "object")
		env, err := hex.DecodeString(vf.Objects[objName].EnvelopeHex)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(env)
		if err != nil {
			t.Fatal(err)
		}
		reEnv, err := decoded.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		if !bytesEqualHex(reEnv, env) {
			t.Fatalf("re-encoding the decoded object did not reproduce the original bytes:\n got  = %x\n want = %x", reEnv, env)
		}
		return "PASS", ""

	case "forward-unknown-profile-stored-not-rendered":
		issuerPriv, err := keyPriv(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		issuerPub, err := keyPub(vf, "issuer")
		if err != nil {
			t.Fatal(err)
		}
		var author [32]byte
		copy(author[:], issuerPub)
		wo := WorkOrder{Profile: "example.org:towing/v0", Title: "unrecognised profile", Expires: 1900000000}
		obj := wo.ToObject(author, vf.Objects["wo1"].TS)
		if err := obj.Sign(issuerPriv); err != nil {
			t.Fatal(err)
		}
		env, err := obj.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(env)
		if err != nil {
			t.Fatalf("decoding a WorkOrder with an unrecognised profile must succeed: %v", err)
		}
		parsed, err := WorkOrderFrom(decoded)
		if err != nil {
			t.Fatalf("WorkOrderFrom must succeed for an unrecognised profile (store, don't validate): %v", err)
		}
		if parsed.Profile != "example.org:towing/v0" {
			t.Fatalf("profile = %q, want the unrecognised profile preserved verbatim", parsed.Profile)
		}
		return "PASS", ""

	default:
		t.Fatalf("unhandled forward vector %q", id)
		return "", ""
	}
}

func bytesEqualHex(a, b []byte) bool {
	return hex.EncodeToString(a) == hex.EncodeToString(b)
}
