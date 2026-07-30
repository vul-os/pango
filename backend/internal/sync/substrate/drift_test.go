package substrate_test

// The drift bound above the engine.
//
// "0x0A05 exists" and "ErrClockDrift is returned" are both true of an
// implementation that checks AFTER the damage is done, so none of these tests
// assert that an error came back and stop there. A hybrid logical clock never
// moves back down: the only question that matters is whether the clock moved,
// and every test here reads it before and after.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	kotvasync "github.com/vul-os/kotva/bindings/go"
	"github.com/vul-os/pango/backend/internal/store"
	"github.com/vul-os/pango/backend/internal/sync/substrate"
)

// frozen is the fixed "now" every test in this file runs at, so a stamp's
// distance from the present is a property of the test rather than of the day it
// runs on.
const frozen int64 = 1_700_000_000_000

func openAt(t *testing.T, now *int64) (*store.Store, *substrate.Engine) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	e, err := substrate.Open(context.Background(), s, substrate.Options{
		NowFn: func() int64 { return *now },
	})
	if err != nil {
		t.Fatalf("open substrate: %v", err)
	}
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	return s, e
}

// A stamp beyond the bound must be refused BEFORE the engine's clock sees it.
// This is the whole point of the guard: Clock.Observe takes no receiver reading
// and so cannot refuse anything itself, and what it accepts it keeps forever.
func TestObserveRefusesBeforeTheEngineClockSeesIt(t *testing.T) {
	now := frozen
	_, e := openAt(t, &now)

	// Move the clock into the present first, so "did it move?" is a question
	// about this refusal and not about a clock that started at zero.
	if err := e.Observe(store.FormatHLC(frozen, 0, testAuthorA)); err != nil {
		t.Fatalf("observing a present-day stamp: %v", err)
	}
	before, err := e.ClockWall()
	if err != nil {
		t.Fatalf("reading the engine clock: %v", err)
	}

	tenYears := frozen + 10*365*24*60*60*1000
	err = e.Observe(store.FormatHLC(tenYears, 0, testAuthorA))
	if err == nil {
		t.Fatal("a stamp ten years in the future was accepted; one such stamp permanently " +
			"out-orders every honest write on this node")
	}
	if !errors.Is(err, store.ErrClockDrift) {
		t.Fatalf("refusal was %v, want store.ErrClockDrift — the refusal has to be the drift "+
			"bound, not some incidental parse failure that would stop guarding if the stamp "+
			"were well-formed", err)
	}

	after, err := e.ClockWall()
	if err != nil {
		t.Fatalf("reading the engine clock: %v", err)
	}
	if after != before {
		t.Fatalf("the engine clock moved from %d to %d despite the refusal — a rejected stamp "+
			"reached a clock that can never move back down", before, after)
	}
}

// The mirror image, and it is not redundant: without it the test above would
// pass just as well against a guard that had stopped forwarding anything at all,
// which would break causality across machines instead of protecting it.
func TestObserveAcceptsAStampInsideTheBound(t *testing.T) {
	now := frozen
	_, e := openAt(t, &now)

	oneMinute := frozen + 60*1000
	if err := e.Observe(store.FormatHLC(oneMinute, 0, testAuthorA)); err != nil {
		t.Fatalf("a stamp one minute ahead was refused (%v); a minute of ordinary NTP skew "+
			"must not break causal ordering", err)
	}
	wall, err := e.ClockWall()
	if err != nil {
		t.Fatalf("reading the engine clock: %v", err)
	}
	if wall < oneMinute {
		t.Fatalf("engine clock is at %d after observing %d — the stamp was accepted but not "+
			"adopted, so a later local write would sort BEFORE a peer write it happened after",
			wall, oneMinute)
	}
}

// The two bounds guard different paths. This test exists to fail if someone
// "simplifies" them into one, in either direction.
func TestTheTwoBoundsAreNotCollapsedIntoOne(t *testing.T) {
	now := frozen
	_, e := openAt(t, &now)

	engineBound, err := e.SkewBoundMS()
	if err != nil {
		t.Fatalf("reading the engine's skew bound: %v", err)
	}
	if engineBound == 0 {
		t.Fatal("the engine reports no skew bound at all, so its op-ingest path is unguarded " +
			"and this file's reasoning no longer holds")
	}
	if got := e.MaxDrift(); got <= time.Duration(engineBound)*time.Millisecond {
		t.Fatalf("the bound above the engine (%s) is no longer looser than the engine's own "+
			"(%dms). The two guard different paths — ops, and the version-vector stamps that "+
			"are not ops — and neither may be tightened to match the other", got, engineBound)
	}

	// A stamp in the gap between the bounds proves each guards its own path: the
	// looser bound above accepts it, and the engine's op-ingest path still
	// refuses an op carrying it.
	gap := time.Duration(engineBound)*time.Millisecond + 30*time.Second
	if gap >= e.MaxDrift() {
		t.Fatalf("the gap between the two bounds is too narrow to test (engine %dms, "+
			"guard %s)", engineBound, e.MaxDrift())
	}
	inGap := frozen + gap.Milliseconds()
	if err := e.Observe(store.FormatHLC(inGap, 0, testAuthorA)); err != nil {
		t.Fatalf("a stamp %s ahead was refused by the bound above the engine (%v), but that "+
			"bound is %s — it is now doing the engine's job instead of its own",
			gap, err, e.MaxDrift())
	}
}

// An op whose stamp is outside the ENGINE's bound must be refused on the ingest
// path with 0x0A05, and the replica must be byte-identical afterwards. This is
// the bound the engine can apply, and the test that it still does.
func TestIngestRefusesSkewAndLeavesTheReplicaUntouched(t *testing.T) {
	// The author mints at `frozen`. The receiver believes it is well before
	// that, so the op looks like it came from the future.
	authorNow := frozen
	as, author := openAt(t, &authorNow)

	receiverNow := frozen
	_, receiver := openAt(t, &receiverNow)

	op := mintedOp(t, as, author, "building", "01BUILDINGAAAAAAAAAAAAAAAA")

	before, err := receiver.StateRoot()
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}

	// 121 seconds behind the mint: §3's bound is one-sided, so an op from the
	// past is fine and only an op from the future is skewed.
	receiverNow = frozen - 121_000
	err = receiver.Ingest(op)
	if err == nil {
		t.Fatal("an op stamped 121s in the receiver's future was accepted; the engine's §3 " +
			"bound is 120s")
	}
	if !kotvasync.IsRefusal(err, substrate.SkewRefusalCode) {
		t.Fatalf("refusal was %v, want %s (FAIL_CLOSED_BLOCK)", err, substrate.SkewRefusalCode)
	}
	after, err := receiver.StateRoot()
	if err != nil {
		t.Fatalf("reading the state root: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("the replica's state root changed across a refused ingest — the refusal was " +
			"not fail-closed, so the engine now holds an op it told the caller it rejected")
	}

	// And the same op, 119 seconds inside the bound, must be accepted — so the
	// refusal above cannot be an ingest path that rejects everything.
	receiverNow = frozen - 119_000
	if err := receiver.Ingest(op); err != nil {
		t.Fatalf("the same op 119s inside the bound was refused: %v", err)
	}
}

// A stamp naming an author that is not a 32-byte key is refused rather than
// forwarded. The engine's author field is a public key; anything else would be
// a different identity space, and Pango has exactly one.
func TestObserveRefusesAStampWithoutARealKey(t *testing.T) {
	now := frozen
	_, e := openAt(t, &now)

	for _, bad := range []string{
		store.FormatHLC(frozen, 0, "not-a-key"),
		store.FormatHLC(frozen, 0, ""),
		"garbage",
		"",
	} {
		if err := e.Observe(bad); err == nil {
			t.Errorf("stamp %q was accepted; Pango's author field is an Ed25519 public key "+
				"and nothing else may enter the order", bad)
		}
	}
}

// Seeding from this node's own journal bypasses the drift bound, because a node
// whose RTC was wrong for an hour has real ops stamped in the future and must
// still be able to open. That bypass must not become a way IN for a peer: a node
// that had once journalled a far-future stamp would otherwise have permanently
// raised its own bound, and the poisoning path would be open again on exactly
// the nodes that had already been bitten once.
func TestSeedingFromOurOwnJournalIsNotADriftBoundBypass(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// A stamp from this node, well past the drift bound.
	ahead := time.Now().UnixMilli() + (store.DefaultMaxDrift + time.Hour).Milliseconds()
	own := store.FormatHLC(ahead, 7, s.PublicKeyHex())
	if _, err := s.DB().Exec(
		`INSERT INTO oplog (hlc, author, org_id, tbl, row_id, deleted, payload, cose, created_at)
		 VALUES (?, ?, 'org', 'building', 'row', 0, '{}', '', ?)`,
		own, s.PublicKeyHex(), store.Now()); err != nil {
		t.Fatalf("journal a future stamp: %v", err)
	}
	if got := s.MaxJournalledHLC(); got != own {
		t.Fatalf("journal high-water mark is %q, want %q — the rest of this test would prove "+
			"nothing", got, own)
	}

	// Opening must succeed. A node that cannot open because of its own history
	// is a node an operator cannot recover.
	e, err := substrate.Open(context.Background(), s, substrate.Options{})
	if err != nil {
		t.Fatalf("open substrate over a journal holding a future stamp: %v", err)
	}
	defer e.Close(context.Background())

	// And a PEER stamp at the same distance is still refused, even though this
	// node's own journal already reaches that far.
	peer := store.FormatHLC(ahead, 8, testAuthorA)
	err = e.Observe(peer)
	if err == nil {
		t.Fatal("a peer stamp beyond the bound was accepted because this node had already " +
			"journalled one of its own that far ahead — seeding has become a drift-bound bypass")
	}
	if !errors.Is(err, store.ErrClockDrift) {
		t.Fatalf("refusal was %v, want store.ErrClockDrift", err)
	}
}
