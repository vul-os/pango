package substrate

// vectors_test.go — SYNC.md §10's frozen conformance vectors, driven through the
// engine Pango actually links.
//
// # Why this file exists
//
// go.sum proves the engine module is the module it says it is. It proves nothing
// about whether that module's ENGINE computes the algebra Pango's merge
// semantics now rest on. This file closes that gap: every one of the 24 vectors
// frozen in the kotva repo at conformance/vectors/sync_vectors.json is driven
// through this binding and compared against the values the specification froze —
// canonical encodings, content addresses, COSE envelopes, merge verdicts,
// tie-breaks, death domination, PN-counter union semantics, RGA sibling order,
// cycle-safe tree replay, observable-state roots, snapshot fold-then-recompute,
// range-Merkle folds, namespace scoping, the fast-join floor predicate and the
// §12 refusal codes.
//
// It is also what makes mapping.go's package doc legitimate. "This is DMTAP-SYNC"
// is a claim about behaviour, and the only evidence for it that is not a reading
// of source is this test passing.
//
// # Why Pango carries its own driver
//
// The obvious alternative — running the binding module's own vectors_test.go in
// CI — cannot work from a proxy fetch: that suite also needs a native Rust trace
// under crates/, which is not in the module zip. So Pango drives the vectors
// itself, against the vectors FILE rather than against a copy of it, so a vector
// that changes upstream changes here on the next checkout instead of drifting
// silently.
//
// # It never passes by doing nothing
//
// The vectors file is not in this repo, so this test needs a kotva checkout —
// KOTVA_DIR, which CI sets from a checkout at the tag matching the engine version
// in go.mod. Without it the test SKIPS, and the skip names how many vectors went
// unverified and how to fix it. With PANGO_REQUIRE_SYNC_VECTORS=1 the skip
// becomes a failure, which is how CI runs it: a guard that has quietly stopped
// running looks exactly like one that passes.
//
// The three anti-vacuity checks are the other half, because `go test` printing
// "ok" proves nothing on its own. Every vector must be claimed by a driver in the
// dispatch table (an unclaimed operation is an error, never a skip — it would
// otherwise pass by not being run); the file must hold exactly wantVectors
// vectors; and the number of subtests whose body actually executed must equal
// that count before the run is allowed to call itself a conformance run.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	kotvasync "github.com/vul-os/kotva/bindings/go"
)

// wantVectors is how many vectors SYNC.md §10 freezes. The file's own scope_note
// says "ALL 24"; if a checkout disagrees, the frozen suite changed size and this
// file has to be READ again rather than have this number adjusted to match.
const wantVectors = 24

// receiverNowMS is the frozen receiver clock every ingest in this file runs at.
//
// The vectors' ops are stamped at a 2023-11-14 wall (1700000100000) and the two
// other drivers of this same fixture — the engine's own harness and the sibling
// product's — both read the receiver at 1700000900000, 800 s later. Matching them
// is what makes a divergence here a finding about the engine rather than about
// which clock somebody picked. §3's skew bound is one-sided (an op from the past
// is not skewed), so a receiver 800 s AHEAD of every op is always safe;
// TestFrozenSyncVectors checks that property against the fixture on every run.
const receiverNowMS = 1_700_000_900_000

// vectorsPath resolves the frozen file from a kotva checkout.
func vectorsPath() (string, bool) {
	dir := os.Getenv("KOTVA_DIR")
	if dir == "" {
		return "", false
	}
	p := filepath.Join(dir, "conformance", "vectors", "sync_vectors.json")
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

// vector is one frozen case. Input and Expected stay raw because their members
// are of mixed and per-operation shape, and decoding them into a union type would
// mean this file deciding what the fixture says.
type vector struct {
	Name      string                     `json:"name"`
	Operation string                     `json:"operation"`
	Input     map[string]json.RawMessage `json:"input"`
	Expected  map[string]json.RawMessage `json:"expected"`
}

func TestFrozenSyncVectors(t *testing.T) {
	path, ok := vectorsPath()
	if !ok {
		// The two branches differ in more than severity, so they say different
		// things. Telling a reader who has already set
		// PANGO_REQUIRE_SYNC_VECTORS=1 to go and set it is how a fail-closed
		// message trains people to ignore it.
		msg := fmt.Sprintf(
			"KOTVA_DIR is unset or holds no conformance/vectors/sync_vectors.json: "+
				"all %d frozen SYNC conformance vectors went UNVERIFIED in this run, so nothing "+
				"here says the engine computes the substrate's algebra. The fixtures are REPO "+
				"files, not module files, so `go get` does not bring them. Point KOTVA_DIR at a "+
				"checkout of github.com/vul-os/kotva AT THE TAG matching the binding version in "+
				"go.mod (tag form: bindings/go/<version>) — a checkout at any other commit would "+
				"be verifying against a different frozen suite than the one linked in. "+
				"`make sync-conformance KOTVA_DIR=...` does this and refuses to pass without it.",
			wantVectors)
		if os.Getenv("PANGO_REQUIRE_SYNC_VECTORS") == "1" {
			t.Fatal(msg + " PANGO_REQUIRE_SYNC_VECTORS=1 is set, so this absence is a failure.")
		}
		t.Skip(msg + " Set PANGO_REQUIRE_SYNC_VECTORS=1 to make this absence a failure" +
			" instead of a skip; CI does.")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var file struct {
		Format  string   `json:"format"`
		Suite   string   `json:"suite"`
		Vectors []vector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(file.Vectors) != wantVectors {
		t.Fatalf("%s holds %d vectors, want %d — the frozen suite changed size, so read the "+
			"file and drive whatever was added before changing this number",
			path, len(file.Vectors), wantVectors)
	}

	inst := newInstance(t)
	assertFixtureDescribesThisEngine(t, inst, file.Format, file.Suite)
	assertReceiverClockIsAheadOfEveryOp(t, raw)

	driven := 0
	for _, v := range file.Vectors {
		drive, known := drivers[v.Operation]
		if !known {
			t.Errorf("vector %q carries operation %q, which no driver in this file claims — "+
				"it would otherwise pass by not being run, which is the one failure mode a "+
				"conformance harness must not have", v.Name, v.Operation)
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			// Counted from INSIDE the subtest body, so the total below measures
			// bodies that actually executed rather than loop iterations.
			driven++
			drive(t, inst, v)
		})
	}

	if driven != wantVectors {
		t.Fatalf("only %d of %d frozen vectors were actually driven — a run that skips a vector "+
			"is not a conformance run, whatever it prints", driven, wantVectors)
	}
	t.Logf("drove all %d frozen SYNC conformance vectors from %s", driven, path)
}

// assertFixtureDescribesThisEngine holds the fixture's own self-description
// against the engine, so a file for some other substrate or suite cannot be
// driven green: the vectors are only evidence about SYNC.md suite 0x01.
func assertFixtureDescribesThisEngine(t *testing.T, in *kotvasync.Instance, format, suite string) {
	t.Helper()
	const wantFormat = "dmtap-conformance-vectors/1"
	if format != wantFormat {
		t.Fatalf("the fixture declares format %q, this driver reads %q — a different container "+
			"format means the fields below are not the fields this file thinks they are",
			format, wantFormat)
	}
	if suite == "" {
		t.Fatal("the fixture declares no suite; without it there is nothing tying these vectors " +
			"to the substrate document and cryptographic suite the engine implements")
	}
	v, err := in.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if !strings.Contains(suite, "SYNC.md") {
		t.Fatalf("the fixture's suite line does not name SYNC.md (%q) — the engine speaks %q, so "+
			"these vectors may be freezing some other capability", suite, v.Substrate)
	}
	if want := fmt.Sprintf("0x%02X", v.Suite); !strings.Contains(suite, want) {
		t.Fatalf("the engine runs cryptographic suite %s but the fixture's suite line (%q) does "+
			"not name it — Ed25519/BLAKE3 known answers are suite-specific", want, suite)
	}
}

// assertReceiverClockIsAheadOfEveryOp checks the one property receiverNowMS has
// to have: no op in the fixture is stamped ahead of it. §3's skew bound is
// one-sided, so an op from the past is fine and an op from the future is refused
// with 0x0A05 — a receiver reading behind the vectors would fail every ingest
// driver here for a reason that has nothing to do with the algebra.
func assertReceiverClockIsAheadOfEveryOp(t *testing.T, raw []byte) {
	t.Helper()
	var doc any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("re-reading the fixture for its clocks: %v", err)
	}
	max, found := uint64(0), false
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			for k, val := range n {
				if k == "wall" {
					if num, ok := val.(json.Number); ok {
						if w, err := strconv.ParseUint(num.String(), 10, 64); err == nil {
							found = true
							if w > max {
								max = w
							}
						}
					}
				}
				walk(val)
			}
		case []any:
			for _, val := range n {
				walk(val)
			}
		}
	}
	walk(doc)
	if !found {
		t.Fatal("the fixture declares no HLC walls at all, so nothing checks that receiverNowMS " +
			"is a clock these vectors can be ingested at")
	}
	if max > receiverNowMS {
		t.Fatalf("the fixture's latest op wall is %d but this driver reads the receiver clock at "+
			"%d — every ingest here would be refused as future skew (0x0A05). Move receiverNowMS "+
			"forward to match the other drivers of this fixture rather than loosening the bound",
			max, uint64(receiverNowMS))
	}
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

type driver func(t *testing.T, in *kotvasync.Instance, v vector)

// drivers claims one entry per vector `operation`. An operation with no entry is
// an error in TestFrozenSyncVectors, never a skip.
var drivers = map[string]driver{
	"sync_op_encode":                driveOpEncode,
	"sync_op_cose_sign1_verify":     driveCoseSign1,
	"sync_author_admission":         driveAuthorAdmission,
	"sync_lww_merge":                driveLWWMerge,
	"sync_orset_merge":              driveORSetMerge,
	"sync_orset_remove_validity":    driveORSetRemoveValidity,
	"sync_death_domination":         driveDeathDomination,
	"sync_death_tie":                driveDeathTie,
	"sync_pn_merge":                 drivePNMerge,
	"sync_counter_foreign_check":    driveCounterForeign,
	"sync_rga_sibling_order":        driveRGASiblingOrder,
	"sync_rga_tombstone_origin":     driveRGATombstoneOrigin,
	"sync_tree_move_replay":         driveTreeMoveReplay,
	"sync_snapshot_state_root":      driveSnapshotStateRoot,
	"sync_snapshot_fast_join":       driveSnapshotFastJoin,
	"sync_snapshot_body_fold":       driveSnapshotBodyFold,
	"sync_ext_value_validate":       driveExtValue,
	"sync_fastjoin_pull_response":   driveFastJoinPullResponse,
	"sync_fastjoin_floor_predicate": driveFastJoinFloorPredicate,
	"sync_recon_fingerprint":        driveReconFingerprint,
	"sync_ns_sparse_filter":         driveNsSparseFilter,
	"sync_ns_leak_check":            driveNsLeakCheck,
	"sync_gc_stability_cut":         driveStabilityCut,
}

// ---------------------------------------------------------------------------
// Drivers — §4 the op and the algebra
// ---------------------------------------------------------------------------

// SYNC-OP-01: the canonical op encoding, and that a second spelling of the same
// op is refused rather than re-canonicalised.
func driveOpEncode(t *testing.T, in *kotvasync.Instance, v vector) {
	kinds := opKinds(t, in)
	kind := uint8(num(t, v.Input, "kind"))
	// The §4.2 kind numbers are the engine's to name, never this file's to
	// remember: reading lww_set out of the engine means a renumbering shows up
	// here as a mismatch instead of as a vector that quietly encodes some other
	// op kind.
	if kind != kinds.LWWSet {
		t.Fatalf("this vector encodes a last-write-wins field write, but its kind %d is not the "+
			"engine's lww_set (%d) — one of the two has been renumbered", kind, kinds.LWWSet)
	}

	field := str(t, v.Input, "field")
	op := kotvasync.Op{
		Kind:   kind,
		NS:     str(t, v.Input, "ns"),
		Target: str(t, v.Input, "target"),
		Field:  &field,
		Value:  kotvasync.Text(str(t, v.Input, "value_tstr")),
		HLC:    hlcOf(t, v.Input, "hlc"),
	}
	got, err := in.EncodeOp(op)
	if err != nil {
		t.Fatalf("EncodeOp: %v", err)
	}
	wantHex(t, "cbor_hex", got, v.Expected)

	// §2.2: decoding and re-encoding must reproduce the same bytes, or content
	// addressing rests on an encoder that spells one value two ways.
	back, err := in.DecodeOp(got)
	if err != nil {
		t.Fatalf("DecodeOp: %v", err)
	}
	again, err := in.EncodeOp(back)
	if err != nil {
		t.Fatalf("re-EncodeOp: %v", err)
	}
	if !equalHex(again, got) {
		t.Fatalf("the op did not survive a decode/encode round trip:\n  got  %x\n  want %x",
			again, got)
	}

	// The same op respelled with `kind` in a two-byte head is NOT canonical CBOR.
	// It must be refused, not accepted-and-normalised: an engine that silently
	// re-canonicalises gives two op-ids to one op, and the op-id is the dedup key
	// the whole replay path depends on.
	if len(got) < 4 || got[1] != 0x01 || got[2] != kind || kind >= 24 {
		t.Fatalf("the frozen op no longer begins with a single-byte `kind` (%x) — the "+
			"non-canonical spelling below can no longer be built, so re-read the encoding", got)
	}
	noncanonical := append([]byte{}, got[:2]...)
	noncanonical = append(noncanonical, 0x18, kind)
	noncanonical = append(noncanonical, got[3:]...)
	invalid := registryCode(t, in, "ERR_SYNC_OP_INVALID")
	if _, err := in.DecodeOp(noncanonical); !kotvasync.IsRefusal(err, invalid) {
		t.Fatalf("a non-shortest-form spelling of the frozen op decoded as %v, want a refusal "+
			"with %s — two encodings of one op means two op-ids for one fact", err, invalid)
	}
}

// SYNC-OP-02: the COSE_Sign1 envelope, signed OUTSIDE the engine with a key it
// never sees, and the two tampering cases that must fail closed.
func driveCoseSign1(t *testing.T, in *kotvasync.Instance, v vector) {
	opBytes := hexIn(t, v.Input, "sync_op_cbor_hex")
	if !boolOf(v.Expected, "verifies") {
		t.Fatal("this vector no longer expects its own envelope to verify; re-read §4.1 before " +
			"touching the assertions below")
	}

	si, err := in.OpSigningInput(opBytes)
	if err != nil {
		t.Fatalf("OpSigningInput: %v", err)
	}
	if want := str(t, v.Input, "signer_pubkey_hex"); si.Author != want {
		t.Fatalf("the signing input names author %s, the op's HLC names %s — the key that signs "+
			"and the key the op claims must be the same by construction", si.Author, want)
	}
	if want := str(t, v.Input, "external_aad_hex"); si.ExternalAAD != want {
		t.Fatalf("external_aad = %s, want %s — this tag is what stops a signature minted for "+
			"another object being replayed as a SyncOp", si.ExternalAAD, want)
	}
	if want := strOf(t, v.Expected, "sig_structure_hex"); si.SigStructure != want {
		t.Fatalf("Sig_structure (the exact preimage a custodian signs) = %s, want %s",
			si.SigStructure, want)
	}

	// The frozen protected header and payload are the WIRE forms — CBOR byte
	// strings, as they appear inside the envelope — so they are compared through
	// the engine's own encoder rather than through a hand-written "5826" prefix.
	protectedWire, err := in.EncodeValue(kotvasync.Bytes(mustHex(t, si.Protected)))
	if err != nil {
		t.Fatalf("encoding the protected header: %v", err)
	}
	wantHex(t, "protected_hex", protectedWire, v.Expected)

	// Ed25519 is deterministic (RFC 8032), so the frozen signature is a
	// reproducible known answer and not a sample: signing the preimage with the
	// frozen seed must yield exactly these bytes.
	preimage, err := si.Bytes()
	if err != nil {
		t.Fatalf("decoding the preimage: %v", err)
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(hexIn(t, v.Input, "signer_seed_hex")), preimage)
	if hex.EncodeToString(sig) != strOf(t, v.Expected, "signature_hex") {
		t.Fatalf("signing the frozen preimage with the frozen seed produced %x, not the frozen "+
			"signature %s", sig, strOf(t, v.Expected, "signature_hex"))
	}

	cose, err := in.OpAttachSignature(opBytes, sig)
	if err != nil {
		t.Fatalf("OpAttachSignature: %v", err)
	}
	if want := str(t, v.Input, "cose_sign1_hex"); hex.EncodeToString(cose) != want {
		t.Fatalf("the assembled envelope is %x, want %s", cose, want)
	}

	payload, err := in.VerifySignedOp(cose)
	if err != nil {
		t.Fatalf("VerifySignedOp: %v", err)
	}
	if !equalHex(payload, opBytes) {
		t.Fatalf("verifying the envelope yielded %x, not the op it carries", payload)
	}
	payloadWire, err := in.EncodeValue(kotvasync.Bytes(payload))
	if err != nil {
		t.Fatalf("encoding the payload: %v", err)
	}
	wantHex(t, "payload_hex", payloadWire, v.Expected)

	parts, err := in.DecodeSignedOp(cose)
	if err != nil {
		t.Fatalf("DecodeSignedOp: %v", err)
	}
	if want := strOf(t, v.Expected, "unprotected_hex"); parts.Unprotected != want {
		t.Fatalf("unprotected header = %s, want %s (a non-empty one is refused)",
			parts.Unprotected, want)
	}
	if want := int(num(t, v.Input, "alg")); parts.Alg != want {
		t.Fatalf("the envelope claims alg %d, want %d", parts.Alg, want)
	}
	if want := str(t, v.Input, "signer_pubkey_hex"); parts.Kid != want {
		t.Fatalf("the envelope's kid is %s, want the op's author %s", parts.Kid, want)
	}

	id, err := in.OpID(opBytes)
	if err != nil {
		t.Fatalf("OpID: %v", err)
	}
	wantHex(t, "op_id_hex", id, v.Expected)

	// Both tampering cases carry their own frozen registry entry, and both must
	// fail closed: a payload swapped under a good signature, and a kid swapped to
	// another admitted author.
	for field, key := range map[string]string{
		"tampered_payload_cose_sign1_hex": "tampered_payload",
		"substituted_kid_cose_sign1_hex":  "substituted_kid",
	} {
		expected := mustObject(t, v.Expected, key)
		if boolOf(expected, "verifies") {
			t.Fatalf("%s: the vector expects a verification FAILURE", key)
		}
		_, err := in.VerifySignedOp(hexIn(t, v.Input, field))
		if err == nil {
			t.Fatalf("%s verified — a tampered envelope was accepted as authentic", key)
		}
		wantRefusal(t, err, expected, key)
	}

	// A third negative the vector's prose demands but does not encode: the
	// signature is bound to the op DS-tag, so a signature over the SAME preimage
	// under any OTHER external_aad must not assemble. Without this, domain
	// separation is asserted only by reading the spec.
	aad := append([]byte{}, mustHex(t, si.ExternalAAD)...)
	aad[0] ^= 0xff
	foreign := sigStructure(t, in, si.Protected, aad, opBytes)
	foreignSig := ed25519.Sign(
		ed25519.NewKeyFromSeed(hexIn(t, v.Input, "signer_seed_hex")), foreign)
	invalidSig := registryCode(t, in, "ERR_SYNC_OP_SIG_INVALID")
	if _, err := in.OpAttachSignature(opBytes, foreignSig); !kotvasync.IsRefusal(err, invalidSig) {
		t.Fatalf("a signature minted under a different external_aad assembled into a wire "+
			"envelope (%v) — domain separation is broken, want %s", err, invalidSig)
	}
}

// SYNC-AUTH-01: admission is a gate, not a blanket deny.
func driveAuthorAdmission(t *testing.T, in *kotvasync.Instance, v vector) {
	if got := strOf(t, v.Expected, "outcome"); got != "reject" {
		t.Fatalf("this vector's outcome is now %q; the driver below asserts a rejection", got)
	}
	author := hexIn(t, v.Input, "op_hlc_author_hex")
	// The refusal has to be about the op's OWN author, so the frozen author is
	// held against the op bytes rather than trusted as a label.
	if got := decodeOp(t, in, hexIn(t, v.Input, "op_cbor_hex")).HLC.Author; got != hex.EncodeToString(author) {
		t.Fatalf("the frozen op is authored by %s but the vector's op_hlc_author_hex is %x",
			got, author)
	}

	var admitted []string
	if err := json.Unmarshal(v.Input["admitted_authors_hex"], &admitted); err != nil {
		t.Fatalf("input/admitted_authors_hex: %v", err)
	}
	if len(admitted) == 0 {
		t.Fatal("the vector admits nobody, so refusing the op proves nothing")
	}
	wantRefusal(t, in.CheckAdmitted(author, admitted), v.Expected, "CheckAdmitted")

	// ...and every admitted author IS admitted, so the check is not simply
	// refusing everything it is handed.
	for i, a := range admitted {
		if err := in.CheckAdmitted(mustHex(t, a), admitted); err != nil {
			t.Fatalf("CheckAdmitted refused admitted author %d (%s): %v", i, a[:16], err)
		}
	}
}

// SYNC-LWW-01 / -02: one winner, whatever the apply order — by HLC, and at an
// exact HLC tie by the larger det_cbor(value).
func driveLWWMerge(t *testing.T, in *kotvasync.Instance, v vector) {
	ops := hexList(t, v.Input, "ops_cbor_hex")
	if len(ops) != 2 {
		t.Fatalf("this vector now carries %d ops; the premise checks below assume the frozen "+
			"pair of competing writes", len(ops))
	}
	first, second := decodeOp(t, in, ops[0]), decodeOp(t, in, ops[1])

	// Which of the two §4.4 cases this is, taken from the vector's own inputs
	// rather than from its name: `hlcs` means the winner is decided by the clock,
	// a single `hlc` means both writes share it and the value breaks the tie.
	cmp, err := in.CompareHLC(second.HLC, first.HLC)
	if err != nil {
		t.Fatalf("CompareHLC: %v", err)
	}
	if _, ordered := v.Input["hlcs"]; ordered {
		var hlcs []hlcJSON
		if err := json.Unmarshal(v.Input["hlcs"], &hlcs); err != nil {
			t.Fatalf("input/hlcs: %v", err)
		}
		if len(hlcs) != 2 {
			t.Fatalf("input/hlcs holds %d stamps, want 2", len(hlcs))
		}
		for i, want := range []kotvasync.HLC{hlcs[0].hlc(), hlcs[1].hlc()} {
			got := []kotvasync.Op{first, second}[i].HLC
			if same, err := in.CompareHLC(got, want); err != nil || same != 0 {
				t.Fatalf("op %d is stamped %+v, the vector froze %+v", i, got, want)
			}
		}
		if cmp <= 0 {
			t.Fatal("the vector's premise is broken: the second write must outrank the first")
		}
	} else {
		if same, err := in.CompareHLC(first.HLC, hlcOf(t, v.Input, "hlc")); err != nil || same != 0 {
			t.Fatalf("op 0 is not stamped with the vector's single frozen HLC: %v", err)
		}
		if cmp != 0 {
			t.Fatal("the vector's premise is broken: an exact-tie case requires both writes to " +
				"carry one identical HLC, so the tie-break is what decides the winner")
		}
	}

	eng := ingestAll(t, in, ops)
	defer eng.Close()

	cell, err := eng.LWWCell(first.Target, deref(first.Field))
	if err != nil || cell == nil {
		t.Fatalf("LWWCell(%s, %s) = %v, %v", first.Target, deref(first.Field), cell, err)
	}
	if got := tstr(t, cell.Value); got != strOf(t, v.Expected, "winner_value") {
		t.Fatalf("the winning value is %q, want %q", got, strOf(t, v.Expected, "winner_value"))
	}
	if _, ok := v.Expected["winner_hlc_hex"]; ok {
		encoded, err := in.EncodeHLC(cell.HLC)
		if err != nil {
			t.Fatalf("EncodeHLC: %v", err)
		}
		wantHex(t, "winner_hlc_hex", encoded, v.Expected)
	}
	if _, ok := v.Expected["winner_value_cbor_hex"]; ok {
		encoded, err := in.EncodeValue(cell.Value)
		if err != nil {
			t.Fatalf("EncodeValue: %v", err)
		}
		wantHex(t, "winner_value_cbor_hex", encoded, v.Expected)
	}

	// The merge is a join, so the reverse order must land on the same state. This
	// runs for both vectors, not only the one carrying apply_order_independent:
	// the tie-break is a property of the values, and a tie that resolved by
	// arrival order would be the same divergence by a subtler route.
	reverse := ingestAll(t, in, reversedOps(ops))
	defer reverse.Close()
	reverseCell, err := reverse.LWWCell(first.Target, deref(first.Field))
	if err != nil || reverseCell == nil {
		t.Fatalf("LWWCell after the reverse apply = %v, %v", reverseCell, err)
	}
	if tstr(t, reverseCell.Value) != tstr(t, cell.Value) {
		t.Fatalf("apply order changed the winner: %q forward, %q backward",
			tstr(t, cell.Value), tstr(t, reverseCell.Value))
	}
	assertSameState(t, eng, reverse, "the two apply orders")

	// `rule` is prose describing the tie-break and is deliberately not asserted;
	// what it describes is asserted above through winner_value_cbor_hex.
}

// SYNC-ORSET-01: add-wins, and the surviving add-tag is the causal evidence for
// why the element is still there.
func driveORSetMerge(t *testing.T, in *kotvasync.Instance, v vector) {
	ops := hexList(t, v.Input, "ops_cbor_hex")
	eng := ingestAll(t, in, ops)
	defer eng.Close()

	target := decodeOp(t, in, ops[0]).Target
	element := kotvasync.Text(str(t, v.Input, "element"))

	present, err := eng.SetContains(target, element)
	if err != nil {
		t.Fatalf("SetContains: %v", err)
	}
	if want := boolOf(v.Expected, "present"); present != want {
		t.Fatalf("the element's presence is %v, want %v — presence is \"at least one add-tag no "+
			"tombstone covers\", so a concurrent add the remove never saw keeps it", present, want)
	}

	// Reverse order too: add-wins cannot depend on the remove arriving second.
	reverse := ingestAll(t, in, reversedOps(ops))
	defer reverse.Close()
	if got, err := reverse.SetContains(target, element); err != nil || got != present {
		t.Fatalf("apply order changed the element's presence (%v backward vs %v forward): %v",
			got, present, err)
	}
	assertSameState(t, eng, reverse, "the two apply orders")

	tags, err := eng.SetSurvivingTags(target, element)
	if err != nil {
		t.Fatalf("SetSurvivingTags: %v", err)
	}
	var got []string
	for _, tag := range tags {
		got = append(got, hlcHex(t, in, tag.HLC))
	}
	want := strOf(t, v.Expected, "surviving_add_tag_hlc_hex")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("the surviving add-tags are %v, want exactly [%s] — an observed-remove must "+
			"tombstone the tag it cited and only that one", got, want)
	}

	// The vector also freezes the whole tag set and which of them the remove
	// tombstoned. Their difference must BE the surviving tag: that is what makes
	// the single hex above evidence rather than a coincidence.
	all := addTagsOf(t, v.Input, "add_tags")
	dead := addTagsOf(t, v.Input, "tombstoned_add_tags")
	buried := map[string]bool{}
	for _, tag := range dead {
		buried[hlcHex(t, in, tag.HLC)] = true
	}
	var survivors []string
	for _, tag := range all {
		if h := hlcHex(t, in, tag.HLC); !buried[h] {
			survivors = append(survivors, h)
		}
	}
	if !reflect.DeepEqual(survivors, []string{want}) {
		t.Fatalf("the vector's own add-tags minus its tombstoned tags leave %v, but it names %s "+
			"as the survivor — the fixture is inconsistent with itself", survivors, want)
	}
}

// SYNC-ORSET-02: a remove citing an add it cannot have seen is causally
// impossible, and is refused by the validator AND by the full ingest path.
func driveORSetRemoveValidity(t *testing.T, in *kotvasync.Instance, v vector) {
	if got := strOf(t, v.Expected, "outcome"); got != "reject" {
		t.Fatalf("this vector's outcome is now %q; the driver below asserts a rejection", got)
	}
	opBytes := hexIn(t, v.Input, "op_cbor_hex")

	// The premise, checked rather than assumed: the remove's own stamp is BELOW
	// the add-tag it claims to have observed, so it is citing the future.
	op := decodeOp(t, in, opBytes)
	if same, err := in.CompareHLC(op.HLC, hlcOf(t, v.Input, "remove_hlc")); err != nil || same != 0 {
		t.Fatalf("the frozen op is not stamped with the vector's remove_hlc: %v", err)
	}
	cited := hlcOf(t, v.Input, "cited_add_tag_hlc")
	if len(op.Observed) != 1 {
		t.Fatalf("the remove cites %d add-tags, want the single frozen one", len(op.Observed))
	}
	if same, err := in.CompareHLC(op.Observed[0].HLC, cited); err != nil || same != 0 {
		t.Fatalf("the remove cites %+v, the vector froze %+v", op.Observed[0].HLC, cited)
	}
	if cmp, err := in.CompareHLC(cited, op.HLC); err != nil || cmp <= 0 {
		t.Fatal("the vector's premise is broken: the cited add-tag must outrank the remove, or " +
			"there is nothing causally impossible about it")
	}

	wantRefusal(t, in.ValidateOp(opBytes, receiverNowMS), v.Expected, "ValidateOp")

	// And the ingest path refuses it too. A validator nobody calls is not a gate,
	// so the full path is checked separately rather than assumed to run it.
	eng, err := in.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()
	_, err = eng.IngestAmbientAuthenticated(opBytes, receiverNowMS)
	wantRefusal(t, err, v.Expected, "IngestAmbientAuthenticated")
}

// SYNC-DEATH-01: a death certificate dominates a concurrent add with a
// NUMERICALLY GREATER HLC. The death dimension is not last-write-wins.
func driveDeathDomination(t *testing.T, in *kotvasync.Instance, v vector) {
	death := hexIn(t, v.Input, "death_op_cbor_hex")
	add := hexIn(t, v.Input, "concurrent_add_op_cbor_hex")
	deathOp, addOp := decodeOp(t, in, death), decodeOp(t, in, add)

	// The premise: the add really does outrank the death by the clock. Without
	// this the vector would pass for an engine that merely does LWW.
	if cmp, err := in.CompareHLC(addOp.HLC, deathOp.HLC); err != nil || cmp <= 0 {
		t.Fatal("the vector's premise is broken: the concurrent add must carry the GREATER HLC")
	}
	if same, err := in.CompareHLC(deathOp.HLC, hlcOf(t, v.Input, "death_hlc")); err != nil || same != 0 {
		t.Fatalf("the death op is not stamped with the vector's death_hlc: %v", err)
	}
	if same, err := in.CompareHLC(addOp.HLC, hlcOf(t, v.Input, "concurrent_add_hlc")); err != nil || same != 0 {
		t.Fatalf("the add op is not stamped with the vector's concurrent_add_hlc: %v", err)
	}

	want := boolOf(v.Expected, "present")
	for _, order := range []struct {
		name string
		ops  [][]byte
	}{
		{"death first", [][]byte{death, add}},
		{"add first", [][]byte{add, death}},
	} {
		eng := ingestAll(t, in, order.ops)
		present, err := eng.SetContains(addOp.Target, addOp.Value)
		if err != nil {
			eng.Close()
			t.Fatalf("SetContains (%s): %v", order.name, err)
		}
		if present != want {
			eng.Close()
			t.Fatalf("%s: the element's presence is %v, want %v — a death certificate must "+
				"dominate a concurrent set-add whatever its HLC", order.name, present, want)
		}
		state, err := eng.DeathState(deathOp.Target)
		if err != nil {
			eng.Close()
			t.Fatalf("DeathState (%s): %v", order.name, err)
		}
		if !state.Deleted {
			eng.Close()
			t.Fatalf("%s: the object is not marked deleted at all", order.name)
		}
		if class := str(t, v.Input, "death_class"); state.Class == nil || *state.Class != class {
			eng.Close()
			t.Fatalf("%s: the death class is %v, want %q", order.name, state.Class, class)
		}
		eng.Close()
	}

	// `rule` restates in prose what the two orders above measure; not asserted.
}

// SYNC-DEATH-02: at an exact HLC tie the death dimension fails SAFE — Deleted
// beats Live, on either arrival order.
func driveDeathTie(t *testing.T, in *kotvasync.Instance, v vector) {
	death := hexIn(t, v.Input, "death_op_cbor_hex")
	live := hexIn(t, v.Input, "live_op_cbor_hex")
	deathOp, liveOp := decodeOp(t, in, death), decodeOp(t, in, live)

	if cmp, err := in.CompareHLC(deathOp.HLC, liveOp.HLC); err != nil || cmp != 0 {
		t.Fatal("the vector's premise is broken: the two writes must share one identical HLC, " +
			"or the winner is decided by the clock and the failsafe is never exercised")
	}
	if same, err := in.CompareHLC(deathOp.HLC, hlcOf(t, v.Input, "hlc")); err != nil || same != 0 {
		t.Fatalf("the tied ops are not stamped with the vector's frozen HLC: %v", err)
	}
	if want := strOf(t, v.Expected, "winner"); want != "Deleted" {
		t.Fatalf("this vector now names %q as the winner; the failsafe direction changed", want)
	}

	for _, order := range []struct {
		name string
		ops  [][]byte
	}{
		{"death first", [][]byte{death, live}},
		{"live first", [][]byte{live, death}},
	} {
		eng := ingestAll(t, in, order.ops)
		state, err := eng.DeathState(deathOp.Target)
		eng.Close()
		if err != nil {
			t.Fatalf("DeathState (%s): %v", order.name, err)
		}
		if !state.Deleted {
			t.Fatalf("%s: the object came out LIVE at an exact tie — the death dimension must "+
				"fail safe toward deletion", order.name)
		}
		if class := strOf(t, v.Expected, "class"); state.Class == nil || *state.Class != class {
			t.Fatalf("%s: the death class is %v, want %q", order.name, state.Class, class)
		}
	}
}

// SYNC-PN-01: the PN-counter merges the per-author UNION of op-id-keyed deltas —
// commutative, associative and idempotent — never a per-author max.
func drivePNMerge(t *testing.T, in *kotvasync.Instance, v vector) {
	ops := hexList(t, v.Input, "ops_cbor_hex")
	first := decodeOp(t, in, ops[0])
	target, field := first.Target, deref(first.Field)

	// Each op's frozen op-id must be the one the engine computes: the whole
	// idempotence claim rests on the id being a content address, so a replay is
	// only a replay if the bytes address to the same id.
	var frozenIDs []string
	if err := json.Unmarshal(v.Input["op_ids_hex"], &frozenIDs); err != nil {
		t.Fatalf("input/op_ids_hex: %v", err)
	}
	if len(frozenIDs) != len(ops) {
		t.Fatalf("the vector froze %d op-ids for %d ops", len(frozenIDs), len(ops))
	}
	distinct := map[string]bool{}
	for i, op := range ops {
		id := opID(t, in, op)
		if id != frozenIDs[i] {
			t.Fatalf("op %d addresses to %s, frozen as %s", i, id, frozenIDs[i])
		}
		distinct[id] = true
	}
	if int64(len(distinct)) != num(t, v.Expected, "distinct_op_ids") {
		t.Fatalf("the %d ops carry %d distinct op-ids, want %d — the replayed op must be "+
			"byte-identical to the one it replays", len(ops), len(distinct),
			num(t, v.Expected, "distinct_op_ids"))
	}

	// The vector's own deltas must sum, over DISTINCT ops only, to the frozen
	// total — so the number below is a consequence of the algebra rather than a
	// second thing to trust.
	var deltas []struct {
		Author string `json:"author_hex"`
		Delta  int64  `json:"delta"`
	}
	if err := json.Unmarshal(v.Input["deltas"], &deltas); err != nil {
		t.Fatalf("input/deltas: %v", err)
	}
	if len(deltas) != len(ops) {
		t.Fatalf("the vector froze %d deltas for %d ops", len(deltas), len(ops))
	}
	seen, sum := map[string]bool{}, int64(0)
	for i, d := range deltas {
		if seen[frozenIDs[i]] {
			continue
		}
		seen[frozenIDs[i]] = true
		sum += d.Delta
	}
	if sum != num(t, v.Expected, "total") {
		t.Fatalf("the vector's distinct deltas sum to %d but it freezes a total of %d",
			sum, num(t, v.Expected, "total"))
	}

	eng := ingestAll(t, in, ops)
	defer eng.Close()

	total, err := eng.CounterTotal(target, field)
	if err != nil {
		t.Fatalf("CounterTotal: %v", err)
	}
	if want := strconv.FormatInt(num(t, v.Expected, "total"), 10); total != want {
		t.Fatalf("the counter totals %s, want %s", total, want)
	}

	entries, err := eng.CounterEntries(target, field)
	if err != nil {
		t.Fatalf("CounterEntries: %v", err)
	}
	var wantP, wantN map[string]int64
	if err := json.Unmarshal(v.Expected["P"], &wantP); err != nil {
		t.Fatalf("expected/P: %v", err)
	}
	if err := json.Unmarshal(v.Expected["N"], &wantN); err != nil {
		t.Fatalf("expected/N: %v", err)
	}
	gotP, gotN := map[string]int64{}, map[string]int64{}
	for _, e := range entries {
		gotP[e.Author] = e.P
		gotN[e.Author] = e.N
	}
	if !reflect.DeepEqual(gotP, wantP) || !reflect.DeepEqual(gotN, wantN) {
		t.Fatalf("per-author entries are P%v N%v, want P%v N%v — §4.6 merges the union of "+
			"op-id-keyed deltas, and a per-author max would silently lose one replica's subset",
			gotP, gotN, wantP, wantN)
	}

	if boolOf(v.Expected, "replay_is_noop") {
		for i, op := range ops {
			if _, err := eng.IngestAmbientAuthenticated(op, receiverNowMS); err != nil {
				t.Fatalf("re-delivering op %d: %v", i, err)
			}
		}
		after, err := eng.CounterTotal(target, field)
		if err != nil {
			t.Fatalf("CounterTotal after the replay: %v", err)
		}
		if after != total {
			t.Fatalf("re-delivering every op moved the total from %s to %s — a re-delivered op "+
				"double-counted, so the merge is not idempotent", total, after)
		}
	}

	if boolOf(v.Expected, "merge_is_associative") {
		reverse := ingestAll(t, in, reversedOps(ops))
		defer reverse.Close()
		assertSameState(t, eng, reverse, "the two apply orders")

		// Associativity where it actually bites: two replicas hold different
		// SUBSETS of one author's deltas and are merged. This is the case a
		// per-author max gets wrong while still passing every check above.
		var split struct {
			One []int `json:"replica_1_op_indices"`
			Two []int `json:"replica_2_op_indices"`
		}
		if err := json.Unmarshal(v.Input["partial_merge_subcase"], &split); err != nil {
			t.Fatalf("input/partial_merge_subcase: %v", err)
		}
		if len(split.One) == 0 || len(split.Two) == 0 {
			t.Fatal("the partial-merge subcase leaves one replica empty, so merging them proves " +
				"nothing about differing subsets")
		}
		pick := func(idx []int) [][]byte {
			out := make([][]byte, 0, len(idx))
			for _, i := range idx {
				if i < 0 || i >= len(ops) {
					t.Fatalf("the partial-merge subcase names op %d of %d", i, len(ops))
				}
				out = append(out, ops[i])
			}
			return out
		}
		one := ingestAll(t, in, pick(split.One))
		defer one.Close()
		two := ingestAll(t, in, pick(split.Two))
		defer two.Close()
		if err := one.Merge(two); err != nil {
			t.Fatalf("merging the two partial replicas: %v", err)
		}
		assertSameState(t, eng, one, "the union of two partial replicas and a full replay")
	}
}

// SYNC-PN-02: an author may only ever move its OWN P/N entry.
func driveCounterForeign(t *testing.T, in *kotvasync.Instance, v vector) {
	if got := strOf(t, v.Expected, "outcome"); got != "reject" {
		t.Fatalf("this vector's outcome is now %q; the driver below asserts a rejection", got)
	}
	opAuthor := hexIn(t, v.Input, "op_hlc_author_hex")
	entryAuthor := hexIn(t, v.Input, "target_entry_author_hex")
	if equalHex(opAuthor, entryAuthor) {
		t.Fatal("the vector's two authors are the same key, so nothing foreign is being touched")
	}
	wantRefusal(t, in.CheckCounterEntry(opAuthor, entryAuthor), v.Expected, "CheckCounterEntry")

	// Both authors may move their own entry, so the check is a rule about
	// ownership rather than a refusal to move anything.
	for _, a := range [][]byte{opAuthor, entryAuthor} {
		if err := in.CheckCounterEntry(a, a); err != nil {
			t.Fatalf("CheckCounterEntry refused an author's own entry (%x): %v", a[:8], err)
		}
	}
}

// SYNC-RGA-01: atoms sharing a left-origin order by descending element id —
// newer-first — and arrival order does not touch that.
func driveRGASiblingOrder(t *testing.T, in *kotvasync.Instance, v vector) {
	origin := hexIn(t, v.Input, "origin_op_cbor_hex")
	siblings := hexList(t, v.Input, "sibling_ops_cbor_hex")
	if len(siblings) < 2 {
		t.Fatalf("the vector froze %d siblings; ordering needs at least two", len(siblings))
	}

	// The siblings' own stamps and values, held against the vector's description
	// of them, so the order below is an order over the ops the vector means.
	var frozenHLCs []hlcJSON
	if err := json.Unmarshal(v.Input["sibling_hlcs"], &frozenHLCs); err != nil {
		t.Fatalf("input/sibling_hlcs: %v", err)
	}
	var frozenValues []string
	if err := json.Unmarshal(v.Input["sibling_values"], &frozenValues); err != nil {
		t.Fatalf("input/sibling_values: %v", err)
	}
	for i, raw := range siblings {
		op := decodeOp(t, in, raw)
		if same, err := in.CompareHLC(op.HLC, frozenHLCs[i].hlc()); err != nil || same != 0 {
			t.Fatalf("sibling %d is stamped %+v, the vector froze %+v", i, op.HLC, frozenHLCs[i])
		}
		if got := tstr(t, op.Value); got != frozenValues[i] {
			t.Fatalf("sibling %d inserts %q, the vector froze %q", i, got, frozenValues[i])
		}
	}

	target := decodeOp(t, in, origin).Target
	read := func(ops [][]byte) (values, ids []string, state []byte) {
		eng := ingestAll(t, in, ops)
		defer eng.Close()
		seq, err := eng.Sequence(target)
		if err != nil || seq == nil {
			t.Fatalf("Sequence(%s) = %v, %v", target, seq, err)
		}
		for _, val := range seq.Values {
			values = append(values, tstr(t, val))
		}
		for _, a := range seq.Atoms {
			ids = append(ids, hlcHex(t, in, a.ID))
		}
		observable, err := eng.ObservableState()
		if err != nil {
			t.Fatalf("ObservableState: %v", err)
		}
		return values, ids, observable
	}

	forwardValues, forwardIDs, forwardState := read(append([][]byte{origin}, siblings...))
	reverseValues, reverseIDs, reverseState := read(append([][]byte{origin}, reversedOps(siblings)...))
	if !reflect.DeepEqual(forwardValues, reverseValues) || !reflect.DeepEqual(forwardIDs, reverseIDs) {
		t.Fatalf("arrival order changed the sequence:\n  forward  %v / %v\n  backward %v / %v",
			forwardValues, forwardIDs, reverseValues, reverseIDs)
	}
	if !equalHex(forwardState, reverseState) {
		t.Fatalf("arrival order changed the observable state:\n  forward  %x\n  backward %x",
			forwardState, reverseState)
	}

	// The origin atom leads; the frozen order names the siblings that follow it.
	var wantValues, wantIDs []string
	if err := json.Unmarshal(v.Expected["order_values"], &wantValues); err != nil {
		t.Fatalf("expected/order_values: %v", err)
	}
	if err := json.Unmarshal(v.Expected["order_by_element_id_desc"], &wantIDs); err != nil {
		t.Fatalf("expected/order_by_element_id_desc: %v", err)
	}
	if got := tail(forwardValues, len(wantValues)); !reflect.DeepEqual(got, wantValues) {
		t.Fatalf("the sibling values come out %v, want the sequence to end in %v — concurrent "+
			"siblings are ordered newer-first, not by arrival", forwardValues, wantValues)
	}
	if got := tail(forwardIDs, len(wantIDs)); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("the sibling element ids come out %v, want the sequence to end in %v",
			forwardIDs, wantIDs)
	}

	// ...and "descending" is checked as an order, not just matched as a list.
	for i := 1; i < len(wantIDs); i++ {
		a, b := frozenHLCs[len(frozenHLCs)-1].hlc(), frozenHLCs[0].hlc()
		cmp, err := in.CompareHLC(a, b)
		if err != nil {
			t.Fatalf("CompareHLC: %v", err)
		}
		if cmp <= 0 {
			t.Fatal("the vector's premise is broken: the sibling listed first in the expected " +
				"order must carry the GREATER element id")
		}
	}
}

// SYNC-RGA-02: an insert whose origin has been tombstoned still resolves —
// §4.7 keeps the tombstone until the stability cut precisely because a later
// insert may cite it.
func driveRGATombstoneOrigin(t *testing.T, in *kotvasync.Instance, v vector) {
	insertX := hexIn(t, v.Input, "insert_x_cbor_hex")
	removeX := hexIn(t, v.Input, "remove_x_cbor_hex")
	insertY := hexIn(t, v.Input, "insert_y_cbor_hex")
	if boolOf(v.Expected, "reject") {
		t.Fatal("this vector now expects the insert to be REJECTED; the driver below asserts it " +
			"resolves, so re-read §4.7 before changing either")
	}
	if !boolOf(v.Expected, "resolves") {
		t.Fatal("this vector no longer expects the insert to resolve")
	}

	// The citation is the whole point: insert_y's origin must be exactly the atom
	// remove_x tombstoned, which the vector freezes as y_ref_origin_hlc.
	xOp, yOp := decodeOp(t, in, insertX), decodeOp(t, in, insertY)
	origin := hlcOf(t, v.Input, "y_ref_origin_hlc")
	if same, err := in.CompareHLC(xOp.HLC, origin); err != nil || same != 0 {
		t.Fatalf("the frozen origin %+v is not insert_x's element id %+v — the insert is not "+
			"citing the tombstoned atom at all: %v", origin, xOp.HLC, err)
	}
	if yOp.Reference == nil || yOp.Reference.HLC == nil {
		t.Fatalf("insert_y carries no origin reference (%+v), so nothing about a tombstoned "+
			"origin is being exercised", yOp.Reference)
	}
	if same, err := in.CompareHLC(*yOp.Reference.HLC, origin); err != nil || same != 0 {
		t.Fatalf("insert_y cites origin %+v, the vector froze %+v", *yOp.Reference.HLC, origin)
	}

	eng := ingestAll(t, in, [][]byte{insertX, removeX, insertY})
	defer eng.Close()
	seq, err := eng.Sequence(xOp.Target)
	if err != nil || seq == nil {
		t.Fatalf("Sequence(%s) = %v, %v", xOp.Target, seq, err)
	}

	var wantVisible []string
	if err := json.Unmarshal(v.Expected["visible_sequence"], &wantVisible); err != nil {
		t.Fatalf("expected/visible_sequence: %v", err)
	}
	var visible []string
	for _, val := range seq.Values {
		visible = append(visible, tstr(t, val))
	}
	if !reflect.DeepEqual(visible, wantVisible) {
		t.Fatalf("the visible sequence is %v, want %v", visible, wantVisible)
	}

	// atom_order_incl_tombstones is a LABEL list — rendered here from the actual
	// atom order rather than restated, so it measures the order and the tombstone
	// flag together.
	var wantAtoms []string
	if err := json.Unmarshal(v.Expected["atom_order_incl_tombstones"], &wantAtoms); err != nil {
		t.Fatalf("expected/atom_order_incl_tombstones: %v", err)
	}
	var labels []string
	resolved := false
	for _, a := range seq.Atoms {
		label := tstrOrEmpty(a.Value)
		if a.Tombstoned {
			label += "(tombstoned)"
		}
		labels = append(labels, label)
		if hlcHex(t, in, a.ID) == hlcHex(t, in, yOp.HLC) {
			resolved = true
		}
	}
	if !reflect.DeepEqual(labels, wantAtoms) {
		t.Fatalf("the atom order including tombstones is %v, want %v — dropping the tombstone "+
			"leaves the insert citing it with nothing to attach to", labels, wantAtoms)
	}
	if !resolved {
		t.Fatal("insert_y's atom is not in the sequence at all, so its tombstoned origin did " +
			"not resolve")
	}

	// `atom_order_incl_tombstones_is` documents that the list above is labels
	// rather than normative bytes; prose, not asserted.
}

// SYNC-TREE-01: a concurrent move that would close a cycle is SKIPPED — the same
// way on every replica, from any arrival order. A skip is a convergent outcome,
// never an error.
func driveTreeMoveReplay(t *testing.T, in *kotvasync.Instance, v vector) {
	baseline := hexList(t, v.Input, "baseline_ops_cbor_hex")
	colliding := hexList(t, v.Input, "colliding_ops_cbor_hex")
	ops := append(append([][]byte{}, baseline...), colliding...)
	if len(colliding) != 2 {
		t.Fatalf("the vector froze %d colliding moves; the labelling below assumes the frozen "+
			"pair", len(colliding))
	}
	if boolOf(v.Expected, "skipped_is_error") {
		t.Fatal("a skipped move is a convergent outcome and must never be an error; this " +
			"vector now says otherwise")
	}
	if !boolOf(v.Expected, "acyclic") {
		t.Fatal("this vector no longer expects an acyclic result")
	}

	// The vector labels its colliding moves h1/h2 and names their stamps, so the
	// skip below can be reported by label instead of by hex.
	var moves []struct {
		Label string  `json:"label"`
		HLC   hlcJSON `json:"hlc"`
	}
	if err := json.Unmarshal(v.Input["colliding_moves"], &moves); err != nil {
		t.Fatalf("input/colliding_moves: %v", err)
	}
	if len(moves) != len(colliding) {
		t.Fatalf("the vector labels %d moves for %d colliding ops", len(moves), len(colliding))
	}
	label := map[string]string{}
	for i, mv := range moves {
		op := decodeOp(t, in, colliding[i])
		if same, err := in.CompareHLC(op.HLC, mv.HLC.hlc()); err != nil || same != 0 {
			t.Fatalf("colliding op %d is stamped %+v, the vector labels %+v as %s",
				i, op.HLC, mv.HLC, mv.Label)
		}
		label[hlcHex(t, in, op.HLC)] = mv.Label
	}
	// The premise §4.8 turns on: replay is ASCENDING by HLC, so the EARLIER move
	// is the one that survives — deliberately not last-writer-wins.
	if cmp, err := in.CompareHLC(moves[0].HLC.hlc(), moves[1].HLC.hlc()); err != nil || cmp >= 0 {
		t.Fatal("the vector's premise is broken: the move it expects to survive must sort " +
			"BEFORE the one it expects to be skipped")
	}

	var wantEdgeRows []struct {
		Node   string `json:"node"`
		Parent string `json:"parent"`
		Ord    string `json:"ord"`
	}
	if err := json.Unmarshal(v.Expected["final_edges"], &wantEdgeRows); err != nil {
		t.Fatalf("expected/final_edges: %v", err)
	}
	var wantEdges [][]string
	for _, e := range wantEdgeRows {
		wantEdges = append(wantEdges, []string{e.Node, e.Parent, e.Ord})
	}
	sortEdges(wantEdges)
	var wantApplied, wantSkipped []string
	if err := json.Unmarshal(v.Expected["applied"], &wantApplied); err != nil {
		t.Fatalf("expected/applied: %v", err)
	}
	if err := json.Unmarshal(v.Expected["skipped"], &wantSkipped); err != nil {
		t.Fatalf("expected/skipped: %v", err)
	}

	// Three arrival orders, including one that delivers the colliding pair before
	// the baseline: a replica that re-evaluates the affected subtree in HLC order
	// reaches the identical tree from all of them.
	orders := [][][]byte{
		ops,
		reversedOps(ops),
		{ops[len(ops)-1], ops[len(ops)-2], ops[0], ops[1]},
	}
	var firstState []byte
	for i, order := range orders {
		eng := ingestAll(t, in, order)
		tree, err := eng.Tree()
		if err != nil {
			eng.Close()
			t.Fatalf("Tree (arrival order %d): %v", i, err)
		}
		state, err := eng.ObservableState()
		eng.Close()
		if err != nil {
			t.Fatalf("ObservableState (arrival order %d): %v", i, err)
		}

		edges := append([][]string(nil), tree.Edges...)
		sortEdges(edges)
		if !reflect.DeepEqual(edges, wantEdges) {
			t.Fatalf("arrival order %d produced the tree %v, want %v", i, edges, wantEdges)
		}
		if got := labelled(t, in, tree.Skipped, label); !reflect.DeepEqual(got, wantSkipped) {
			t.Fatalf("arrival order %d skipped %v, want %v — the move that closes a cycle is "+
				"skipped deterministically, and which one is not a matter of arrival", i, got, wantSkipped)
		}
		if got := labelled(t, in, tree.Applied, label); !reflect.DeepEqual(got, wantApplied) {
			t.Fatalf("arrival order %d applied the colliding moves %v, want %v", i, got, wantApplied)
		}
		assertAcyclic(t, tree.Edges, i)
		if i == 0 {
			firstState = state
			continue
		}
		if !equalHex(state, firstState) {
			t.Fatalf("arrival order %d produced a different observable state:\n  order 0 %x\n"+
				"  order %d %x", i, firstState, i, state)
		}
	}

	if !boolOf(v.Expected, "apply_order_independent") {
		t.Fatal("this vector no longer claims apply-order independence, which is the property " +
			"the three arrival orders above exist to measure")
	}
}

// ---------------------------------------------------------------------------
// Drivers — §5/§6 the wire, the checkpoint and the floor
// ---------------------------------------------------------------------------

// SYNC-SNAP-01: the canonical six-section observable state, its root, and what
// does and does not change either.
func driveSnapshotStateRoot(t *testing.T, in *kotvasync.Instance, v vector) {
	// The vector describes its state in prose scalars, so the state is BUILT from
	// that description and required to encode to the frozen bytes. Decoding the
	// frozen bytes and re-encoding them (below) would only prove the codec is a
	// bijection; this proves the engine spells this state these bytes.
	described := observableStateOf(t, v.Input, "observable_state")
	encoded, err := in.EncodeObservableState(described)
	if err != nil {
		t.Fatalf("EncodeObservableState: %v", err)
	}
	wantHex(t, "observable_state_cbor_hex", encoded, v.Expected)

	root, err := in.ObservableStateRoot(encoded)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	wantHex(t, "root_hex", root, v.Expected)
	// The same state must address the same way every time it is hashed; the
	// vector states this as same_covers_same_root.
	if !boolOf(v.Expected, "same_covers_same_root") {
		t.Fatal("this vector no longer claims a deterministic root")
	}
	again, err := in.ObservableStateRoot(encoded)
	if err != nil {
		t.Fatalf("ObservableStateRoot (second call): %v", err)
	}
	if !equalHex(again, root) {
		t.Fatalf("hashing one state twice gave %x then %x", root, again)
	}

	// §6.1.1 re-sorts sections canonically on the way out, which is what makes
	// "equal bytes" a usable definition of "equal state": a decode/encode round
	// trip and a shuffled section order must both reproduce these bytes.
	decoded, err := in.DecodeObservableState(encoded)
	if err != nil {
		t.Fatalf("DecodeObservableState: %v", err)
	}
	roundTripped, err := in.EncodeObservableState(decoded)
	if err != nil {
		t.Fatalf("re-EncodeObservableState: %v", err)
	}
	if !equalHex(roundTripped, encoded) {
		t.Fatalf("the observable state did not survive a decode/encode round trip:\n"+
			"  got  %x\n  want %x", roundTripped, encoded)
	}
	shuffled := decoded
	shuffled.Tree = reverseRows(decoded.Tree)
	shuffled.RGA = reverseRows(decoded.RGA)
	shuffledBytes, err := in.EncodeObservableState(shuffled)
	if err != nil {
		t.Fatalf("EncodeObservableState (shuffled sections): %v", err)
	}
	if !equalHex(shuffledBytes, encoded) {
		t.Fatalf("shuffling the section rows changed the encoding — section order would then "+
			"leak into the root:\n  got  %x\n  want %x", shuffledBytes, encoded)
	}

	// The empty state is a real state with six empty sections, not a special case.
	if want := num(t, v.Input, "empty_state_sections"); want != 6 {
		t.Fatalf("the vector describes %d sections; this driver builds the six §6.1.1 sections", want)
	}
	empty := kotvasync.ObservableState{
		ORSet: [][]json.RawMessage{}, LWW: [][]json.RawMessage{}, PN: [][]json.RawMessage{},
		Death: [][]json.RawMessage{}, RGA: [][]json.RawMessage{}, Tree: [][]json.RawMessage{},
	}
	emptyBytes, err := in.EncodeObservableState(empty)
	if err != nil {
		t.Fatalf("EncodeObservableState (empty): %v", err)
	}
	wantHex(t, "empty_state_cbor_hex", emptyBytes, v.Expected)
	emptyRoot, err := in.ObservableStateRoot(emptyBytes)
	if err != nil {
		t.Fatalf("ObservableStateRoot (empty): %v", err)
	}
	wantHex(t, "empty_state_root_hex", emptyRoot, v.Expected)

	// A one-value difference is a DIFFERENT root. Without this the root could be
	// a constant and every check above would still pass.
	diverged := decoded
	diverged.LWW = append([][]json.RawMessage{}, decoded.LWW...)
	if len(diverged.LWW) == 0 {
		t.Fatal("the frozen state has no LWW section to diverge, so nothing here shows the root " +
			"tracks the state")
	}
	row := append([]json.RawMessage{}, diverged.LWW[0]...)
	row[2] = kotvasync.Text("DIVERGED")
	diverged.LWW[0] = row
	divergedBytes, err := in.EncodeObservableState(diverged)
	if err != nil {
		t.Fatalf("EncodeObservableState (diverged): %v", err)
	}
	divergedRoot, err := in.ObservableStateRoot(divergedBytes)
	if err != nil {
		t.Fatalf("ObservableStateRoot (diverged): %v", err)
	}
	if equalHex(divergedRoot, root) {
		t.Fatalf("a diverged state addressed to the same root %x — the root does not commit to "+
			"the state", root)
	}

	// A root that does not match is evidence of divergence, so its §12 action is
	// HALT_ALERT rather than a retry.
	eng, err := in.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()
	code := strOf(t, v.Expected, "mismatch_error_code")
	if err := eng.VerifyRoot(root); !kotvasync.IsRefusal(err, code) {
		t.Fatalf("an empty replica verified against the frozen non-empty root with %v, want a "+
			"refusal with %s", err, code)
	}
	entry := assertRegistryEntry(t, in, code, strOf(t, v.Expected, "mismatch_error_name"))
	if want := strOf(t, v.Expected, "mismatch_action"); entry.Action != want {
		t.Fatalf("§12 %s carries action %q in the engine, frozen as %q — a mismatch is evidence "+
			"of divergence, and retrying it would paper over that", code, entry.Action, want)
	}
}

// SYNC-SNAP-02: adopting a checkpoint and then applying the suffix past `covers`
// lands on the SAME bytes as a full replay.
func driveSnapshotFastJoin(t *testing.T, in *kotvasync.Instance, v vector) {
	fastJoin := hexOf(t, v.Expected, "fast_join_state_cbor_hex")
	fullReplay := hexOf(t, v.Expected, "full_replay_state_cbor_hex")
	if !boolOf(v.Expected, "states_byte_identical") || !boolOf(v.Expected, "roots_equal") {
		t.Fatal("this vector no longer claims fast-join and full replay agree, which is the " +
			"whole of §5.2.1")
	}
	if !equalHex(fastJoin, fullReplay) {
		t.Fatalf("the frozen fast-join and full-replay states are not byte-identical:\n"+
			"  fast-join   %x\n  full replay %x", fastJoin, fullReplay)
	}

	// The snapshot's committed state must address to the snapshot's own root, or
	// the checkpoint being adopted is not the state it claims to be.
	snapState := hexIn(t, v.Input, "snapshot_observable_state_cbor_hex")
	snapRoot, err := in.ObservableStateRoot(snapState)
	if err != nil {
		t.Fatalf("ObservableStateRoot (snapshot state): %v", err)
	}
	if want := str(t, v.Input, "snapshot_root_hex"); hex.EncodeToString(snapRoot) != want {
		t.Fatalf("the snapshot's state addresses to %x, but it commits to %s", snapRoot, want)
	}

	// Now the actual claim: adopt that state, apply the ops past `covers` to the
	// adopted projection, and require the frozen fast-join bytes. Re-hashing the
	// frozen state would prove only that BLAKE3 works.
	adopted, err := in.DecodeObservableState(snapState)
	if err != nil {
		t.Fatalf("DecodeObservableState: %v", err)
	}
	kinds := opKinds(t, in)
	var described []struct {
		Kind   int     `json:"kind"`
		Target string  `json:"target"`
		Field  string  `json:"field"`
		Value  string  `json:"value"`
		HLC    hlcJSON `json:"hlc"`
	}
	if err := json.Unmarshal(v.Input["post_covers_ops"], &described); err != nil {
		t.Fatalf("input/post_covers_ops: %v", err)
	}
	suffix := hexList(t, v.Input, "post_covers_ops_cbor_hex")
	if len(suffix) != len(described) || len(suffix) == 0 {
		t.Fatalf("the vector froze %d suffix ops and %d descriptions of them",
			len(suffix), len(described))
	}
	for i, raw := range suffix {
		op := decodeOp(t, in, raw)
		want := described[i]
		if op.Kind != uint8(want.Kind) || op.Target != want.Target || deref(op.Field) != want.Field {
			t.Fatalf("suffix op %d is kind %d on (%s,%s), the vector describes kind %d on (%s,%s)",
				i, op.Kind, op.Target, deref(op.Field), want.Kind, want.Target, want.Field)
		}
		if got := tstr(t, op.Value); got != want.Value {
			t.Fatalf("suffix op %d writes %q, the vector describes %q", i, got, want.Value)
		}
		if same, err := in.CompareHLC(op.HLC, want.HLC.hlc()); err != nil || same != 0 {
			t.Fatalf("suffix op %d is stamped %+v, the vector describes %+v", i, op.HLC, want.HLC)
		}
		if op.Kind != kinds.LWWSet {
			t.Fatalf("suffix op %d is kind %d, but this driver applies the suffix by hand and "+
				"only knows lww_set (%d) — extend it rather than dropping the op",
				i, op.Kind, kinds.LWWSet)
		}
		adopted.LWW = applyLWW(t, adopted.LWW, op)
	}

	joined, err := in.EncodeObservableState(adopted)
	if err != nil {
		t.Fatalf("EncodeObservableState (after the suffix): %v", err)
	}
	if !equalHex(joined, fastJoin) {
		t.Fatalf("adopting the checkpoint and applying the suffix produced\n  %x\nbut the frozen "+
			"fast-join state is\n  %x", joined, fastJoin)
	}
	joinedRoot, err := in.ObservableStateRoot(joined)
	if err != nil {
		t.Fatalf("ObservableStateRoot (fast-join state): %v", err)
	}
	wantHex(t, "root_hex", joinedRoot, v.Expected)

	// `snapshot_covers_note` and `snapshot_observable_state_role` are prose about
	// what these fields are for; not asserted.
}

// SYNC-SNAP-03: the snapshot BODY is an op set, verified by fold-then-recompute,
// and a projection adopted without its HLCs diverges permanently.
func driveSnapshotBodyFold(t *testing.T, in *kotvasync.Instance, v vector) {
	body := hexIn(t, v.Input, "snapshot_body_cbor_hex")
	root := hexIn(t, v.Input, "snapshot_root_hex")
	if !boolOf(v.Expected, "body_folds_to_root") {
		t.Fatal("this vector no longer claims the body folds to the root it is offered against")
	}

	members, err := in.SnapshotBodyDecode(body)
	if err != nil {
		t.Fatalf("SnapshotBodyDecode: %v", err)
	}
	var describedOps []struct {
		Kind   int     `json:"kind"`
		Target string  `json:"target"`
		Field  string  `json:"field"`
		Value  string  `json:"value"`
		HLC    hlcJSON `json:"hlc"`
	}
	if err := json.Unmarshal(v.Input["snapshot_body_ops"], &describedOps); err != nil {
		t.Fatalf("input/snapshot_body_ops: %v", err)
	}
	if len(members) != len(describedOps) {
		t.Fatalf("the body carries %d members, the vector describes %d ops",
			len(members), len(describedOps))
	}
	// Each member is a signed op that verifies, and is the op the vector says it
	// is — otherwise the fold below could be over anything.
	for i, member := range members {
		payload, err := in.VerifySignedOp(mustHex(t, member))
		if err != nil {
			t.Fatalf("body member %d does not verify: %v", i, err)
		}
		op := decodeOp(t, in, payload)
		want := describedOps[i]
		if op.Kind != uint8(want.Kind) || op.Target != want.Target || deref(op.Field) != want.Field {
			t.Fatalf("body member %d is kind %d on (%s,%s), the vector describes kind %d on (%s,%s)",
				i, op.Kind, op.Target, deref(op.Field), want.Kind, want.Target, want.Field)
		}
		if same, err := in.CompareHLC(op.HLC, want.HLC.hlc()); err != nil || same != 0 {
			t.Fatalf("body member %d is stamped %+v, the vector describes %+v", i, op.HLC, want.HLC)
		}
	}

	// C-06 framing: members are embedded CBOR items, so re-encoding what was
	// decoded reproduces the body byte for byte.
	reencoded, err := in.SnapshotBodyEncode(members)
	if err != nil {
		t.Fatalf("SnapshotBodyEncode: %v", err)
	}
	if !equalHex(reencoded, body) {
		t.Fatalf("the body did not survive a decode/encode round trip:\n  got  %x\n  want %x",
			reencoded, body)
	}

	// §6.1.2's fold-then-recompute. Hashing the received bytes would prove only
	// that the sender shipped what it promised; this proves the ops PRODUCE the
	// committed state.
	state, err := in.SnapshotBodyVerifyRoot(body, root, "", receiverNowMS)
	if err != nil {
		t.Fatalf("SnapshotBodyVerifyRoot: %v", err)
	}
	wantHex(t, "folded_state_cbor_hex", state, v.Expected)
	if want := hexIn(t, v.Input, "observable_state_cbor_hex"); !equalHex(state, want) {
		t.Fatalf("the fold produced %x, but the snapshot commits to %x", state, want)
	}
	folded, err := in.ObservableStateRoot(state)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	wantHex(t, "folded_root_hex", folded, v.Expected)
	if !equalHex(folded, root) {
		t.Fatalf("the body folds to root %x, but it is offered against %x", folded, root)
	}

	// A body that does not reproduce the claimed root is discarded WHOLE.
	wrong := append([]byte(nil), root...)
	wrong[len(wrong)-1] ^= 0x01
	code := strOf(t, v.Expected, "body_mismatch_error_code")
	if _, err := in.SnapshotBodyVerifyRoot(body, wrong, "", receiverNowMS); !kotvasync.IsRefusal(err, code) {
		t.Fatalf("a body offered against a root it does not produce was accepted (%v), want a "+
			"refusal with %s", err, code)
	}
	assertRegistryEntry(t, in, code, strOf(t, v.Expected, "body_mismatch_error_name"))

	// The ordering premise this vector exists for: the post-`covers` op is
	// genuinely AFTER covers for its own author, and still BELOW the incumbent —
	// `covers` bounds each author's own stream, the §3 HLC orders across authors.
	postSigned := hexIn(t, v.Input, "post_covers_op_cbor_hex")
	postPayload, err := in.VerifySignedOp(postSigned)
	if err != nil {
		t.Fatalf("the post-covers op does not verify: %v", err)
	}
	post := decodeOp(t, in, postPayload)
	incumbentPayload, err := in.VerifySignedOp(mustHex(t, members[0]))
	if err != nil {
		t.Fatalf("body member 0 does not verify: %v", err)
	}
	incumbent := decodeOp(t, in, incumbentPayload)
	for _, mark := range coversMarks(t, in, str(t, v.Input, "snapshot_covers_cbor_hex")) {
		if mark.Author != post.HLC.Author {
			continue
		}
		if cmp, err := in.CompareHLC(post.HLC, mark.HLC); err != nil || cmp <= 0 {
			t.Fatal("the vector's premise is broken: the post-covers op must sort after its " +
				"own author's mark in `covers`")
		}
	}
	if cmp, err := in.CompareHLC(post.HLC, incumbent.HLC); err != nil || cmp >= 0 {
		t.Fatal("the vector's premise is broken: the post-covers op must sort BELOW the " +
			"incumbent write it loses to")
	}

	// A conformant replica FOLDED the body, so it holds the incumbent's HLC — and
	// keeps the incumbent's value when the later-arriving, lower-stamped op lands.
	conformant, err := in.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer conformant.Close()
	for i, member := range members {
		if _, err := conformant.IngestSigned(mustHex(t, member), receiverNowMS); err != nil {
			t.Fatalf("ingesting body member %d: %v", i, err)
		}
	}
	if _, err := conformant.IngestSigned(postSigned, receiverNowMS); err != nil {
		t.Fatalf("ingesting the post-covers op: %v", err)
	}
	after, err := conformant.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState: %v", err)
	}
	wantHex(t, "state_after_post_op_cbor_hex", after, v.Expected)
	afterRoot, err := in.ObservableStateRoot(after)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	wantHex(t, "root_after_post_op_hex", afterRoot, v.Expected)
	cell, err := conformant.LWWCell(incumbent.Target, deref(incumbent.Field))
	if err != nil || cell == nil {
		t.Fatalf("LWWCell = %v, %v", cell, err)
	}
	if got := tstr(t, cell.Value); got != strOf(t, v.Expected, "winning_value_after_post_op") {
		t.Fatalf("the winning value after the post-covers op is %q, want %q — a replica that "+
			"folded the body holds the incumbent's HLC and must keep it",
			got, strOf(t, v.Expected, "winning_value_after_post_op"))
	}

	// A replica that adopted a PROJECTION has the value but not its HLC, so the
	// same op wins there — a different root, permanently, with no error raised on
	// either side. That is why §6.1.2 forbids adopting a state document.
	if !boolOf(v.Expected, "projection_adopt_is_nonconformant") || !boolOf(v.Expected, "roots_differ") {
		t.Fatal("this vector no longer claims a projection-adopter diverges, which is the " +
			"reason the body is an op set at all")
	}
	projection, err := in.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer projection.Close()
	if _, err := projection.IngestSigned(postSigned, receiverNowMS); err != nil {
		t.Fatalf("ingesting the post-covers op into the projection-adopter: %v", err)
	}
	projected, err := projection.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState: %v", err)
	}
	wantHex(t, "projection_adopt_state_cbor_hex", projected, v.Expected)
	projectedRoot, err := in.ObservableStateRoot(projected)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	wantHex(t, "projection_adopt_root_hex", projectedRoot, v.Expected)
	if equalHex(projectedRoot, afterRoot) {
		t.Fatal("the projection-adopter reached the same root as the conformant replica, so the " +
			"divergence this vector freezes was not reproduced")
	}
}

// SYNC-VAL-01: the recursive ext-value boundary, from both sides.
func driveExtValue(t *testing.T, in *kotvasync.Instance, v vector) {
	type valueCase struct {
		Case    string `json:"case"`
		CBORHex string `json:"cbor_hex"`
	}
	var accept, reject []valueCase
	if err := json.Unmarshal(v.Input["accept"], &accept); err != nil {
		t.Fatalf("input/accept: %v", err)
	}
	if err := json.Unmarshal(v.Input["reject"], &reject); err != nil {
		t.Fatalf("input/reject: %v", err)
	}
	if len(accept) == 0 || len(reject) == 0 {
		t.Fatalf("the ext-value vector froze %d accept and %d reject cases", len(accept), len(reject))
	}
	if !boolOf(v.Expected, "accept_all") || !boolOf(v.Expected, "reject_all") {
		t.Fatal("this vector no longer expects every accept case accepted and every reject case " +
			"refused")
	}

	// Every accept case must DECODE and VALIDATE and re-encode byte-identically.
	// Both stages matter: conflating an encoder-side refusal (the value could not
	// be built at all) with a validator-side one is how a legitimate shape gets
	// silently locked out.
	seenAccept := map[string]bool{}
	for _, c := range accept {
		seenAccept[c.Case] = true
		raw := mustHex(t, c.CBORHex)
		tagged, err := in.DecodeValue(raw)
		if err != nil {
			t.Fatalf("accept case %q could not even be decoded: %v", c.Case, err)
		}
		ok, err := in.IsExtValue(tagged)
		if err != nil {
			t.Fatalf("accept case %q: IsExtValue: %v", c.Case, err)
		}
		if !ok {
			t.Fatalf("accept case %q decoded but did not validate as an ext-value (%s)",
				c.Case, tagged)
		}
		again, err := in.EncodeValue(tagged)
		if err != nil {
			t.Fatalf("accept case %q re-encode: %v", c.Case, err)
		}
		if !equalHex(again, raw) {
			t.Fatalf("accept case %q did not survive a round trip: %s -> %x", c.Case, c.CBORHex, again)
		}
	}

	// A reject case is refused at ONE of two stages and both count: a
	// non-canonical item cannot be decoded at all, while an integer-keyed map
	// decodes as CBOR and is then not an ext-value. What is forbidden is
	// accepting it — that would put a value in the algebra a second decoder
	// refuses, which is divergence by another route.
	seenReject := map[string]bool{}
	for _, c := range reject {
		seenReject[c.Case] = true
		tagged, err := in.DecodeValue(mustHex(t, c.CBORHex))
		if err != nil {
			continue
		}
		ok, err := in.IsExtValue(tagged)
		if err != nil {
			t.Fatalf("reject case %q: IsExtValue: %v", c.Case, err)
		}
		if ok {
			t.Fatalf("reject case %q was ACCEPTED as an ext-value (%s)", c.Case, tagged)
		}
	}
	// The recursion is the point: the violation nested at depth 2 must be caught
	// by the same walk, not waved through by a shallow check of the outer map.
	if !boolOf(v.Expected, "validation_is_recursive") {
		t.Fatal("this vector no longer claims validation is recursive")
	}
	if !seenReject["nested_int_keyed_map"] {
		t.Fatal("the reject cases no longer include a nested violation, so nothing here shows " +
			"the walk recurses")
	}
	// C-14: the empty map is key-type-ambiguous but vacuously so, and its
	// non-empty integer-keyed sibling is still refused.
	if !boolOf(v.Expected, "empty_map_is_ext_value") ||
		!boolOf(v.Expected, "empty_map_key_type_is_undeterminable") ||
		!boolOf(v.Expected, "nonempty_int_keyed_map_still_rejected") {
		t.Fatal("this vector's C-14 premises about the empty map no longer hold")
	}
	if !seenAccept["map_empty"] || !seenAccept["array_empty"] || !seenReject["int_keyed_map"] {
		t.Fatal("the C-14 cases (map_empty, array_empty, int_keyed_map) are no longer in the " +
			"accept/reject lists that exercise them")
	}
	if want := strOf(t, v.Expected, "empty_map_cbor_hex"); want != "a0" {
		t.Fatalf("the empty map is frozen as %s, not a0", want)
	}

	// The frozen refusal is the engine's own §12 row, read out of the registry
	// rather than restated here.
	code := strOf(t, v.Expected, "reject_error_code")
	assertRegistryEntry(t, in, code, strOf(t, v.Expected, "reject_error_name"))

	// The carrier op — the intended end-to-end shape, an op whose value is one of
	// the accepted nested maps — must validate as a whole op and round-trip.
	if !boolOf(v.Expected, "carrier_op_accepted") {
		t.Fatal("this vector no longer expects its carrier op to be accepted")
	}
	carrier := hexIn(t, v.Input, "carrier_op_cbor_hex")
	if err := in.ValidateOp(carrier, receiverNowMS); err != nil {
		t.Fatalf("the carrier op was refused: %v", err)
	}
	carrierOp := decodeOp(t, in, carrier)
	if again, err := in.EncodeOp(carrierOp); err != nil || !equalHex(again, carrier) {
		t.Fatalf("the carrier op did not survive a round trip (%v):\n  got  %x\n  want %x",
			err, again, carrier)
	}

	// §4.1.1: nesting is REPRESENTATION. The merge unit is the whole value, so a
	// concurrent write of a different nested map replaces it entire — there is no
	// per-key merge at this boundary.
	rival := carrierOp
	rival.HLC.Counter++
	rival.Value = json.RawMessage(`{"tmap":[["x",{"int":99}]]}`)
	rivalBytes, err := in.EncodeOp(rival)
	if err != nil {
		t.Fatalf("encoding the rival op: %v", err)
	}
	rivalValue, err := in.EncodeValue(rival.Value)
	if err != nil {
		t.Fatalf("encoding the rival value: %v", err)
	}
	eng := ingestAll(t, in, [][]byte{carrier, rivalBytes})
	defer eng.Close()
	cell, err := eng.LWWCell(carrierOp.Target, deref(carrierOp.Field))
	if err != nil || cell == nil {
		t.Fatalf("LWWCell = %v, %v", cell, err)
	}
	winning, err := in.EncodeValue(cell.Value)
	if err != nil {
		t.Fatalf("encoding the winning value: %v", err)
	}
	if !equalHex(winning, rivalValue) {
		t.Fatalf("the winning value is %x, want the rival's whole value %x — a per-key merge at "+
			"this boundary would mean two replicas can hold different maps under one HLC",
			winning, rivalValue)
	}

	// C-14: the depth ceiling is a MUST for ALL sync decoding, so it is
	// demonstrated on a decode path other than the bare value grammar.
	if !boolOf(v.Expected, "max_nesting_depth_is_a_MUST") {
		t.Fatal("this vector no longer makes the nesting ceiling a MUST")
	}
	depth := num(t, v.Expected, "max_nesting_depth")
	if depth != 64 {
		t.Fatalf("the frozen nesting ceiling is %d, not 64", depth)
	}
	overDeep := make([]byte, 0, depth+3)
	for i := int64(0); i < depth+2; i++ {
		overDeep = append(overDeep, 0x81)
	}
	overDeep = append(overDeep, 0x00)
	if _, err := in.SnapshotBodyDecode(overDeep); !kotvasync.IsRefusal(err, code) {
		t.Fatalf("a snapshot body nested %d levels deep decoded as %v, want a refusal with %s "+
			"before the recursion completes", depth+2, err, code)
	}

	// C-13(b): the sub-token is observational and must never be a gate. Both are
	// the vector's own statements; the spelling is asserted, the note is prose.
	if want := "sync-1/ext-value-2"; strOf(t, v.Expected, "value_profile_subtoken") != want {
		t.Fatalf("the frozen value-profile sub-token is %q, want %q",
			strOf(t, v.Expected, "value_profile_subtoken"), want)
	}
	if boolOf(v.Expected, "value_profile_subtoken_is_a_gate") {
		t.Fatal("the value-profile sub-token must never be a gate")
	}
}

// SYNC-FJ-01: the frozen fast-join and pull response, the snapshot signature
// minted outside the engine, and the inline body held to fold-then-recompute.
func driveFastJoinPullResponse(t *testing.T, in *kotvasync.Instance, v vector) {
	// The snapshot is rebuilt from the vector's INPUTS and re-signed through the
	// detached path — the seed never enters the engine — so the frozen bytes are a
	// known answer rather than something read back out of themselves.
	version, err := in.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	covers := coversMarks(t, in, str(t, v.Input, "snapshot_covers_cbor_hex"))
	if len(covers) == 0 {
		t.Fatal("the snapshot covers nothing, so there is no gap for a fast-join to close")
	}
	unsigned := kotvasync.Snapshot{
		Suite:  uint8(version.Suite),
		NS:     str(t, v.Input, "snapshot_ns"),
		Covers: covers,
		Root:   str(t, v.Input, "snapshot_root_hex"),
		TS:     uint64(num(t, v.Input, "snapshot_ts")),
		Signer: str(t, v.Input, "snapshot_signer_pubkey_hex"),
	}
	preimage, err := in.SnapshotSigningInput(unsigned)
	if err != nil {
		t.Fatalf("SnapshotSigningInput: %v", err)
	}
	wantHex(t, "snapshot_sig_preimage_hex", preimage, v.Expected)
	sig := ed25519.Sign(
		ed25519.NewKeyFromSeed(hexIn(t, v.Input, "snapshot_signer_seed_hex")), preimage)
	if hex.EncodeToString(sig) != strOf(t, v.Expected, "snapshot_sig_hex") {
		t.Fatalf("signing the snapshot preimage produced %x, not the frozen signature %s",
			sig, strOf(t, v.Expected, "snapshot_sig_hex"))
	}
	snapshot, err := in.SnapshotAssemble(unsigned, sig)
	if err != nil {
		t.Fatalf("SnapshotAssemble: %v", err)
	}
	wantHex(t, "snapshot_cbor_hex", snapshot, v.Expected)
	if err := in.SnapshotVerify(snapshot); err != nil {
		t.Fatalf("the reassembled snapshot does not verify: %v", err)
	}
	decoded, err := in.SnapshotDecode(snapshot)
	if err != nil {
		t.Fatalf("SnapshotDecode: %v", err)
	}
	if decoded.Root != unsigned.Root || decoded.Signer != unsigned.Signer ||
		decoded.TS != unsigned.TS || decoded.NS != unsigned.NS {
		t.Fatalf("the snapshot decoded as %+v, want the fields it was built from %+v",
			decoded, unsigned)
	}
	if decoded.Sig != strOf(t, v.Expected, "snapshot_sig_hex") {
		t.Fatalf("the snapshot carries signature %s", decoded.Sig)
	}

	// The floor is taken from the frozen FastJoin through the engine's own decoder
	// and then held against the vector's separately frozen floor bytes.
	frozenFastJoin := hexOf(t, v.Expected, "fastjoin_cbor_hex")
	fj, err := in.FastJoinDecode(frozenFastJoin)
	if err != nil {
		t.Fatalf("FastJoinDecode: %v", err)
	}
	floorBytes, err := in.EncodeHLC(fj.Floor)
	if err != nil {
		t.Fatalf("EncodeHLC: %v", err)
	}
	if want := str(t, v.Input, "floor_hlc_cbor_hex"); hex.EncodeToString(floorBytes) != want {
		t.Fatalf("the fast-join's floor encodes to %x, the vector froze %s", floorBytes, want)
	}

	byRef, err := in.FastJoinEncode(kotvasync.FastJoin{Snapshot: decoded, Floor: fj.Floor})
	if err != nil {
		t.Fatalf("FastJoinEncode: %v", err)
	}
	wantHex(t, "fastjoin_cbor_hex", byRef, v.Expected)
	roundTripped, err := in.FastJoinEncode(fj)
	if err != nil {
		t.Fatalf("re-FastJoinEncode: %v", err)
	}
	if !equalHex(roundTripped, frozenFastJoin) {
		t.Fatalf("the fast-join did not survive a decode/encode round trip:\n  got  %x\n  want %x",
			roundTripped, frozenFastJoin)
	}

	// The §5.2.1 pull response is `{2: FastJoin}` — and NOT `{1: [ops]}`, which is
	// the answer this caller must not be given. The frozen key list is checked
	// against the envelope actually built.
	pull := wrapPullEnvelope(t, in, byRef)
	wantHex(t, "pull_response_cbor_hex", pull, v.Expected)
	var wantKeys []int64
	if err := json.Unmarshal(v.Expected["pull_response_keys"], &wantKeys); err != nil {
		t.Fatalf("expected/pull_response_keys: %v", err)
	}
	if got := envelopeKeys(t, in, pull); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("the pull response carries keys %v, want %v", got, wantKeys)
	}
	if boolOf(v.Expected, "ops_key_present") {
		t.Fatal("a fast-join response must not carry an ops member; this vector now says it does")
	}

	// C-11: key 3 carries a REAL §6.1.2 body — a set of individually signed ops —
	// and not a state document. The two frozen spellings must be the same bytes.
	bodyHex := str(t, v.Input, "snapshot_body_cbor_hex")
	inlineBody := hexOf(t, v.Expected, "inline_body_cbor_hex")
	if !equalHex(inlineBody, mustHex(t, bodyHex)) {
		t.Fatalf("the inline body and the vector's own snapshot body are different bytes:\n"+
			"  inline %x\n  body   %s", inlineBody, bodyHex)
	}
	if !boolOf(v.Expected, "inline_body_is_an_op_set_not_a_state_document") ||
		!boolOf(v.Expected, "inline_body_is_cache_hint_verified_by_fold_then_recompute") {
		t.Fatal("this vector no longer treats the inline body as an op-set cache hint, which is " +
			"the whole of C-09/C-11")
	}
	inlineFastJoin, err := in.FastJoinEncode(
		kotvasync.FastJoin{Snapshot: decoded, Floor: fj.Floor, State: &bodyHex})
	if err != nil {
		t.Fatalf("FastJoinEncode (inline body): %v", err)
	}
	wantHex(t, "pull_response_with_inline_state_cbor_hex",
		wrapPullEnvelope(t, in, inlineFastJoin), v.Expected)

	// The state fetch address IS the root: the body is content-addressed by what
	// it folds to, not by who served it.
	addr, err := in.FastJoinStateAddress(byRef)
	if err != nil {
		t.Fatalf("FastJoinStateAddress: %v", err)
	}
	if want := strOf(t, v.Expected, "state_fetch_address_hex"); hex.EncodeToString(addr) != want {
		t.Fatalf("the state fetch address is %x, want %s", addr, want)
	}
	if want := str(t, v.Input, "snapshot_root_hex"); hex.EncodeToString(addr) != want {
		t.Fatalf("the state fetch address %x is not the snapshot root %s", addr, want)
	}

	// THE FOLD: the body's ops reproduce the committed state and hence the root.
	members, err := in.SnapshotBodyDecode(inlineBody)
	if err != nil {
		t.Fatalf("SnapshotBodyDecode: %v", err)
	}
	if int64(len(members)) != num(t, v.Input, "snapshot_body_op_count") {
		t.Fatalf("the body carries %d ops, the vector froze %d",
			len(members), num(t, v.Input, "snapshot_body_op_count"))
	}
	folded, err := in.SnapshotBodyFold(inlineBody, str(t, v.Input, "snapshot_ns"), receiverNowMS)
	if err != nil {
		t.Fatalf("SnapshotBodyFold: %v", err)
	}
	if want := hexIn(t, v.Input, "observable_state_cbor_hex"); !equalHex(folded, want) {
		t.Fatalf("folding the body produced\n  %x\nbut the snapshot commits to\n  %x", folded, want)
	}
	foldedRoot, err := in.ObservableStateRoot(folded)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	if want := strOf(t, v.Expected, "inline_body_folds_to_root_hex"); hex.EncodeToString(foldedRoot) != want {
		t.Fatalf("the body folds to %x, want %s", foldedRoot, want)
	}

	// Adoption end to end, through the caller-side sequence: the inline hint is
	// verified by the same fold, and the result is the committed state.
	adopted, err := in.FastJoinAdopt(inlineFastJoin, []kotvasync.Mark{}, nil, nil, receiverNowMS, nil)
	if err != nil {
		t.Fatalf("FastJoinAdopt: %v", err)
	}
	if !equalHex(adopted, folded) {
		t.Fatalf("adoption produced %x, the fold produced %x", adopted, folded)
	}
	adoptedRoot, err := in.ObservableStateRoot(adopted)
	if err != nil {
		t.Fatalf("ObservableStateRoot: %v", err)
	}
	if !equalHex(adoptedRoot, addr) {
		t.Fatalf("the adopted state addresses to %x, not the snapshot root %x", adoptedRoot, addr)
	}

	// A corrupted hint is DISCARDED in favour of a fetch, and with nothing
	// fetchable the same hint fails CLOSED rather than being trusted unverified.
	corrupt := append([]byte{}, inlineBody...)
	corrupt[len(corrupt)-1] ^= 0xff
	corruptHex := hex.EncodeToString(corrupt)
	hinted, err := in.FastJoinEncode(
		kotvasync.FastJoin{Snapshot: decoded, Floor: fj.Floor, State: &corruptHex})
	if err != nil {
		t.Fatalf("FastJoinEncode (corrupt hint): %v", err)
	}
	viaFetch, err := in.FastJoinAdopt(hinted, []kotvasync.Mark{}, nil, nil, receiverNowMS, inlineBody)
	if err != nil {
		t.Fatalf("a corrupted inline hint was not discarded in favour of the fetched body: %v", err)
	}
	if !equalHex(viaFetch, folded) {
		t.Fatalf("the fetch-fallback path produced %x, want the committed state %x", viaFetch, folded)
	}
	// ...and with nothing fetchable, the same unverifiable hint fails CLOSED: the
	// caller adopts NOTHING rather than trusting a body it could not verify. The
	// exact code for this path is frozen by SYNC-FJ-02, whose driver asserts its
	// code, name and action; it cannot be looked up by name here because the
	// engine's §12 registry entry point stops at 0x0A0B and does not list it.
	unverified, err := in.FastJoinAdopt(hinted, []kotvasync.Mark{}, nil, nil, receiverNowMS, nil)
	se, ok := kotvasync.AsSyncError(err)
	if !ok || !strings.HasPrefix(se.Code, "0x0A") {
		t.Fatalf("an unverifiable hint with nothing fetchable was adopted anyway (%v), want a "+
			"substrate refusal", err)
	}
	if se.Action != "FAIL_CLOSED_BLOCK" {
		t.Fatalf("the refusal for an unverifiable body carries action %q, want FAIL_CLOSED_BLOCK "+
			"— any other action invites the caller to retry or continue on state it never verified",
			se.Action)
	}
	if unverified != nil {
		t.Fatalf("a failed adoption still returned %d bytes of state — on failure the caller must "+
			"keep its old vector and adopt nothing", len(unverified))
	}

	// C-11's labelled non-conformant artifact: the pre-C-09 shape, where key 3
	// carried a state document. It must be reproducible so it can be RECOGNISED
	// as wrong, and it must be REJECTED when offered as a body.
	stateHex := str(t, v.Input, "observable_state_cbor_hex")
	asDocument, err := in.FastJoinEncode(
		kotvasync.FastJoin{Snapshot: decoded, Floor: fj.Floor, State: &stateHex})
	if err != nil {
		t.Fatalf("FastJoinEncode (state document): %v", err)
	}
	wantHex(t, "inline_state_document_would_be_nonconformant_cbor_hex",
		wrapPullEnvelope(t, in, asDocument), v.Expected)
	_, err = in.FastJoinAdopt(byRef, []kotvasync.Mark{}, nil, nil, receiverNowMS,
		hexIn(t, v.Input, "observable_state_cbor_hex"))
	if se, ok := kotvasync.AsSyncError(err); !ok || !strings.HasPrefix(se.Code, "0x0A") {
		t.Fatalf("a det_cbor(ObservableState) document was accepted as a snapshot body (%v) — "+
			"that is the C-09 defect, and adopting a projection diverges permanently", err)
	}

	// The responder predicate, both ways round: a caller already at `covers` is
	// not below the floor, and one holding nothing is.
	atCovers, err := in.CallerIsBelowFloor(snapshot, covers)
	if err != nil {
		t.Fatalf("CallerIsBelowFloor: %v", err)
	}
	if atCovers {
		t.Fatal("a caller already holding everything in `covers` was called below the floor — it " +
			"would be fast-joined instead of being sent the ops it can still use")
	}
	empty, err := in.CallerIsBelowFloor(snapshot, []kotvasync.Mark{})
	if err != nil {
		t.Fatalf("CallerIsBelowFloor (empty vector): %v", err)
	}
	if !empty {
		t.Fatal("a caller holding nothing was NOT called below the floor, so the predicate above " +
			"is vacuous")
	}

	// `scenario`, `state_fetch_endpoint`, `*_role` and `inline_state_document_note`
	// are prose about intent; not asserted.
}

// SYNC-FJ-02: the §5.2.1 MUST in both directions, C-06's op framing, C-07's
// rejected predicate, the progress MUST, and the caller-side fail-closed paths.
func driveFastJoinFloorPredicate(t *testing.T, in *kotvasync.Instance, v vector) {
	// The responder's own answer to the caller who IS below the floor, taken out
	// of the frozen pull envelope.
	fjBytes := unwrapPullEnvelope(t, in, hexOf(t, v.Expected, "caller_behind_response_cbor_hex"))
	fj, err := in.FastJoinDecode(fjBytes)
	if err != nil {
		t.Fatalf("FastJoinDecode: %v", err)
	}
	// The snapshot inside it, re-assembled from its own decoded fields so the
	// predicate calls below run against bytes that verify.
	unsigned := fj.Snapshot
	unsigned.Sig = ""
	snapshot, err := in.SnapshotAssemble(unsigned, mustHex(t, fj.Snapshot.Sig))
	if err != nil {
		t.Fatalf("SnapshotAssemble: %v", err)
	}
	if err := in.SnapshotVerify(snapshot); err != nil {
		t.Fatalf("the fast-join's own snapshot does not verify: %v", err)
	}

	// The floor and covers the vector froze must be the ones the fast-join
	// carries, or every predicate below is measured on different data.
	floorBytes, err := in.EncodeHLC(fj.Floor)
	if err != nil {
		t.Fatalf("EncodeHLC: %v", err)
	}
	if want := str(t, v.Input, "responder_floor_hlc_cbor_hex"); hex.EncodeToString(floorBytes) != want {
		t.Fatalf("the fast-join's floor encodes to %x, the vector froze %s", floorBytes, want)
	}
	if same, err := in.CompareHLC(fj.Floor, hlcOf(t, v.Input, "responder_floor_hlc")); err != nil || same != 0 {
		t.Fatalf("the fast-join's floor is not the vector's responder_floor_hlc: %v", err)
	}
	wantCovers := coversMarks(t, in, str(t, v.Input, "responder_snapshot_covers_cbor_hex"))
	if !sameMarks(t, in, fj.Snapshot.Covers, wantCovers) {
		t.Fatalf("the snapshot covers %+v, the vector froze %+v", fj.Snapshot.Covers, wantCovers)
	}

	behind := coversMarks(t, in, str(t, v.Input, "caller_behind_vector_cbor_hex"))
	caughtUp := coversMarks(t, in, str(t, v.Input, "caller_caught_up_vector_cbor_hex"))
	for _, c := range []struct {
		name    string
		vector  []kotvasync.Mark
		wantKey string
	}{
		{"the caller behind the floor", behind, "caller_behind_is_below_floor"},
		{"the caught-up caller", caughtUp, "caller_caught_up_is_below_floor"},
	} {
		got, err := in.CallerIsBelowFloor(snapshot, c.vector)
		if err != nil {
			t.Fatalf("CallerIsBelowFloor (%s): %v", c.name, err)
		}
		if want := boolOf(v.Expected, c.wantKey); got != want {
			t.Fatalf("%s is reported below-floor=%v, want %v — a responder MUST fast-join the "+
				"first and MUST answer the second with ops", c.name, got, want)
		}
	}

	// The surviving suffix: each member is a COSE_Sign1 embedded as a CBOR item,
	// and verifying one yields exactly the inner SyncOp the vector froze.
	envelopes := hexList(t, v.Input, "surviving_suffix_ops_cbor_hex")
	inner := hexList(t, v.Input, "surviving_suffix_inner_syncop_cbor_hex")
	if len(envelopes) != len(inner) || len(envelopes) == 0 {
		t.Fatalf("the vector froze %d envelopes and %d inner ops", len(envelopes), len(inner))
	}
	for i := range envelopes {
		payload, err := in.VerifySignedOp(envelopes[i])
		if err != nil {
			t.Fatalf("suffix op %d does not verify: %v", i, err)
		}
		if !equalHex(payload, inner[i]) {
			t.Fatalf("suffix op %d carries %x, want %x", i, payload, inner[i])
		}
	}

	// The forbidden answer is WELL-FORMED — that is exactly why the MUST is
	// needed — and the same bytes are the CORRECT answer for a caught-up caller.
	if !boolOf(v.Expected, "caller_behind_ops_response_forbidden") ||
		!boolOf(v.Expected, "caller_caught_up_fastjoin_forbidden") {
		t.Fatal("this vector no longer forbids the two crossed answers, which is the whole of " +
			"the §5.2.1 MUST")
	}
	opsResponse := opsEnvelope(t, in, envelopes, false)
	wantHex(t, "caller_behind_ops_response_would_be_cbor_hex", opsResponse, v.Expected)
	wantHex(t, "caller_caught_up_response_cbor_hex", opsResponse, v.Expected)

	// C-06: op framing is item-embedded. The bstr-wrapped spelling of the same
	// ops must be reproducible — so it can be rejected rather than guessed at —
	// recognisably different, and refused on decode with the frozen code. The
	// conformant framing is driven first, so a decoder that refuses everything
	// cannot pass this.
	if boolOf(v.Expected, "ops_member_bstr_wrapped_conformant") {
		t.Fatal("this vector now calls bstr-wrapped members conformant; re-read §5.2 C-06")
	}
	if want := strOf(t, v.Expected, "ops_member_framing"); want != "item-embedded COSE_Sign1" {
		t.Fatalf("the frozen op framing is now %q", want)
	}
	wrapped := opsEnvelope(t, in, envelopes, true)
	wantHex(t, "ops_member_bstr_wrapped_NONCONFORMANT_cbor_hex", wrapped, v.Expected)
	if equalHex(wrapped, opsResponse) {
		t.Fatal("the two framings encode identically, which would make the C-06 rule " +
			"unenforceable")
	}
	var membersHex []string
	for _, e := range envelopes {
		membersHex = append(membersHex, hex.EncodeToString(e))
	}
	conformantBody, err := in.SnapshotBodyEncode(membersHex)
	if err != nil {
		t.Fatalf("SnapshotBodyEncode: %v", err)
	}
	if got, err := in.SnapshotBodyDecode(conformantBody); err != nil || !reflect.DeepEqual(got, membersHex) {
		t.Fatalf("an item-framed body did not survive a round trip (%v): %v", err, got)
	}
	// A wrapped member must be REFUSED wherever it turns up, never silently
	// unwrapped, so both spellings are offered to the op-set decoder — the only
	// exported surface that reads §5.2 op framing.
	//
	// The frozen non-conformant response is refused with exactly the frozen code.
	// A bare ARRAY of wrapped members surfaces 0x0A02 (ERR_SYNC_OP_SIG_INVALID)
	// instead of the vector's 0x0A03, because on this path the decoder verifies
	// each member's COSE signature before it reaches structural validation, and a
	// byte string is not an envelope whose signature can verify. Both refusals
	// carry FAIL_CLOSED_BLOCK, which is asserted below, so the guarantee the
	// vector protects — the framing is rejected rather than unwrapped, and nothing
	// continues on it — holds either way.
	code := strOf(t, v.Expected, "ops_member_bstr_wrapped_error_code")
	if _, err := in.SnapshotBodyDecode(wrapped); !kotvasync.IsRefusal(err, code) {
		t.Fatalf("the frozen bstr-wrapped framing decoded as %v, want a refusal with %s", err, code)
	}
	unwrappedAnyway, err := in.SnapshotBodyDecode(bstrArray(t, in, envelopes))
	if err == nil {
		t.Fatalf("an array of bstr-wrapped members decoded into %v — a wrapped member must be "+
			"refused, not unwrapped", unwrappedAnyway)
	}
	se, ok := kotvasync.AsSyncError(err)
	if !ok {
		t.Fatalf("an array of bstr-wrapped members was refused with %v, want a substrate refusal",
			err)
	}
	if se.Action != "FAIL_CLOSED_BLOCK" {
		t.Fatalf("a bstr-wrapped member was refused with %s/%s, whose action is %q rather than "+
			"FAIL_CLOSED_BLOCK — the framing must not merely be reported, it must stop the round",
			se.Code, se.Name, se.Action)
	}

	// C-07: floor and covers are not comparable. The rejected naive predicate
	// still FIRES on this well-formed fast-join, and step 2 must accept it anyway.
	// A true above with a rejection below is precisely the defect C-07 removed.
	if boolOf(v.Expected, "floor_vs_covers_is_orderable") {
		t.Fatal("floor and covers are not orderable; this vector now says they are")
	}
	if want := "covers.lacks(floor)"; strOf(t, v.Expected, "floor_vs_covers_naive_predicate_rejected") != want {
		t.Fatalf("the rejected predicate is now spelled %q",
			strOf(t, v.Expected, "floor_vs_covers_naive_predicate_rejected"))
	}
	naive, err := in.FastJoinNaiveCoversLacksFloorRejected(fjBytes)
	if err != nil {
		t.Fatalf("FastJoinNaiveCoversLacksFloorRejected: %v", err)
	}
	if want := boolOf(v.Expected, "floor_vs_covers_naive_predicate_value_here"); naive != want {
		t.Fatalf("the rejected naive predicate evaluates %v here, want %v — the counterexample "+
			"has stopped firing, so nothing below guards against reinstating it", naive, want)
	}
	if err := in.FastJoinCheckCovers(fjBytes, behind); err != nil {
		t.Fatalf("step 2 REJECTED a conformant fast-join whose floor sits above covers[A]: %v — "+
			"that is the C-07 defect, and it rejects conformant peers", err)
	}
	mark, err := in.FastJoinCoversCarriesFloorAuthorMark(fjBytes)
	if err != nil {
		t.Fatalf("FastJoinCoversCarriesFloorAuthorMark: %v", err)
	}
	if want := boolOf(v.Expected, "covers_carries_mark_for_floor_author"); mark != want {
		t.Fatalf("the advisory floor-author mark is %v, want %v", mark, want)
	}
	if boolOf(v.Expected, "covers_mark_for_floor_author_is_MUST") {
		t.Fatal("the advisory mark must never be a MUST — treating its absence as a failure " +
			"rejects conformant peers")
	}

	// The step-5 progress MUST: the same root AND the same covers twice means the
	// responder is looping, and nothing else in the protocol terminates it.
	if err := in.FastJoinCheckProgress(fjBytes, nil, nil); err != nil {
		t.Fatalf("the first round of a join was reported as making no progress: %v", err)
	}
	prevRoot, err := in.FastJoinStateAddress(fjBytes)
	if err != nil {
		t.Fatalf("FastJoinStateAddress: %v", err)
	}
	loopCode := strOf(t, v.Expected, "repeated_fastjoin_same_root_and_covers_error_code")
	err = in.FastJoinCheckProgress(fjBytes, prevRoot, fj.Snapshot.Covers)
	if !kotvasync.IsRefusal(err, loopCode) {
		t.Fatalf("the same checkpoint offered twice was accepted as progress (%v), want a "+
			"refusal with %s — adopting it again cannot advance the caller", err, loopCode)
	}

	// Caller-side fail-closed: a body no holder can serve is a refusal with all
	// three frozen registry fields, and never a fallback to the suffix.
	if !boolOf(v.Expected, "suffix_fallback_after_failed_fastjoin_forbidden") {
		t.Fatal("this vector no longer forbids a suffix fallback after a failed fast-join, which " +
			"is the silent lost-write the whole path exists to prevent")
	}
	_, err = in.FastJoinAdopt(fjBytes, behind, nil, nil, receiverNowMS, nil)
	se, ok = kotvasync.AsSyncError(err)
	if !ok {
		t.Fatalf("adopting with no fetchable body returned %v, want a substrate refusal", err)
	}
	for label, want := range map[string]string{
		"code":   strOf(t, v.Expected, "state_body_unfetchable_error_code"),
		"name":   strOf(t, v.Expected, "state_body_unfetchable_error_name"),
		"action": strOf(t, v.Expected, "state_body_unfetchable_action"),
	} {
		got := map[string]string{"code": se.Code, "name": se.Name, "action": se.Action}[label]
		if got != want {
			t.Fatalf("the unfetchable-body refusal carries %s %q, want %q", label, got, want)
		}
	}

	// ...and the other direction is refused from the caller's side too: a
	// caught-up caller must not adopt a checkpoint at all. The vector freezes the
	// prohibition but no code for it, so only the refusal is asserted.
	_, err = in.FastJoinAdopt(fjBytes, caughtUp, nil, nil, receiverNowMS, nil)
	if se, ok := kotvasync.AsSyncError(err); !ok || !strings.HasPrefix(se.Code, "0x0A") {
		t.Fatalf("a caught-up caller was allowed to fast-join (%v) — it would regress to the "+
			"checkpoint and lose everything it holds past `covers`", err)
	}

	// `predicate`, the `*_note` fields, and the declarative claims about covers
	// regression (adopting_covers_may_regress_caller_vector and its companion)
	// are prose about consequences this driver does not stage; not asserted.
}

// SYNC-RECON-01: the range-Merkle fold, and a diff whose cost tracks the size of
// the difference rather than the size of the history.
func driveReconFingerprint(t *testing.T, in *kotvasync.Instance, v vector) {
	var opsByLabel, idsByLabel map[string]string
	if err := json.Unmarshal(v.Input["ops_cbor_hex"], &opsByLabel); err != nil {
		t.Fatalf("input/ops_cbor_hex: %v", err)
	}
	if err := json.Unmarshal(v.Input["op_ids_hex"], &idsByLabel); err != nil {
		t.Fatalf("input/op_ids_hex: %v", err)
	}

	entry := func(label string) kotvasync.OpEntry {
		raw, ok := opsByLabel[label]
		if !ok {
			t.Fatalf("the vector names op %q but does not freeze its bytes", label)
		}
		// The frozen id must be the one the engine computes, or the fold below is
		// over labels rather than over ops.
		if got := opID(t, in, mustHex(t, raw)); got != idsByLabel[label] {
			t.Fatalf("op %s addresses to %s, frozen as %s", label, got, idsByLabel[label])
		}
		return kotvasync.OpEntry{HLC: decodeOp(t, in, mustHex(t, raw)).HLC, ID: idsByLabel[label]}
	}
	holds := func(key string) []kotvasync.OpEntry {
		var labels []string
		if err := json.Unmarshal(v.Input[key], &labels); err != nil {
			t.Fatalf("input/%s: %v", key, err)
		}
		out := make([]kotvasync.OpEntry, 0, len(labels))
		for _, l := range labels {
			out = append(out, entry(l))
		}
		return out
	}
	a, b := holds("replica_A_holds"), holds("replica_B_holds")

	rng := mustObject(t, v.Input, "range")
	lo, hi := hlcOf(t, rng, "lo"), hlcOf(t, rng, "hi")
	split := hlcOf(t, v.Input, "split_at")

	type sideExp struct {
		FPHex string `json:"fp_hex"`
		Count uint64 `json:"count"`
	}
	type rangeExp struct {
		A     sideExp `json:"A"`
		B     sideExp `json:"B"`
		Match bool    `json:"match"`
	}

	// The full range, through Fingerprint (no bounds) and Summarize (the whole
	// range as bounds) — the two must agree, or a range query is not the same fold
	// as the set it covers.
	var full rangeExp
	if err := json.Unmarshal(v.Expected["full_range"], &full); err != nil {
		t.Fatalf("expected/full_range: %v", err)
	}
	for _, side := range []struct {
		name    string
		entries []kotvasync.OpEntry
		want    sideExp
	}{{"A", a, full.A}, {"B", b, full.B}} {
		fp, err := in.Fingerprint(side.entries)
		if err != nil {
			t.Fatalf("Fingerprint(%s): %v", side.name, err)
		}
		if fp.FP != side.want.FPHex || fp.Count != side.want.Count {
			t.Fatalf("replica %s fingerprints to %s/%d, want %s/%d",
				side.name, fp.FP, fp.Count, side.want.FPHex, side.want.Count)
		}
		s, err := in.Summarize(side.entries, lo, hi)
		if err != nil {
			t.Fatalf("Summarize(%s, full range): %v", side.name, err)
		}
		if s.FP != fp.FP || s.Count != fp.Count {
			t.Fatalf("replica %s summarises the full range as %s/%d but fingerprints as %s/%d",
				side.name, s.FP, s.Count, fp.FP, fp.Count)
		}
	}
	if (full.A.FPHex == full.B.FPHex) != full.Match {
		t.Fatalf("the full range's match verdict (%v) disagrees with its own fingerprints", full.Match)
	}

	// The split, and the economy the protocol exists for: the matching subrange
	// exchanges NOTHING, the mismatching one surfaces exactly the differing op.
	for key, bounds := range map[string][2]kotvasync.HLC{
		"subrange_1": {lo, split},
		"subrange_2": {split, hi},
	} {
		var want rangeExp
		if err := json.Unmarshal(v.Expected[key], &want); err != nil {
			t.Fatalf("expected/%s: %v", key, err)
		}
		fields := mustObject(t, v.Expected, key)
		for _, side := range []struct {
			name    string
			entries []kotvasync.OpEntry
			want    sideExp
		}{{"A", a, want.A}, {"B", b, want.B}} {
			s, err := in.Summarize(side.entries, bounds[0], bounds[1])
			if err != nil {
				t.Fatalf("Summarize(%s, %s): %v", key, side.name, err)
			}
			if s.FP != side.want.FPHex || s.Count != side.want.Count {
				t.Fatalf("%s replica %s is %s/%d, want %s/%d",
					key, side.name, s.FP, s.Count, side.want.FPHex, side.want.Count)
			}
		}
		matched := want.A.FPHex == want.B.FPHex && want.A.Count == want.B.Count
		if matched != want.Match {
			t.Fatalf("%s: the match verdict (%v) disagrees with its own fingerprints", key, want.Match)
		}
		rec, err := in.Reconcile(a, b, bounds[0], bounds[1])
		if err != nil {
			t.Fatalf("Reconcile(%s): %v", key, err)
		}
		if matched {
			if len(rec.MissingHere)+len(rec.MissingThere) != 0 {
				t.Fatalf("%s matches on both sides but reconciliation still exchanged %v/%v — a "+
					"matching range must prune whole", key, rec.MissingHere, rec.MissingThere)
			}
			if raw, ok := fields["ops_exchanged"]; ok {
				var exchanged []string
				if err := json.Unmarshal(raw, &exchanged); err != nil {
					t.Fatalf("expected/%s/ops_exchanged: %v", key, err)
				}
				if len(exchanged) != 0 {
					t.Fatalf("%s matches on both sides, but the vector expects %v exchanged in it",
						key, exchanged)
				}
			}
			continue
		}
		var shipped []string
		if err := json.Unmarshal(fields["ops_shipped_to_B"], &shipped); err != nil {
			t.Fatalf("expected/%s/ops_shipped_to_B: %v", key, err)
		}
		if !reflect.DeepEqual(rec.MissingThere, shipped) {
			t.Fatalf("%s: reconciliation ships %v to B, want %v", key, rec.MissingThere, shipped)
		}
		if len(rec.MissingHere) != 0 {
			t.Fatalf("%s: reconciliation would ship %v to A, but A holds a superset",
				key, rec.MissingHere)
		}
	}

	// And over the whole range, the total shipped is the frozen one.
	rec, err := in.Reconcile(a, b, lo, hi)
	if err != nil {
		t.Fatalf("Reconcile(full range): %v", err)
	}
	if int64(len(rec.MissingThere)) != num(t, v.Expected, "ops_shipped_total") {
		t.Fatalf("reconciling the full range ships %d ops, want %d",
			len(rec.MissingThere), num(t, v.Expected, "ops_shipped_total"))
	}
	if rec.RangesCompared < 2 {
		t.Fatalf("reconciliation compared %d ranges — a mismatching full range must be split, "+
			"or the diff is a full transfer by another name", rec.RangesCompared)
	}

	// An empty range and a range whose ops happen to fold to the same value are
	// distinguishable only because the count travels with the hash.
	empty, err := in.Fingerprint(nil)
	if err != nil {
		t.Fatalf("Fingerprint(empty): %v", err)
	}
	if want := strOf(t, v.Expected, "empty_range_fp_hex"); empty.FP != want {
		t.Fatalf("the empty range fingerprints to %s, want %s", empty.FP, want)
	}
	if int64(empty.Count) != num(t, v.Expected, "empty_range_count") {
		t.Fatalf("the empty range counts %d, want %d",
			empty.Count, num(t, v.Expected, "empty_range_count"))
	}
}

// SYNC-NS-01: a responder ships only the namespaces the caller subscribed to. An
// op outside the subscription must not leave the responder at all.
func driveNsSparseFilter(t *testing.T, in *kotvasync.Instance, v vector) {
	ops := hexList(t, v.Input, "responder_ops_cbor_hex")
	var declaredNS []string
	if err := json.Unmarshal(v.Input["responder_ops_ns"], &declaredNS); err != nil {
		t.Fatalf("input/responder_ops_ns: %v", err)
	}
	if len(declaredNS) != len(ops) {
		t.Fatalf("the vector froze %d ops and %d namespaces for them", len(ops), len(declaredNS))
	}
	var docs []string
	for i, op := range ops {
		if got := decodeOp(t, in, op).NS; got != declaredNS[i] {
			t.Fatalf("responder op %d is in namespace %q, the vector says %q", i, got, declaredNS[i])
		}
		doc, err := in.DecodeOpJSON(op)
		if err != nil {
			t.Fatalf("DecodeOpJSON: %v", err)
		}
		docs = append(docs, doc)
	}

	var subscribed []string
	if err := json.Unmarshal(v.Input["caller_subscribed_ns"], &subscribed); err != nil {
		t.Fatalf("input/caller_subscribed_ns: %v", err)
	}
	if len(subscribed) >= len(declaredNS) {
		t.Fatal("the caller subscribes to everything the responder holds, so filtering nothing " +
			"out would pass")
	}
	shipped, err := in.ScopeToSubscription("["+strings.Join(docs, ",")+"]", subscribed)
	if err != nil {
		t.Fatalf("ScopeToSubscription: %v", err)
	}

	var wantOps, wantNS []string
	if err := json.Unmarshal(v.Expected["shipped_ops_cbor_hex"], &wantOps); err != nil {
		t.Fatalf("expected/shipped_ops_cbor_hex: %v", err)
	}
	if err := json.Unmarshal(v.Expected["shipped_ns"], &wantNS); err != nil {
		t.Fatalf("expected/shipped_ns: %v", err)
	}
	var gotOps, gotNS []string
	for _, b := range shipped {
		gotOps = append(gotOps, hex.EncodeToString(b))
		gotNS = append(gotNS, decodeOp(t, in, b).NS)
	}
	if !reflect.DeepEqual(gotOps, wantOps) {
		t.Fatalf("sparse scoping shipped %v, want %v — an op outside the caller's subscription "+
			"must not leave the responder", gotOps, wantOps)
	}
	if !reflect.DeepEqual(gotNS, wantNS) {
		t.Fatalf("the shipped ops are in namespaces %v, want %v", gotNS, wantNS)
	}
}

// SYNC-NS-02: a cross-namespace reference is a leak, not a convenience.
func driveNsLeakCheck(t *testing.T, in *kotvasync.Instance, v vector) {
	if got := strOf(t, v.Expected, "outcome"); got != "reject" {
		t.Fatalf("this vector's outcome is now %q; the driver below asserts a rejection", got)
	}
	op := decodeOp(t, in, hexIn(t, v.Input, "op_cbor_hex"))
	ns := str(t, v.Input, "op_ns")
	if op.NS != ns {
		t.Fatalf("the frozen op is in namespace %q, the vector says %q", op.NS, ns)
	}
	if op.Reference == nil {
		t.Fatal("the frozen op references nothing, so there is no leak to refuse")
	}
	if want := str(t, v.Input, "ref_target"); op.Reference.Target != want {
		t.Fatalf("the op references %q, the vector says %q", op.Reference.Target, want)
	}
	target := str(t, v.Input, "ref_target_actual_ns")
	if target == ns {
		t.Fatal("the referenced target is in the op's own namespace, so nothing crosses one")
	}
	wantRefusal(t, in.CheckNsRef(ns, target), v.Expected, "CheckNsRef")

	// A same-namespace reference is fine, so the check is a boundary rather than a
	// refusal to reference anything.
	if err := in.CheckNsRef(ns, ns); err != nil {
		t.Fatalf("CheckNsRef refused a same-namespace reference: %v", err)
	}
}

// SYNC-GC-01: the stability cut, why an unknown watermark yields no cut at all,
// and that collecting below the cut is observably a no-op.
func driveStabilityCut(t *testing.T, in *kotvasync.Instance, v vector) {
	var live []struct {
		Replica string  `json:"replica"`
		HLC     hlcJSON `json:"max_applied_hlc"`
	}
	if err := json.Unmarshal(v.Input["live_replica_watermarks"], &live); err != nil {
		t.Fatalf("input/live_replica_watermarks: %v", err)
	}
	if len(live) < 2 {
		t.Fatalf("the vector froze %d live watermarks; a minimum over one replica is not a cut",
			len(live))
	}
	marks := make([]*kotvasync.HLC, 0, len(live))
	for _, w := range live {
		h := w.HLC.hlc()
		marks = append(marks, &h)
	}

	cut, err := in.StabilityCut(marks)
	if err != nil {
		t.Fatalf("StabilityCut: %v", err)
	}
	if cut == nil {
		t.Fatalf("StabilityCut returned no cut for %d known watermarks", len(marks))
	}
	if int64(cut.Counter) != num(t, v.Expected, "stability_cut_counter") {
		t.Fatalf("the stability cut sits at counter %d, want %d",
			cut.Counter, num(t, v.Expected, "stability_cut_counter"))
	}
	// The cut is the MINIMUM: no live replica may be behind it, or history would
	// be truncated out from under a replica that has not caught up.
	for i, mark := range marks {
		if cmp, err := in.CompareHLC(*cut, *mark); err != nil || cmp > 0 {
			t.Fatalf("the cut %+v is above live replica %d's watermark %+v: %v",
				*cut, i, *mark, err)
		}
	}

	// Including a stale replica drags the cut down forever — which is why
	// excluding one is a liveness decision the caller makes, not the engine.
	if !boolOf(v.Expected, "stale_replica_excluded") {
		t.Fatal("this vector no longer excludes its stale replica, so the cut below is not the " +
			"one it describes")
	}
	stale := hlcOf(t, mustObject(t, v.Input, "stale_replica_watermark"), "max_applied_hlc")
	if cmp, err := in.CompareHLC(stale, *cut); err != nil || cmp >= 0 {
		t.Fatal("the vector's premise is broken: its stale replica's watermark must sit BELOW " +
			"the cut computed without it")
	}
	withStale, err := in.StabilityCut(append(append([]*kotvasync.HLC{}, marks...), &stale))
	if err != nil {
		t.Fatalf("StabilityCut (including the stale replica): %v", err)
	}
	if withStale == nil {
		t.Fatal("StabilityCut returned no cut when every watermark was known")
	}
	if cmp, err := in.CompareHLC(*withStale, *cut); err != nil || cmp >= 0 {
		t.Fatalf("including the stale replica did not drag the cut down (%+v vs %+v): %v",
			*withStale, *cut, err)
	}

	// Fail-closed: a live replica whose watermark is UNKNOWN yields no cut at all,
	// because "unknown" must never be read as "caught up" — GC on incomplete
	// knowledge is unrecoverable.
	none, err := in.StabilityCut(append(append([]*kotvasync.HLC{}, marks...), nil))
	if err != nil {
		t.Fatalf("StabilityCut (with an unknown watermark): %v", err)
	}
	if none != nil {
		t.Fatalf("a live replica with no known watermark still produced a cut at %+v", *none)
	}

	// And what the cut is FOR: a collapsed add/tombstone pair strictly below it is
	// reclaimable, and reclaiming it does not move observable state.
	kinds := opKinds(t, in)
	addHLC := kotvasync.HLC{Wall: cut.Wall, Counter: 1, Author: cut.Author}
	removeHLC := kotvasync.HLC{Wall: cut.Wall, Counter: 2, Author: cut.Author}
	for _, h := range []kotvasync.HLC{addHLC, removeHLC} {
		if cmp, err := in.CompareHLC(h, *cut); err != nil || cmp >= 0 {
			t.Fatalf("the ops built for the prune below are not below the cut %+v: %v", *cut, err)
		}
	}
	add, err := in.EncodeOp(kotvasync.Op{
		Kind: kinds.SetAdd, Target: "tags", Value: kotvasync.Text("e1"), HLC: addHLC})
	if err != nil {
		t.Fatalf("encoding the add: %v", err)
	}
	remove, err := in.EncodeOp(kotvasync.Op{
		Kind: kinds.SetRemove, Target: "tags", Value: kotvasync.Text("e1"), HLC: removeHLC,
		Observed: []kotvasync.AddTag{{Author: cut.Author, HLC: addHLC}}})
	if err != nil {
		t.Fatalf("encoding the remove: %v", err)
	}
	eng := ingestAll(t, in, [][]byte{add, remove})
	defer eng.Close()
	before, err := eng.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState: %v", err)
	}
	pruned, err := eng.PruneBelow(*cut)
	if err != nil {
		t.Fatalf("PruneBelow: %v", err)
	}
	if pruned == 0 {
		t.Fatal("a collapsed add/tombstone pair entirely below the cut reclaimed nothing, so the " +
			"cut buys no space")
	}
	after, err := eng.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState after the prune: %v", err)
	}
	if !equalHex(before, after) {
		t.Fatalf("collecting below the stability cut changed observable state:\n  before %x\n"+
			"  after  %x", before, after)
	}

	// `note_no_watermark_case` states in prose what the unknown-watermark check
	// above measures; not asserted.
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// newInstance brings up one bare runtime and instance for a test, closing the
// instance before the runtime so the module is not torn down under a live caller.
func newInstance(t *testing.T) *kotvasync.Instance {
	t.Helper()
	ctx := t.Context()
	rt, err := kotvasync.New(ctx)
	if err != nil {
		t.Fatalf("kotvasync.New: %v", err)
	}
	in, err := rt.Instance(ctx)
	if err != nil {
		t.Fatalf("Instance: %v", err)
	}
	t.Cleanup(func() {
		_ = in.Close(ctx)
		_ = rt.Close(ctx)
	})
	return in
}

// opKinds reads the §4.2 kind numbers out of the engine. Nothing in this file
// writes a kind number down: a renumbering must surface as a mismatch against the
// vectors rather than as a driver that silently encodes a different op.
func opKinds(t *testing.T, in *kotvasync.Instance) kotvasync.OpKinds {
	t.Helper()
	k, err := in.OpKinds()
	if err != nil {
		t.Fatalf("OpKinds: %v", err)
	}
	return k
}

// ingestAll folds every op into a fresh replica through the §5.6 ambient path.
// The vectors' ops are bare SyncOps rather than envelopes — the COSE framing is
// exercised on its own by SYNC-OP-02 — so this is the correct entry point and not
// a weakening: every §4 validator still runs.
func ingestAll(t *testing.T, in *kotvasync.Instance, ops [][]byte) *kotvasync.Engine {
	t.Helper()
	eng, err := in.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	for i, op := range ops {
		if _, err := eng.IngestAmbientAuthenticated(op, receiverNowMS); err != nil {
			eng.Close()
			t.Fatalf("ingesting op %d: %v", i, err)
		}
	}
	return eng
}

// assertSameState requires two replicas to hold byte-identical observable state.
// Equal bytes IS the definition of equal state (§6.1.1), which is a much stronger
// check than comparing whichever projection a vector happens to name.
func assertSameState(t *testing.T, a, b *kotvasync.Engine, what string) {
	t.Helper()
	left, err := a.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState: %v", err)
	}
	right, err := b.ObservableState()
	if err != nil {
		t.Fatalf("ObservableState: %v", err)
	}
	if !equalHex(left, right) {
		t.Fatalf("%s produced different observable state:\n  %x\n  %x", what, left, right)
	}
}

func reversedOps(ops [][]byte) [][]byte {
	out := make([][]byte, 0, len(ops))
	for i := len(ops) - 1; i >= 0; i-- {
		out = append(out, ops[i])
	}
	return out
}

func reverseRows(rows [][]json.RawMessage) [][]json.RawMessage {
	out := make([][]json.RawMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, rows[i])
	}
	return out
}

func tail(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[len(s)-n:]
}

func decodeOp(t *testing.T, in *kotvasync.Instance, raw []byte) kotvasync.Op {
	t.Helper()
	op, err := in.DecodeOp(raw)
	if err != nil {
		t.Fatalf("DecodeOp: %v", err)
	}
	return op
}

func opID(t *testing.T, in *kotvasync.Instance, raw []byte) string {
	t.Helper()
	id, err := in.OpID(raw)
	if err != nil {
		t.Fatalf("OpID: %v", err)
	}
	return hex.EncodeToString(id)
}

func hlcHex(t *testing.T, in *kotvasync.Instance, h kotvasync.HLC) string {
	t.Helper()
	b, err := in.EncodeHLC(h)
	if err != nil {
		t.Fatalf("EncodeHLC: %v", err)
	}
	return hex.EncodeToString(b)
}

// tstr reads the text out of a tagged value, failing if it is not one: JSON
// cannot tell a text string from a hex-spelled byte string, so the tag is the
// only thing that says which this is.
func tstr(t *testing.T, tagged json.RawMessage) string {
	t.Helper()
	var v struct {
		Tstr *string `json:"tstr"`
	}
	if err := json.Unmarshal(tagged, &v); err != nil || v.Tstr == nil {
		t.Fatalf("value %s is not a tagged text", tagged)
	}
	return *v.Tstr
}

// tstrOrEmpty is tstr for places where a value may legitimately be absent — a
// tombstoned RGA atom carries none.
func tstrOrEmpty(tagged json.RawMessage) string {
	if len(tagged) == 0 || string(tagged) == "null" {
		return ""
	}
	var v struct {
		Tstr *string `json:"tstr"`
	}
	if err := json.Unmarshal(tagged, &v); err != nil || v.Tstr == nil {
		return ""
	}
	return *v.Tstr
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func equalHex(a, b []byte) bool { return hex.EncodeToString(a) == hex.EncodeToString(b) }

func sortEdges(e [][]string) {
	sort.Slice(e, func(i, j int) bool {
		return strings.Join(e[i], "\x00") < strings.Join(e[j], "\x00")
	})
}

// labelled renders a list of tree moves in the vector's own h1/h2 labels, so a
// failure names the move the fixture names instead of an HLC nobody can read.
// Moves the vector does not label (the baseline ones) are left out.
func labelled(t *testing.T, in *kotvasync.Instance, moves []kotvasync.TreeMove, label map[string]string) []string {
	t.Helper()
	var out []string
	for _, mv := range moves {
		if l, ok := label[hlcHex(t, in, mv.HLC)]; ok {
			out = append(out, l)
		}
	}
	return out
}

// assertAcyclic walks every node to its root, so "acyclic" is measured rather
// than taken from the engine's own word for it.
func assertAcyclic(t *testing.T, edges [][]string, order int) {
	t.Helper()
	parent := map[string]string{}
	for _, e := range edges {
		if len(e) < 2 {
			t.Fatalf("arrival order %d produced the malformed edge %v", order, e)
		}
		parent[e[0]] = e[1]
	}
	for node := range parent {
		cur, steps := node, 0
		for {
			next, ok := parent[cur]
			if !ok {
				break
			}
			cur = next
			steps++
			if steps > len(parent) {
				t.Fatalf("arrival order %d left a cycle reachable from %q — a move that closes "+
					"one must be skipped, not applied", order, node)
			}
		}
	}
}

// applyLWW writes one lww-set op into a decoded observable-state projection: the
// suffix a replica applies by hand after adopting a checkpoint.
func applyLWW(t *testing.T, rows [][]json.RawMessage, op kotvasync.Op) [][]json.RawMessage {
	t.Helper()
	out := append([][]json.RawMessage{}, rows...)
	for i, row := range out {
		if len(row) != 3 {
			t.Fatalf("an LWW row has %d members, want [target, field, value]", len(row))
		}
		var target, field string
		if err := json.Unmarshal(row[0], &target); err != nil {
			t.Fatalf("an LWW row's target is not a string: %v", err)
		}
		if err := json.Unmarshal(row[1], &field); err != nil {
			t.Fatalf("an LWW row's field is not a string: %v", err)
		}
		if target == op.Target && field == deref(op.Field) {
			replaced := append([]json.RawMessage{}, row...)
			replaced[2] = op.Value
			out[i] = replaced
			return out
		}
	}
	target, err := json.Marshal(op.Target)
	if err != nil {
		t.Fatalf("marshalling the op's target: %v", err)
	}
	field, err := json.Marshal(deref(op.Field))
	if err != nil {
		t.Fatalf("marshalling the op's field: %v", err)
	}
	return append(out, []json.RawMessage{target, field, op.Value})
}

// observableStateOf builds the canonical six-section projection from a vector's
// PROSE description of it — bare JSON scalars retagged into the substrate's
// tagged-value spelling, because the bytes are the semantics (§2.2) and an
// untagged value is refused rather than guessed at.
func observableStateOf(t *testing.T, m map[string]json.RawMessage, key string) kotvasync.ObservableState {
	t.Helper()
	var described struct {
		ORSet [][]any `json:"orset"`
		LWW   [][]any `json:"lww"`
		PN    [][]any `json:"pn"`
		Death [][]any `json:"death"`
		RGA   [][]any `json:"rga"`
		Tree  [][]any `json:"tree"`
	}
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&described); err != nil {
		t.Fatalf("%s is not a six-section observable state: %v", key, err)
	}

	text := func(v any) json.RawMessage {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s: %v is not a string where the projection requires one", key, v)
		}
		return kotvasync.Text(s)
	}
	plain := func(v any) json.RawMessage {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s: %v is not a string where the projection requires one", key, v)
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("%s: marshalling %q: %v", key, s, err)
		}
		return b
	}
	rows := func(section string, in [][]any, want int, f func(row []any) []json.RawMessage) [][]json.RawMessage {
		out := make([][]json.RawMessage, 0, len(in))
		for _, row := range in {
			if len(row) != want {
				t.Fatalf("%s/%s has a row of %d members, want %d", key, section, len(row), want)
			}
			out = append(out, f(row))
		}
		return out
	}

	return kotvasync.ObservableState{
		// [target, value]: the value is an ext-value and is tagged.
		ORSet: rows("orset", described.ORSet, 2, func(r []any) []json.RawMessage {
			return []json.RawMessage{plain(r[0]), text(r[1])}
		}),
		// [target, field, value]
		LWW: rows("lww", described.LWW, 3, func(r []any) []json.RawMessage {
			return []json.RawMessage{plain(r[0]), plain(r[1]), text(r[2])}
		}),
		// [target, field, total]: the total travels as a decimal STRING, because a
		// PN-counter total is not bounded by what JSON can represent exactly.
		PN: rows("pn", described.PN, 3, func(r []any) []json.RawMessage {
			n, ok := r[2].(json.Number)
			if !ok {
				t.Fatalf("%s/pn carries a non-numeric total %v", key, r[2])
			}
			b, err := json.Marshal(n.String())
			if err != nil {
				t.Fatalf("%s/pn: marshalling the total: %v", key, err)
			}
			return []json.RawMessage{plain(r[0]), plain(r[1]), b}
		}),
		// [object, class]: the class is a bare token, not an ext-value.
		Death: rows("death", described.Death, 2, func(r []any) []json.RawMessage {
			return []json.RawMessage{plain(r[0]), plain(r[1])}
		}),
		// [target, atoms]: the visible atom values, each tagged.
		RGA: rows("rga", described.RGA, 2, func(r []any) []json.RawMessage {
			atoms, ok := r[1].([]any)
			if !ok {
				t.Fatalf("%s/rga carries a non-list of atoms %v", key, r[1])
			}
			tagged := make([]json.RawMessage, 0, len(atoms))
			for _, a := range atoms {
				tagged = append(tagged, text(a))
			}
			b, err := json.Marshal(tagged)
			if err != nil {
				t.Fatalf("%s/rga: marshalling the atoms: %v", key, err)
			}
			return []json.RawMessage{plain(r[0]), b}
		}),
		// [node, parent, ordinal]
		Tree: rows("tree", described.Tree, 3, func(r []any) []json.RawMessage {
			return []json.RawMessage{plain(r[0]), plain(r[1]), plain(r[2])}
		}),
	}
}

// ---------------------------------------------------------------------------
// Generic CBOR shapes
//
// A few §5 wire objects — the pull envelope and the ops response — have no typed
// constructor on the binding, by design: they are transport framing rather than
// substrate state. They are built here through the engine's own generic value
// codec, so even these comparisons go through the engine's encoder rather than
// through hand-assembled bytes.
// ---------------------------------------------------------------------------

// sval encodes a tagged generic CBOR value.
func sval(t *testing.T, in *kotvasync.Instance, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling a generic CBOR value: %v", err)
	}
	b, err := in.EncodeValue(raw)
	if err != nil {
		t.Fatalf("EncodeValue(%s): %v", raw, err)
	}
	return b
}

// decodeSval decodes bytes into the engine's tagged generic spelling.
func decodeSval(t *testing.T, in *kotvasync.Instance, b []byte) json.RawMessage {
	t.Helper()
	v, err := in.DecodeValue(b)
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	return v
}

// wrapPullEnvelope builds the §5.2.1 pull response `{2: FastJoin}`.
func wrapPullEnvelope(t *testing.T, in *kotvasync.Instance, fastjoin []byte) []byte {
	t.Helper()
	inner := decodeSval(t, in, fastjoin)
	return sval(t, in, map[string]any{
		"map": [][]json.RawMessage{{json.RawMessage("2"), inner}},
	})
}

// unwrapPullEnvelope pulls the FastJoin bytes back out of a `{2: FastJoin}`.
func unwrapPullEnvelope(t *testing.T, in *kotvasync.Instance, pull []byte) []byte {
	t.Helper()
	var outer struct {
		Map [][]json.RawMessage `json:"map"`
	}
	if err := json.Unmarshal(decodeSval(t, in, pull), &outer); err != nil {
		t.Fatalf("the pull response is not a CBOR map: %v", err)
	}
	for _, e := range outer.Map {
		if len(e) != 2 {
			t.Fatalf("a pull-response entry has %d members, want a key and a value", len(e))
		}
		var k int64
		if err := json.Unmarshal(e[0], &k); err == nil && k == 2 {
			b, err := in.EncodeValue(e[1])
			if err != nil {
				t.Fatalf("re-encoding the fast-join member: %v", err)
			}
			return b
		}
	}
	t.Fatal("the pull response carries no fast-join member (key 2)")
	return nil
}

// envelopeKeys lists a response envelope's integer keys, so "this is a fast-join
// answer and not an ops answer" is checked against the bytes.
func envelopeKeys(t *testing.T, in *kotvasync.Instance, envelope []byte) []int64 {
	t.Helper()
	var outer struct {
		Map [][]json.RawMessage `json:"map"`
	}
	if err := json.Unmarshal(decodeSval(t, in, envelope), &outer); err != nil {
		t.Fatalf("the response envelope is not a CBOR map: %v", err)
	}
	var keys []int64
	for _, e := range outer.Map {
		var k int64
		if err := json.Unmarshal(e[0], &k); err != nil {
			t.Fatalf("a response envelope key is not an integer: %s", e[0])
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// opsEnvelope builds the ops response `{1: [op, ...]}` in either framing: the
// conformant one embeds each COSE_Sign1 as a CBOR item, the non-conformant one
// wraps each in a byte string (C-06). Both are needed — the wrong answer has to
// be reproducible to be recognised rather than guessed at.
func opsEnvelope(t *testing.T, in *kotvasync.Instance, ops [][]byte, bstrWrapped bool) []byte {
	t.Helper()
	members := make([]json.RawMessage, 0, len(ops))
	for _, op := range ops {
		if bstrWrapped {
			b, err := json.Marshal(map[string]string{"bstr": hex.EncodeToString(op)})
			if err != nil {
				t.Fatalf("marshalling a bstr-wrapped member: %v", err)
			}
			members = append(members, b)
			continue
		}
		members = append(members, decodeSval(t, in, op))
	}
	arr, err := json.Marshal(map[string]any{"arr": members})
	if err != nil {
		t.Fatalf("marshalling the ops array: %v", err)
	}
	return sval(t, in, map[string]any{
		"map": [][]json.RawMessage{{json.RawMessage("1"), arr}},
	})
}

// bstrArray builds the C-06 non-conformant snapshot body: an array whose members
// are byte strings rather than embedded items.
func bstrArray(t *testing.T, in *kotvasync.Instance, ops [][]byte) []byte {
	t.Helper()
	members := make([]json.RawMessage, 0, len(ops))
	for _, op := range ops {
		b, err := json.Marshal(map[string]string{"bstr": hex.EncodeToString(op)})
		if err != nil {
			t.Fatalf("marshalling a bstr-wrapped member: %v", err)
		}
		members = append(members, b)
	}
	return sval(t, in, map[string]any{"arr": members})
}

// sigStructure builds the RFC 9052 Sig_structure a SyncOp signature covers:
// ["Signature1", protected, external_aad, payload]. Built here only so a
// signature over a DIFFERENT external_aad can be offered and refused.
func sigStructure(t *testing.T, in *kotvasync.Instance, protectedHex string, aad, payload []byte) []byte {
	t.Helper()
	part := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshalling a Sig_structure member: %v", err)
		}
		return b
	}
	return sval(t, in, map[string]any{"arr": []json.RawMessage{
		part(map[string]string{"tstr": "Signature1"}),
		part(map[string]string{"bstr": protectedHex}),
		part(map[string]string{"bstr": hex.EncodeToString(aad)}),
		part(map[string]string{"bstr": hex.EncodeToString(payload)}),
	}})
}

// coversMarks turns a §5.1 VersionVector's CBOR into the marks the binding takes.
// The vectors freeze these vectors as bytes only, so this is the one way to hand
// the engine the caller vector a vector describes.
func coversMarks(t *testing.T, in *kotvasync.Instance, vectorHex string) []kotvasync.Mark {
	t.Helper()
	var vv struct {
		BMap [][]json.RawMessage `json:"bmap"`
	}
	if err := json.Unmarshal(decodeSval(t, in, mustHex(t, vectorHex)), &vv); err != nil {
		t.Fatalf("a version vector did not decode as a byte-string-keyed map: %v", err)
	}
	if len(vv.BMap) == 0 {
		t.Fatalf("the version vector %s… is empty", vectorHex[:16])
	}
	marks := make([]kotvasync.Mark, 0, len(vv.BMap))
	for _, e := range vv.BMap {
		if len(e) != 2 {
			t.Fatalf("a version-vector entry has %d members, want an author and an HLC", len(e))
		}
		var author string
		if err := json.Unmarshal(e[0], &author); err != nil {
			t.Fatalf("a version-vector key is not a hex byte string: %s", e[0])
		}
		marks = append(marks, kotvasync.Mark{Author: author, HLC: svalHLC(t, e[1])})
	}
	return marks
}

// svalHLC reads an HLC out of the generic CBOR spelling `{1: wall, 2: counter,
// 3: author}`.
func svalHLC(t *testing.T, s json.RawMessage) kotvasync.HLC {
	t.Helper()
	var m struct {
		Map [][]json.RawMessage `json:"map"`
	}
	if err := json.Unmarshal(s, &m); err != nil {
		t.Fatalf("an HLC did not decode as a CBOR map: %v", err)
	}
	fields := map[int64]json.RawMessage{}
	for _, e := range m.Map {
		if len(e) != 2 {
			t.Fatalf("an HLC entry has %d members", len(e))
		}
		var k int64
		if err := json.Unmarshal(e[0], &k); err != nil {
			t.Fatalf("an HLC key is not an integer: %s", e[0])
		}
		fields[k] = e[1]
	}
	var wall struct {
		Int uint64 `json:"int"`
	}
	var counter struct {
		Int uint32 `json:"int"`
	}
	var author struct {
		Bstr string `json:"bstr"`
	}
	if err := json.Unmarshal(fields[1], &wall); err != nil {
		t.Fatalf("an HLC's wall is not an integer: %v", err)
	}
	if err := json.Unmarshal(fields[2], &counter); err != nil {
		t.Fatalf("an HLC's counter is not an integer: %v", err)
	}
	if err := json.Unmarshal(fields[3], &author); err != nil {
		t.Fatalf("an HLC's author is not a byte string: %v", err)
	}
	return kotvasync.HLC{Wall: wall.Int, Counter: counter.Int, Author: author.Bstr}
}

// sameMarks compares two version vectors as SETS: §5.1 fixes the encoding's key
// order, not the order a caller happens to build marks in.
func sameMarks(t *testing.T, in *kotvasync.Instance, got, want []kotvasync.Mark) bool {
	t.Helper()
	render := func(marks []kotvasync.Mark) []string {
		out := make([]string, 0, len(marks))
		for _, mk := range marks {
			out = append(out, mk.Author+"@"+hlcHex(t, in, mk.HLC))
		}
		sort.Strings(out)
		return out
	}
	return reflect.DeepEqual(render(got), render(want))
}

// ---------------------------------------------------------------------------
// Assertions and vector accessors
// ---------------------------------------------------------------------------

// wantRefusal asserts the engine refused with the frozen §12 registry entry —
// code, name and action together, because a product branching on the code needs
// all three to stay in step, and a refusal with the right code but the wrong
// action is a refusal somebody will retry.
func wantRefusal(t *testing.T, err error, expected map[string]json.RawMessage, what string) {
	t.Helper()
	se, ok := kotvasync.AsSyncError(err)
	if !ok {
		t.Fatalf("%s: expected a substrate refusal, got %v", what, err)
	}
	if want := strOf(t, expected, "error_code"); se.Code != want {
		t.Fatalf("%s refused with code %s, want %s", what, se.Code, want)
	}
	if raw, ok := expected["error_name"]; ok {
		var want string
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatalf("expected/error_name: %v", err)
		}
		if se.Name != want {
			t.Fatalf("%s refused with name %s, want %s", what, se.Name, want)
		}
	}
	if raw, ok := expected["action"]; ok {
		var want string
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatalf("expected/action: %v", err)
		}
		if se.Action != want {
			t.Fatalf("%s refused with action %s, want %s — the action is what a caller does next, "+
				"so the wrong one is acted on rather than logged", what, se.Action, want)
		}
	}
}

// assertRegistryEntry holds a frozen §12 code/name pair against the engine's own
// registry, so a vector that names a refusal names one the engine actually has.
func assertRegistryEntry(t *testing.T, in *kotvasync.Instance, code, name string) kotvasync.RegistryEntry {
	t.Helper()
	entries, err := in.ErrorRegistry()
	if err != nil {
		t.Fatalf("ErrorRegistry: %v", err)
	}
	for _, e := range entries {
		if e.Code == code {
			if e.Name != name {
				t.Fatalf("§12 %s is %q in the engine, frozen as %q", code, e.Name, name)
			}
			return e
		}
	}
	t.Fatalf("the engine's §12 registry has no %s, which the vector names as its refusal", code)
	return kotvasync.RegistryEntry{}
}

// registryCode looks up a §12 code by name, for the few refusals a vector demands
// in prose without freezing the code. Reading it out of the engine keeps the
// number from being written down here twice.
func registryCode(t *testing.T, in *kotvasync.Instance, name string) string {
	t.Helper()
	entries, err := in.ErrorRegistry()
	if err != nil {
		t.Fatalf("ErrorRegistry: %v", err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e.Code
		}
	}
	t.Fatalf("the engine's §12 registry has no %s", name)
	return ""
}

func wantHex(t *testing.T, key string, got []byte, expected map[string]json.RawMessage) {
	t.Helper()
	raw, ok := expected[key]
	if !ok {
		t.Fatalf("the vector has no expected/%q", key)
	}
	var want string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("expected/%s is not a hex string: %v", key, err)
	}
	if hex.EncodeToString(got) != want {
		t.Fatalf("%s:\n  got  %x\n  want %s", key, got, want)
	}
}

// hlcJSON is an HLC as the VECTORS spell it. The binding's own JSON spells the
// author key `author`, so the two cannot share a struct tag.
type hlcJSON struct {
	Wall    uint64 `json:"wall"`
	Counter uint32 `json:"counter"`
	Author  string `json:"author_hex"`
}

func (h hlcJSON) hlc() kotvasync.HLC {
	return kotvasync.HLC{Wall: h.Wall, Counter: h.Counter, Author: h.Author}
}

func hlcOf(t *testing.T, m map[string]json.RawMessage, key string) kotvasync.HLC {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var h hlcJSON
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("%s is not an HLC: %v", key, err)
	}
	if h.Author == "" {
		t.Fatalf("%s names no author; an HLC without one has no total order", key)
	}
	return h.hlc()
}

func addTagsOf(t *testing.T, m map[string]json.RawMessage, key string) []kotvasync.AddTag {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var tags []struct {
		Author string  `json:"author_hex"`
		HLC    hlcJSON `json:"hlc"`
	}
	if err := json.Unmarshal(raw, &tags); err != nil {
		t.Fatalf("%s is not a list of add-tags: %v", key, err)
	}
	out := make([]kotvasync.AddTag, 0, len(tags))
	for _, tag := range tags {
		out = append(out, kotvasync.AddTag{Author: tag.Author, HLC: tag.HLC.hlc()})
	}
	return out
}

func str(t *testing.T, m map[string]json.RawMessage, key string) string {
	t.Helper()
	return strOf(t, m, key)
}

func strOf(t *testing.T, m map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s is not a string: %v", key, err)
	}
	return s
}

func num(t *testing.T, m map[string]json.RawMessage, key string) int64 {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("%s is not a number: %v", key, err)
	}
	return n
}

// boolOf reads a boolean the vector may or may not carry. An absent flag is
// false: a claim the fixture does not make is not one to assert.
func boolOf(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

func hexIn(t *testing.T, m map[string]json.RawMessage, key string) []byte {
	t.Helper()
	return mustHex(t, strOf(t, m, key))
}

func hexOf(t *testing.T, m map[string]json.RawMessage, key string) []byte {
	t.Helper()
	return mustHex(t, strOf(t, m, key))
}

func hexList(t *testing.T, m map[string]json.RawMessage, key string) [][]byte {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var hexes []string
	if err := json.Unmarshal(raw, &hexes); err != nil {
		t.Fatalf("%s is not a list of hex strings: %v", key, err)
	}
	out := make([][]byte, 0, len(hexes))
	for _, h := range hexes {
		out = append(out, mustHex(t, h))
	}
	return out
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("%q is not hex: %v", s, err)
	}
	return b
}

func mustObject(t *testing.T, m map[string]json.RawMessage, key string) map[string]json.RawMessage {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("the vector has no %q", key)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s is not an object: %v", key, err)
	}
	return out
}
