package store

// Hybrid logical clocks give Pango a total order over writes that were made
// on machines which never spoke to each other — a contractor's tablet in a
// basement and the office laptop both stamping the same job. Wall clocks alone
// cannot do this: a tablet whose clock is three hours slow would have its edits
// silently lose to every office edit, forever, with no error anywhere.
//
// DUPLICATION: this file is a near-copy of FlowStock's
// backend/internal/store/hlc.go. That is deliberate for now and tracked in
// docs/SYNC.md §11 ("Duplicated sync substrate"), which also records why the
// two are NOT allowed to import each other. The behavioural contract the two
// copies must agree on is pinned by conformance/hlc_vectors.json, run here by
// hlc_vectors_test.go; change the rule there first, in both repos, before
// changing it here.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultMaxDrift bounds how far into the FUTURE a remote stamp may reach and
// still be folded into this clock.
//
// The width bounds above stop a peer widening a field. They do not stop a peer
// inside those widths from handing over a stamp dated 2085: `Observe` would
// fold it forward, and because a hybrid logical clock never moves backwards
// that one stamp then out-orders every honest write on this node forever. No
// error, no recovery short of editing the database. That is a permanent
// poisoning from a single message, so it is bounded here.
//
// The bound is deliberately LOOSER than the substrate engine's own §3
// op-ingest bound (120s, read at runtime from the engine's version rather than
// written down anywhere). The two guard different paths and neither may be
// tightened to match the other:
//
//   - The engine checks an op against the receiver's clock as it is ingested,
//     and refuses with 0x0A05 leaving the replica untouched.
//   - This bound checks stamps that never pass through op ingest at all — a
//     peer's advertised version vector (§6) is a bare map of author to newest
//     HLC, and folding one is how a node decides what to ask for. The engine's
//     Clock.Observe takes no receiver reading and so structurally cannot
//     check. Above it is the only place the check can live.
const DefaultMaxDrift = 5 * time.Minute

// ErrClockDrift means a remote stamp reached further into the future than
// DefaultMaxDrift allows and was refused. The clock is left untouched.
var ErrClockDrift = errors.New("store: remote timestamp exceeds max clock drift")

// ErrMalformedStamp means a remote stamp was not a well-formed, in-width HLC.
var ErrMalformedStamp = errors.New("store: malformed HLC stamp")

// Field-width bounds. Lexical order over the timestamp string equals numeric
// order over (wall, counter, author) ONLY while every field is fixed-width;
// these are what guarantee that, and TestHLCFormat plus the `order` group of
// conformance/hlc_vectors.json are the executable statement of it.
const (
	// MaxCounter is the largest value "%04x" renders in four characters. One
	// more and the field widens to five, at which point
	// "1700000000000-10000-k" sorts BEFORE "1700000000000-ffff-k" and causal
	// order silently inverts.
	//
	// This is reachable from the network, not only from a 65k-ops-in-one-
	// millisecond burst: Observe folds a REMOTE counter forward, so a peer
	// sending 0xffff drives this node to 0x10000. Nothing errors — the mesh
	// just stops agreeing. Hence the spill in bump() rather than a comment.
	MaxCounter = 0xffff
	// MaxWallMS is the largest value "%013d" renders in thirteen digits
	// (year 2286). Same failure mode as MaxCounter, far away in time but
	// reachable now via a wildly-skewed or hostile remote timestamp.
	MaxWallMS = 9999999999999
)

// HLC is a hybrid logical clock. Timestamps are strings that sort lexically in
// causal order: "{unix_ms:013d}-{counter:04x}-{author_hex}".
//
// The third field is the AUTHOR's Ed25519 public key, not a node identifier.
// That is deliberate and differs from FlowStock: it keeps the order a property
// of the object itself, so an op relayed through a third node orders identically
// to one received directly, and the DMTAP-SYNC binding (§7) is lossless because
// its algebra breaks the same tie on the same value. A node-id tie-break would
// mean the built-in engine and the substrate engine could pick different
// winners from identical history.
type HLC struct {
	mu      sync.Mutex
	author  string
	lastMS  int64
	counter uint32
	nowFn   func() int64 // injectable for tests
	// maxDrift bounds how far ahead of nowFn an observed stamp may be. Zero
	// means DefaultMaxDrift — a zero-value HLC is bounded, not unbounded, so a
	// clock built by struct literal (the conformance harness does) cannot be
	// silently unguarded.
	maxDrift time.Duration
}

func wallMS() int64 { return time.Now().UnixMilli() }

// NewHLC builds a clock stamping for author (public key hex), seeded past
// lastSeen — an existing maximum timestamp, normally MAX(hlc) from the oplog.
//
// The seeding is what makes a backwards-moving wall clock survivable: a machine
// that reboots with a dead RTC, or an NTP step backwards mid-shift, would
// otherwise mint timestamps below writes it has already journalled and quietly
// lose every subsequent edit to its own history.
func NewHLC(author, lastSeen string) *HLC {
	return NewHLCWithMaxDrift(author, lastSeen, DefaultMaxDrift)
}

// NewHLCWithMaxDrift is NewHLC with an explicit future-drift bound. maxDrift <= 0
// means DefaultMaxDrift.
//
// Seeding past lastSeen deliberately does NOT go through the drift bound.
// lastSeen is this node's own journal high-water mark, not a remote claim: if it
// is already out beyond the bound (a stamp folded in before this bound existed,
// or an hour of writes made while the RTC was wrong) then refusing to seed past
// it would leave the clock minting stamps BELOW writes this node has already
// journalled, and it would lose every subsequent edit to its own history. The
// bound exists to stop a stranger moving this clock; it must not stop the clock
// recovering its own past.
func NewHLCWithMaxDrift(author, lastSeen string, maxDrift time.Duration) *HLC {
	return NewHLCWithClock(author, lastSeen, maxDrift, nil)
}

// NewHLCWithClock is NewHLCWithMaxDrift with the wall-clock reading injected.
// now == nil means the real clock.
//
// This exists for the substrate binding rather than for tests. That binding
// applies this drift bound and then hands the SAME reading to the merge engine as
// the receiver's clock; if the two read the clock separately they can disagree
// about what time it is here, and then a stamp can pass one bound and fail the
// other for no reason a reader could reconstruct.
func NewHLCWithClock(author, lastSeen string, maxDrift time.Duration, now func() int64) *HLC {
	if now == nil {
		now = wallMS
	}
	h := &HLC{author: author, nowFn: now, maxDrift: maxDrift}
	if lastSeen != "" {
		h.fold(lastSeen)
	}
	return h
}

// MaxDrift is the effective future-drift bound this clock applies in Observe.
func (h *HLC) MaxDrift() time.Duration {
	if h.maxDrift <= 0 {
		return DefaultMaxDrift
	}
	return h.maxDrift
}

// ParseHLC splits a timestamp into (unix_ms, counter, author_hex).
//
// A timestamp whose fields fall outside their fixed width is rejected rather
// than parsed. Accepting one would let a single remote op drag this node's
// clock past a width boundary through Observe, breaking lexical ordering for
// every timestamp minted afterwards — so this fails closed at the edge.
func ParseHLC(ts string) (ms int64, counter uint32, author string, ok bool) {
	parts := strings.SplitN(ts, "-", 3)
	if len(parts) != 3 {
		return 0, 0, "", false
	}
	m, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || m < 0 || m > MaxWallMS {
		return 0, 0, "", false
	}
	c, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil || c > MaxCounter {
		return 0, 0, "", false
	}
	return m, uint32(c), parts[2], true
}

// FormatHLC renders the wire form. The only place the layout is written down —
// the width bounds above are meaningless if a second call site disagrees.
//
// Exported because the substrate binding has to render a stamp back from the
// engine's (wall, counter, author) triple, and a second copy of "%013d-%04x-%s"
// living over there is exactly the disagreement this function exists to prevent.
func FormatHLC(ms int64, counter uint32, author string) string {
	return fmt.Sprintf("%013d-%04x-%s", ms, counter, author)
}

// bump advances the logical counter by one, spilling into the wall field when
// the counter would outgrow the four hex digits "%04x" gives it. Spilling
// keeps the stamp strictly greater than the previous one AND keeps every field
// fixed-width, which is the invariant lexical ordering depends on.
//
// At the very top of the wall range there is nowhere left to spill; the counter
// then saturates. Order stops being strict there (two ticks can tie) but it
// never inverts, which is the property convergence actually needs.
func (h *HLC) bump() {
	if h.counter < MaxCounter {
		h.counter++
		return
	}
	if h.lastMS < MaxWallMS {
		h.lastMS++
		h.counter = 0
	}
}

// Tick mints a timestamp strictly greater than every timestamp this clock has
// minted or observed.
func (h *HLC) Tick() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.nowFn()
	if now > h.lastMS && now <= MaxWallMS {
		h.lastMS = now
		h.counter = 0
	} else {
		h.bump()
	}
	return FormatHLC(h.lastMS, h.counter, h.author)
}

// Observe folds a remote timestamp into the clock so future ticks sort after
// it. This is how causality crosses machines: once we have seen a peer's write,
// everything we write afterwards is ordered after it regardless of whose wall
// clock is ahead.
//
// A stamp that is malformed, out of width, or further ahead than the drift
// bound is ignored — the same silent-drop the malformed case has always had,
// which is why this signature does not change and why
// conformance/hlc_vectors.json's `observe` group still describes it exactly.
// Callers that need to know a stamp was refused (the substrate binding does,
// because a refusal there has to propagate as a sync error) use ObserveChecked.
func (h *HLC) Observe(remote string) { _ = h.ObserveChecked(remote) }

// ObserveChecked is Observe with the refusal made visible. It returns
// ErrMalformedStamp or ErrClockDrift, and in both cases the clock is left
// exactly as it was — the assertion a test can actually make, since "an error
// was returned" is also true of an implementation that checks after the damage
// is done.
func (h *HLC) ObserveChecked(remote string) error {
	if err := h.WithinDrift(remote); err != nil {
		return err
	}
	h.fold(remote)
	return nil
}

// WithinDrift applies the bound WITHOUT folding: it reports whether remote is a
// well-formed, in-width stamp no further ahead than the drift bound, and leaves
// this clock exactly as it was either way.
//
// It is separate from ObserveChecked because a caller that has a second clock to
// keep in step — the substrate binding has the engine's — needs to decide
// whether a stamp is admissible before anything anywhere has moved. Folding
// first and unwinding on failure is not available: a hybrid logical clock has no
// way to move back down.
func (h *HLC) WithinDrift(remote string) error {
	ms, _, _, ok := ParseHLC(remote)
	if !ok {
		return fmt.Errorf("%w: %q", ErrMalformedStamp, remote)
	}
	h.mu.Lock()
	bound := h.MaxDrift().Milliseconds()
	ahead := ms - h.nowFn()
	h.mu.Unlock()
	if ahead > bound {
		return fmt.Errorf("%w: stamp is %dms ahead of this node, bound is %dms",
			ErrClockDrift, ahead, bound)
	}
	return nil
}

// fold is the unguarded merge step: the clock arithmetic with no drift bound in
// front of it. Only two callers may use it — ObserveChecked, which has just
// applied the bound, and seeding from this node's own journal.
func (h *HLC) fold(remote string) {
	ms, counter, _, ok := ParseHLC(remote)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ms > h.lastMS || (ms == h.lastMS && counter >= h.counter) {
		h.lastMS = ms
		h.counter = counter
		h.bump()
	}
}

// Current reports the stamp this clock would repeat, without advancing it.
//
// It exists so a test can prove a refusal was side-effect-free: a hybrid
// logical clock never moves backwards, so "did this rejected stamp touch the
// clock?" is only answerable by reading the clock without moving it.
func (h *HLC) Current() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return FormatHLC(h.lastMS, h.counter, h.author)
}
