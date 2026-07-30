package substrate_test

// Does Pango hand the shared engine the right ops?
//
// vectors_test.go proves the engine computes the algebra correctly. That is a
// different claim from this one and neither implies the other: an engine that
// passes every frozen vector will still converge on the wrong answer if Pango
// addresses two distinct facts as one element, or classifies an append-only
// table as a register. These tests are about the mapping.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vul-os/pango/backend/internal/store"
	propsync "github.com/vul-os/pango/backend/internal/sync"
	"github.com/vul-os/pango/backend/internal/sync/substrate"
)

// Two author keys that are real 32-byte Ed25519 public keys as far as the stamp
// format is concerned. Nothing here verifies a signature against them — they
// stand in for peers whose ops we only ever construct, never sign.
const (
	testAuthorA = "aaaa000000000000000000000000000000000000000000000000000000000001"
	testAuthorB = "bbbb000000000000000000000000000000000000000000000000000000000002"
)

// testOrg is the organisation every op in this file belongs to.
const testOrg = "01ORGAAAAAAAAAAAAAAAAAAAAA"

// opAt builds an unsigned Pango op with an explicit stamp.
//
// It does not go through store.Journal, deliberately: Journal mints from the real
// wall clock, and a test whose stamps move with the calendar cannot say anything
// about how far a stamp is from the present.
func opAt(author string, ms int64, counter uint32, tbl, rowID string, payload string, deleted bool) store.Op {
	return store.Op{
		HLC:     store.FormatHLC(ms, counter, author),
		Author:  author,
		OrgID:   testOrg,
		Tbl:     tbl,
		RowID:   rowID,
		Deleted: deleted,
		Payload: json.RawMessage(payload),
	}
}

// sign mints op's envelope through e, which must be the node that authored it.
func sign(t *testing.T, e *substrate.Engine, op store.Op) store.Op {
	t.Helper()
	cose, err := e.Mint(op)
	if err != nil {
		t.Fatalf("minting %s/%s: %v", op.Tbl, op.RowID, err)
	}
	if cose == "" {
		t.Fatalf("minting %s/%s produced an empty envelope", op.Tbl, op.RowID)
	}
	op.Cose = cose
	return op
}

// mintedOp is the common case: an op authored by s's node at the frozen instant,
// signed by e.
func mintedOp(t *testing.T, s *store.Store, e *substrate.Engine, tbl, rowID string) store.Op {
	t.Helper()
	payload := fmt.Sprintf(`{"id":%q,"org_id":%q,"name":"a row"}`, rowID, testOrg)
	return sign(t, e, opAt(s.PublicKeyHex(), frozen, 0, tbl, rowID, payload, false))
}

// ── §4.3: element identity ──────────────────────────────────────────────────

// THE MONEY TEST. §4.3 identifies a set element by its VALUE, so two adds
// carrying identical bytes are ONE element with two add-tags. Pango reads a
// job's cost as SUM(amount_minor) over its entries (docs/SYNC.md §4), so two
// entries that collapsed into one read as half the money — converged, on every
// replica, with no error anywhere.
//
// The payloads here are byte-identical on purpose. That is not a contrived case:
// two people costing the same job for the same amount with the same description
// while partitioned is the scenario §4 was written about.
func TestTwoIdenticalCostEntriesDoNotCollapse(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	// Byte-identical bodies. Even the embedded "id" matches, which is stronger
	// than reality needs — Pango mints a fresh ULID per row — precisely so
	// this test does not lean on the payload happening to differ.
	const body = `{"job_id":"01JOBAAAAAAAAAAAAAAAAAAAAA","amount_minor":120000,` +
		`"currency":"ZAR","kind":"materials","description":"paint"}`

	first := sign(t, a, opAt(as.PublicKeyHex(), frozen, 0,
		"cost_entry", "01COSTAAAAAAAAAAAAAAAAAAAA", body, false))
	second := sign(t, a, opAt(as.PublicKeyHex(), frozen, 1,
		"cost_entry", "01COSTBBBBBBBBBBBBBBBBBBBB", body, false))

	for _, op := range []store.Op{first, second} {
		if err := b.Ingest(op); err != nil {
			t.Fatalf("ingesting %s: %v", op.RowID, err)
		}
	}

	held, err := b.UnionMembers("cost_entry")
	if err != nil {
		t.Fatalf("reading cost_entry members: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("the engine holds %d cost entries, want 2. Two identical facts have collapsed "+
			"into one element, so this job's cost reads as R1 200 instead of R2 400 — on every "+
			"replica, with no error anywhere. See mapping.go on element identity.", len(held))
	}

	// And the amounts really are both there, which is the product-level claim.
	var total int64
	seen := map[string]bool{}
	for _, op := range held {
		var row struct {
			AmountMinor int64 `json:"amount_minor"`
		}
		if err := json.Unmarshal(op.Payload, &row); err != nil {
			t.Fatalf("element payload did not survive the round trip: %v", err)
		}
		total += row.AmountMinor
		seen[op.RowID] = true
	}
	if total != 240000 {
		t.Errorf("SUM(amount_minor) over the engine's elements = %d, want 240000", total)
	}
	if !seen[first.RowID] || !seen[second.RowID] {
		t.Errorf("the engine holds %v, want both %s and %s",
			seen, first.RowID, second.RowID)
	}
}

// The two things that keep those facts apart are the row id in the TARGET and
// the stamp in the VALUE. This test says so as an assertion rather than as a
// comment: strip both and the two elements are byte-identical, which is the
// collapse the mapping is arranged to prevent.
//
// It fails if a future change coarsens the target (grouping cost entries under
// their job, say, to make a per-job read one lookup) without keeping the stamp.
func TestElementIdentityIsOpIdentityNotPayloadIdentity(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	const body = `{"job_id":"01JOBAAAAAAAAAAAAAAAAAAAAA","minutes":90,"note":"callout"}`
	first := sign(t, a, opAt(as.PublicKeyHex(), frozen, 0,
		"time_entry", "01TIMEAAAAAAAAAAAAAAAAAAAA", body, false))
	second := sign(t, a, opAt(as.PublicKeyHex(), frozen, 1,
		"time_entry", "01TIMEBBBBBBBBBBBBBBBBBBBB", body, false))

	for _, op := range []store.Op{first, second} {
		if err := b.Ingest(op); err != nil {
			t.Fatalf("ingesting %s: %v", op.RowID, err)
		}
	}
	held, err := b.UnionMembers("time_entry")
	if err != nil {
		t.Fatalf("reading time_entry members: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("the engine holds %d time entries, want 2", len(held))
	}

	// The payloads ARE equal. That is the premise: value identity alone would
	// have merged these two into one element, so something other than the value
	// has to be carrying their distinctness.
	if !bytes.Equal(held[0].Payload, held[1].Payload) {
		t.Fatal("the two elements' payloads differ, so this test is no longer about value " +
			"identity and proves nothing about the collapse")
	}
	if held[0].RowID == held[1].RowID {
		t.Fatal("both elements share a row id, so the target is not what distinguishes them")
	}
	if held[0].HLC == held[1].HLC {
		t.Fatal("both elements share a stamp, so the stamp is not what distinguishes them")
	}
}

// Replaying an op must not add a second element. Element identity equals OP
// identity, and the engine addresses an op by a 33-byte content address, so a
// replay is the same content and therefore the same element.
//
// This is the property Pango's oplog primary key already has (it is keyed on
// the stamp), and the two have to agree or one node's row count and the engine's
// element count drift apart.
func TestReplayingAnOpAddsNoElement(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	op := sign(t, a, opAt(as.PublicKeyHex(), frozen, 0, "job_event",
		"01EVENTAAAAAAAAAAAAAAAAAAA", `{"job_id":"01JOBA","kind":"note","body":"hi"}`, false))

	for i := range 3 {
		if err := b.Ingest(op); err != nil {
			t.Fatalf("ingest %d of the same op: %v", i, err)
		}
	}
	held, err := b.UnionMembers("job_event")
	if err != nil {
		t.Fatalf("reading job_event members: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("three ingests of ONE op left %d elements, want 1 — the engine and the oplog "+
			"now disagree about how many facts exist", len(held))
	}
}

// ── §4.4: registers ─────────────────────────────────────────────────────────

// Both engines must pick the same winner. Pango's built-in engine upserts only
// when `excluded.hlc > tbl.hlc`; the shared engine orders by (wall, counter,
// author). The suite learned the hard way that two engines can both converge and
// still disagree about who won — and a replica set cannot survive that.
func TestRegisterWinnerIsTheGreaterStamp(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	const row = "01BUILDINGAAAAAAAAAAAAAAAA"
	older := sign(t, a, opAt(as.PublicKeyHex(), frozen, 0, "building", row,
		`{"id":"`+row+`","name":"old name"}`, false))
	newer := sign(t, a, opAt(as.PublicKeyHex(), frozen+1000, 0, "building", row,
		`{"id":"`+row+`","name":"new name"}`, false))

	// Ingest newest first, so a winner chosen by arrival order would be wrong.
	for _, op := range []store.Op{newer, older} {
		if err := b.Ingest(op); err != nil {
			t.Fatalf("ingesting %s: %v", op.HLC, err)
		}
	}
	payload, hlc, deleted, ok := b.Resolve("building", row)
	if !ok {
		t.Fatal("the engine holds no opinion about a row it has just been given twice")
	}
	if hlc != newer.HLC {
		t.Fatalf("winner is %s, want %s — the newer write lost, so the two engines disagree "+
			"and this replica set cannot converge", hlc, newer.HLC)
	}
	if deleted {
		t.Error("winner is a deletion, but neither write was one")
	}
	if !strings.Contains(string(payload), "new name") {
		t.Errorf("winning payload is %s, want the newer one", payload)
	}
}

// An exact (wall, counter) tie breaks on the author public key, in both engines.
// Pango's node id IS its public key, which is the precondition docs/SYNC.md §10
// names for the swap being safe; this asserts the precondition rather than
// trusting it.
func TestRegisterTieBreaksOnTheAuthorKey(t *testing.T) {
	const row = "01UNITAAAAAAAAAAAAAAAAAAAA"
	fromA := opAt(testAuthorA, frozen, 4, "unit", row, `{"id":"`+row+`","label":"A"}`, false)
	fromB := opAt(testAuthorB, frozen, 4, "unit", row, `{"id":"`+row+`","label":"B"}`, false)

	// These are peers' ops, so they cannot be signed here. Ingest is the signed
	// path by design; the ordering claim is about the stamps, and Pango's own
	// stamp comparison is a string compare, so assert it directly on the two
	// stamps the engine would compare.
	if !(fromA.HLC < fromB.HLC) {
		t.Fatalf("Pango orders %s after %s; the tie-break is meant to be the author key "+
			"ascending, and B's key sorts after A's", fromA.HLC, fromB.HLC)
	}
	// And the engine agrees, via the mapping's own round trip: the author it
	// reads out of a stamp is the same 32 bytes Pango put in.
	for _, op := range []store.Op{fromA, fromB} {
		_, _, author, ok := store.ParseHLC(op.HLC)
		if !ok || author != op.Author {
			t.Fatalf("stamp %s does not carry author %s, so the two engines are breaking ties "+
				"on different values", op.HLC, op.Author)
		}
	}
}

// A soft delete is an ordinary register write and must be dominated by the next
// one. A §4.5 death certificate would dominate every later write instead, so a
// unit deleted and re-created with the same key would be invisible on every
// replica at once — see mapping.go's "deliberate non-choices".
func TestSoftDeleteIsNotADeathCertificate(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	const row = "01UNITBBBBBBBBBBBBBBBBBBBB"
	created := sign(t, a, opAt(as.PublicKeyHex(), frozen, 0, "unit", row,
		`{"id":"`+row+`","label":"3A"}`, false))
	deleted := sign(t, a, opAt(as.PublicKeyHex(), frozen+1000, 0, "unit", row,
		`{"id":"`+row+`","label":"3A"}`, true))
	recreated := sign(t, a, opAt(as.PublicKeyHex(), frozen+2000, 0, "unit", row,
		`{"id":"`+row+`","label":"3A again"}`, false))

	for _, op := range []store.Op{created, deleted, recreated} {
		if err := b.Ingest(op); err != nil {
			t.Fatalf("ingesting %s: %v", op.HLC, err)
		}
	}
	payload, hlc, isDeleted, ok := b.Resolve("unit", row)
	if !ok {
		t.Fatal("the engine holds no opinion about the re-created row")
	}
	if isDeleted {
		t.Fatal("the row is still deleted after being re-created with a later stamp. A " +
			"deletion that no later write can undo has been used for an ordinary soft delete, " +
			"and the re-created unit is now invisible on every replica at once")
	}
	if hlc != recreated.HLC {
		t.Errorf("winner is %s, want the re-creation %s", hlc, recreated.HLC)
	}
	if !strings.Contains(string(payload), "3A again") {
		t.Errorf("winning payload is %s, want the re-created row", payload)
	}
}

// ── convergence ─────────────────────────────────────────────────────────────

// Two replicas given the same ops in opposite orders must hold byte-identical
// state. The §6.1 state root is what makes this a measurement rather than an
// eyeball — it is also the answer to docs/SYNC.md §11's "verifying convergence"
// open question, which Pango had no way to answer before.
func TestConvergenceIsOrderIndependent(t *testing.T) {
	now := frozen
	as, author := openAt(t, &now)
	_, left := openAt(t, &now)
	_, right := openAt(t, &now)

	const building = "01BUILDINGCCCCCCCCCCCCCCCC"
	ops := []store.Op{
		sign(t, author, opAt(as.PublicKeyHex(), frozen, 0, "building", building,
			`{"id":"`+building+`","name":"first"}`, false)),
		sign(t, author, opAt(as.PublicKeyHex(), frozen+10, 0, "building", building,
			`{"id":"`+building+`","name":"second"}`, false)),
		sign(t, author, opAt(as.PublicKeyHex(), frozen+20, 0, "cost_entry",
			"01COSTCCCCCCCCCCCCCCCCCCCC", `{"amount_minor":500,"currency":"ZAR"}`, false)),
		sign(t, author, opAt(as.PublicKeyHex(), frozen+30, 0, "cost_entry",
			"01COSTDDDDDDDDDDDDDDDDDDDD", `{"amount_minor":500,"currency":"ZAR"}`, false)),
		sign(t, author, opAt(as.PublicKeyHex(), frozen+40, 0, "job_event",
			"01EVENTBBBBBBBBBBBBBBBBBBB", `{"kind":"note","body":"done"}`, false)),
	}

	for _, op := range ops {
		if err := left.Ingest(op); err != nil {
			t.Fatalf("left ingesting %s: %v", op.HLC, err)
		}
	}
	for i := len(ops) - 1; i >= 0; i-- {
		if err := right.Ingest(ops[i]); err != nil {
			t.Fatalf("right ingesting %s: %v", ops[i].HLC, err)
		}
	}

	lroot, err := left.StateRoot()
	if err != nil {
		t.Fatalf("left state root: %v", err)
	}
	rroot, err := right.StateRoot()
	if err != nil {
		t.Fatalf("right state root: %v", err)
	}
	if !bytes.Equal(lroot, rroot) {
		t.Fatalf("state roots differ after the same ops in opposite orders:\n  %x\n  %x",
			lroot, rroot)
	}
	// A root that is equal because both replicas are empty would prove nothing.
	if len(lroot) == 0 {
		t.Fatal("the state root is empty, so the comparison above compared nothing")
	}
	if held, err := left.UnionMembers("cost_entry"); err != nil || len(held) != 2 {
		t.Fatalf("left holds %d cost entries (err %v), want 2 — the roots may be equal "+
			"because both replicas dropped the same ops", len(held), err)
	}
}

// ── failing closed ──────────────────────────────────────────────────────────

// An envelope that does not agree with the op record it travelled with must be
// refused. Otherwise the bytes the engine merges and the bytes Pango writes to
// its tables are different writes — silent divergence on one node, which no
// amount of syncing repairs.
func TestEnvelopeThatDisagreesWithItsOpRecordIsRefused(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	good := mintedOp(t, as, a, "building", "01BUILDINGDDDDDDDDDDDDDDDD")

	before, err := b.StateRoot()
	if err != nil {
		t.Fatalf("state root: %v", err)
	}

	for _, tc := range []struct {
		name string
		op   store.Op
	}{
		{"payload rewritten after signing", func() store.Op {
			op := good
			op.Payload = json.RawMessage(`{"id":"x","name":"tampered"}`)
			return op
		}()},
		{"stamp rewritten after signing", func() store.Op {
			op := good
			op.HLC = store.FormatHLC(frozen+9999, 0, as.PublicKeyHex())
			return op
		}()},
		{"row id rewritten after signing", func() store.Op {
			op := good
			op.RowID = "01BUILDINGEEEEEEEEEEEEEEEE"
			return op
		}()},
		{"org rewritten after signing", func() store.Op {
			op := good
			op.OrgID = "01ORGBBBBBBBBBBBBBBBBBBBBB"
			return op
		}()},
		{"deletion flag flipped after signing", func() store.Op {
			op := good
			op.Deleted = true
			return op
		}()},
		{"envelope replaced with rubbish", func() store.Op {
			op := good
			op.Cose = "deadbeef"
			return op
		}()},
		{"no envelope at all", func() store.Op {
			op := good
			op.Cose = ""
			return op
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := b.Ingest(tc.op); err == nil {
				t.Fatal("accepted: the engine merged bytes that differ from the op record " +
					"Pango will write to its tables")
			}
			after, err := b.StateRoot()
			if err != nil {
				t.Fatalf("state root: %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("the replica changed across a refused ingest, so the refusal was not " +
					"fail-closed")
			}
		})
	}

	// The unmodified op must still be accepted, or every assertion above would
	// hold just as well against an Ingest that refuses everything.
	if err := b.Ingest(good); err != nil {
		t.Fatalf("the untampered op was refused too (%v); the refusals above prove nothing", err)
	}
}

// A table with no merge rule fails closed at mint. The built-in engine can
// afford to ignore an op for a table it has never heard of, because ignoring it
// changes nothing; minting one into the engine cannot be undone.
func TestAnUnclassifiedTableIsRefusedAtMint(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)

	// `peer` is a real table that is deliberately NOT replicated: it carries no
	// hlc column, so it has no place in either ownership class.
	if propsync.ClassOf("peer") != propsync.ClassUnknown {
		t.Fatal("`peer` has acquired a merge rule; pick another unreplicated table for this test")
	}
	_, err := a.Mint(opAt(as.PublicKeyHex(), frozen, 0, "peer", "01PEERAAAAAAAAAAAAAAAAAAAA",
		`{"id":"01PEERAAAAAAAAAAAAAAAAAAAA"}`, false))
	if err == nil {
		t.Fatal("an op for an unreplicated table was minted into the engine; there is no " +
			"ownership class for it, so whichever one it landed in is wrong")
	}
	if !strings.Contains(err.Error(), "classes.go") {
		t.Errorf("refusal was %q; it should point the reader at where the rule would be "+
			"declared", err)
	}
}

// Minting an op this node did not author must fail. The stamp names its author
// and the signature is made with this node's key; if the two could differ, an op
// would claim a key it was not signed by.
func TestMintRefusesAnOpThisNodeDidNotAuthor(t *testing.T) {
	now := frozen
	_, a := openAt(t, &now)

	if _, err := a.Mint(opAt(testAuthorB, frozen, 0, "building", "01BUILDINGFFFFFFFFFFFFFFFF",
		`{"id":"x"}`, false)); err == nil {
		t.Fatal("minted an op authored by another node; the envelope would claim a key this " +
			"node cannot sign for")
	}
}

// ── the seam, wired ─────────────────────────────────────────────────────────

// With an engine installed, every locally authored write must carry an envelope.
// A write journalled without one is a write the rest of the fleet cannot verify,
// and store.Journal is meant to abort rather than produce one.
func TestInstalledEngineMintsAnEnvelopeForEveryWrite(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	e, err := substrate.Open(context.Background(), s, substrate.Options{})
	if err != nil {
		t.Fatalf("open substrate: %v", err)
	}
	defer e.Close(context.Background())
	s.SetMerger(e)

	// Journal one op directly: the repo layer's own writers are covered by
	// repo's tests, and going through them here would test their validation
	// rather than the seam.
	var stamp string
	err = s.Tx(func(tx *sql.Tx) error {
		var err error
		stamp, err = s.Journal(tx, testOrg, "building", "01BUILDINGGGGGGGGGGGGGGGGG",
			map[string]any{"id": "01BUILDINGGGGGGGGGGGGGGGGG", "name": "seam"}, false)
		return err
	})
	if err != nil {
		t.Fatalf("journalling through the installed engine: %v", err)
	}

	var cose string
	if err := s.DB().QueryRow(`SELECT cose FROM oplog WHERE hlc = ?`, stamp).Scan(&cose); err != nil {
		t.Fatalf("reading the journalled envelope: %v", err)
	}
	if cose == "" {
		t.Fatal("the op was journalled with an empty envelope while an engine was installed; " +
			"peers running the shared engine would refuse it as unverifiable")
	}

	// And the engine holds it, so Resolve can answer about our own writes rather
	// than only about peers'.
	if _, hlc, _, ok := e.Resolve("building", "01BUILDINGGGGGGGGGGGGGGGGG"); !ok || hlc != stamp {
		t.Fatalf("the engine resolved %q/ok=%t for a row this node just wrote, want %s",
			hlc, ok, stamp)
	}
	if got := e.Stats().Minted; got != 1 {
		t.Errorf("Stats().Minted = %d, want 1", got)
	}
}

// An op the engine refuses must not reach the database. A row this node holds and
// the engine does not is silent divergence, and nothing removes it afterwards.
func TestApplyOpsDoesNotJournalWhatTheEngineRefuses(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	e, err := substrate.Open(context.Background(), s, substrate.Options{})
	if err != nil {
		t.Fatalf("open substrate: %v", err)
	}
	defer e.Close(context.Background())

	// The organisation has to exist locally or ApplyOps drops the op for a
	// reason that has nothing to do with the engine.
	if _, err := s.DB().Exec(
		`INSERT INTO organisation (id, name, hlc, deleted, created_at) VALUES (?, 'Org', '', 0, ?)`,
		testOrg, store.Now()); err != nil {
		t.Fatalf("seed organisation: %v", err)
	}
	s.SetMerger(e)

	bad := opAt(testAuthorA, frozen, 0, "building", "01BUILDINGHHHHHHHHHHHHHHHH",
		`{"id":"01BUILDINGHHHHHHHHHHHHHHHH","name":"unverifiable"}`, false)
	bad.Cose = "deadbeef" // present, so it is not treated as a legacy op

	eng := propsync.New(s)
	applied, err := eng.ApplyOps([]store.Op{bad})
	if err == nil {
		t.Fatal("ApplyOps accepted an op whose envelope does not verify")
	}
	if applied != 0 {
		t.Errorf("ApplyOps reports %d applied, want 0", applied)
	}
	var rows int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM building WHERE id = ?`, bad.RowID).Scan(&rows); err != nil {
		t.Fatalf("count buildings: %v", err)
	}
	if rows != 0 {
		t.Fatal("the refused op's row was written anyway: this node now holds a row the merge " +
			"engine rejected, and no amount of syncing will reconcile that")
	}
	var journalled int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM oplog WHERE hlc = ?`, bad.HLC).Scan(&journalled); err != nil {
		t.Fatalf("count oplog: %v", err)
	}
	if journalled != 0 {
		t.Error("the refused op was journalled, so it would be relayed onward to peers")
	}
}

// An op with NO envelope was authored by a node still running the built-in
// engine. It is counted, not refused: refusing would present a misconfigured
// fleet as a transport failure, and docs/SYNC.md §10 wants the mixed state
// visible instead.
func TestLegacyOpsAreCountedNotRefused(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	e, err := substrate.Open(context.Background(), s, substrate.Options{})
	if err != nil {
		t.Fatalf("open substrate: %v", err)
	}
	defer e.Close(context.Background())

	if _, err := s.DB().Exec(
		`INSERT INTO organisation (id, name, hlc, deleted, created_at) VALUES (?, 'Org', '', 0, ?)`,
		testOrg, store.Now()); err != nil {
		t.Fatalf("seed organisation: %v", err)
	}
	s.SetMerger(e)

	// A payload is a whole row: sync/apply.go builds its INSERT from SQLite's
	// column catalogue and reads every column out of the payload with
	// json_extract, so a missing key becomes NULL rather than falling back to the
	// column's DEFAULT. Every NOT NULL column of `building` has to be here.
	legacy := opAt(testAuthorA, frozen, 0, "building", "01BUILDINGIIIIIIIIIIIIIIII",
		`{"id":"01BUILDINGIIIIIIIIIIIIIIII","org_id":"`+testOrg+`","name":"from a legacy node",`+
			`"address":"1 Long Street","unit_scheme":"","created_at":"2026-07-30T00:00:00Z"}`, false)

	eng := propsync.New(s)
	applied, err := eng.ApplyOps([]store.Op{legacy})
	if err != nil {
		t.Fatalf("ApplyOps refused a legacy op: %v", err)
	}
	if applied != 1 {
		t.Fatalf("ApplyOps applied %d ops, want 1", applied)
	}
	if got := e.Stats().Legacy; got != 1 {
		t.Errorf("Stats().Legacy = %d, want 1 — a fleet running two algebras at once has to "+
			"be visible somewhere", got)
	}
}

// ── the mapping is complete ─────────────────────────────────────────────────

// Every classified table must actually round-trip through the mapping. A table
// classified in classes.go but unmappable here would fail at the first write to
// it, in production, on whichever node happened to make that write first.
func TestEveryClassifiedTableRoundTrips(t *testing.T) {
	now := frozen
	as, a := openAt(t, &now)
	_, b := openAt(t, &now)

	if len(propsync.MergeClasses) == 0 {
		t.Fatal("there are no classified tables, so this test checks nothing")
	}
	var i int64
	for tbl, class := range propsync.MergeClasses {
		t.Run(tbl, func(t *testing.T) {
			i++
			rowID := fmt.Sprintf("01ROW%021d", i)
			payload := fmt.Sprintf(`{"id":%q,"org_id":%q}`, rowID, testOrg)
			op := sign(t, a, opAt(as.PublicKeyHex(), frozen+i, 0, tbl, rowID, payload, false))
			if err := b.Ingest(op); err != nil {
				t.Fatalf("a %s table classified as %s did not survive mint+ingest: %v",
					tbl, class, err)
			}
			gotPayload, gotHLC, gotDeleted, ok := b.Resolve(tbl, rowID)
			if !ok {
				t.Fatalf("the engine holds no opinion about %s/%s after ingesting it", tbl, rowID)
			}
			if gotHLC != op.HLC {
				t.Errorf("resolved stamp %s, want %s", gotHLC, op.HLC)
			}
			if gotDeleted {
				t.Error("resolved as deleted, but the op was not a deletion")
			}
			if !bytes.Equal(gotPayload, op.Payload) {
				t.Errorf("resolved payload %s, want %s", gotPayload, op.Payload)
			}
		})
	}
}
