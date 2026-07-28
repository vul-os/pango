package store

// Hybrid logical clocks give PropFix a total order over writes that were made
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
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Field-width bounds. Lexical order over the timestamp string equals numeric
// order over (wall, counter, author) ONLY while every field is fixed-width;
// these are what guarantee that, and TestHLCFormat plus the `order` group of
// conformance/hlc_vectors.json are the executable statement of it.
const (
	// maxCounter is the largest value "%04x" renders in four characters. One
	// more and the field widens to five, at which point
	// "1700000000000-10000-k" sorts BEFORE "1700000000000-ffff-k" and causal
	// order silently inverts.
	//
	// This is reachable from the network, not only from a 65k-ops-in-one-
	// millisecond burst: Observe folds a REMOTE counter forward, so a peer
	// sending 0xffff drives this node to 0x10000. Nothing errors — the mesh
	// just stops agreeing. Hence the spill in bump() rather than a comment.
	maxCounter = 0xffff
	// maxWallMS is the largest value "%013d" renders in thirteen digits
	// (year 2286). Same failure mode as maxCounter, far away in time but
	// reachable now via a wildly-skewed or hostile remote timestamp.
	maxWallMS = 9999999999999
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
	h := &HLC{author: author, nowFn: wallMS}
	if lastSeen != "" {
		h.Observe(lastSeen)
	}
	return h
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
	if err != nil || m < 0 || m > maxWallMS {
		return 0, 0, "", false
	}
	c, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil || c > maxCounter {
		return 0, 0, "", false
	}
	return m, uint32(c), parts[2], true
}

// formatHLC renders the wire form. The only place the layout is written down —
// the width bounds above are meaningless if a second call site disagrees.
func formatHLC(ms int64, counter uint32, author string) string {
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
	if h.counter < maxCounter {
		h.counter++
		return
	}
	if h.lastMS < maxWallMS {
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
	if now > h.lastMS && now <= maxWallMS {
		h.lastMS = now
		h.counter = 0
	} else {
		h.bump()
	}
	return formatHLC(h.lastMS, h.counter, h.author)
}

// Observe folds a remote timestamp into the clock so future ticks sort after
// it. This is how causality crosses machines: once we have seen a peer's write,
// everything we write afterwards is ordered after it regardless of whose wall
// clock is ahead.
func (h *HLC) Observe(remote string) {
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
