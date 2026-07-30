package substrate

// The store.Merger implementation. See mapping.go's package doc for the
// ownership-class mapping this file encodes.
//
// # What this replaces, and what it deliberately does not
//
// It replaces the ALGEBRA: with this installed, the shared engine is what
// decides who won a conflicting write and what an add-only set contains.
//
// It does NOT take over minting. store.Journal mints an op's stamp from
// Pango's own HLC and then hands the finished op here, and that stays true,
// because the stamp is not only an ordering device in Pango — it is the
// oplog's primary key and the `hlc` column of every replicated row. There is
// still exactly one minter, so there is no second timeline: this engine's clock
// is fed and never asked. `Mint` folds in the stamp Pango just minted;
// `Ingest` folds in a peer's, through the drift bound below. Nothing here ever
// calls the engine clock's Tick.
//
// # The drift bound, and why it lives above the engine
//
// The engine enforces §3's skew bound (120s, read from it at runtime, never
// written down) on the op-ingest path, refusing with 0x0A05 and leaving the
// replica untouched. Its Clock.Observe takes no receiver reading and so
// structurally cannot check anything — and a hybrid logical clock never moves
// back down, so one far-future stamp folded in there out-orders every honest
// write on this node permanently.
//
// So every stamp is put through store.HLC's drift bound BEFORE it reaches the
// engine's clock, and both the bound and the receiver reading the engine is
// given come from the same nowFn. The two bounds guard different paths and
// neither may be loosened to match the other; drift_test.go asserts that they
// have not been collapsed into one.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	kotvasync "github.com/vul-os/kotva/bindings/go"
	"github.com/vul-os/pango/backend/internal/store"
	propsync "github.com/vul-os/pango/backend/internal/sync"
)

// SkewRefusalCode is the §3 refusal the engine raises on the op-ingest path when
// an op's stamp is outside its own skew bound. Its action is FAIL_CLOSED_BLOCK:
// the replica is not modified.
const SkewRefusalCode = "0x0A05"

// DefaultNS is the namespace every Pango op is minted into. See the package
// doc on why org_id is not the namespace.
const DefaultNS = "pango"

// Options configures Open.
type Options struct {
	// NS is the substrate namespace. Empty means DefaultNS. It must be the
	// same on every node of a replica set: an op in another namespace is
	// refused, not merged.
	NS string
	// CacheDir persists wazero's compiled machine code across process starts.
	// Empty means compile on every boot, which costs a few hundred
	// milliseconds once per process.
	CacheDir string
	// MaxDrift bounds how far into the future a peer's stamp may reach. Zero
	// means store.DefaultMaxDrift. It must stay LOOSER than the engine's own
	// op-ingest bound — see SkewBoundMS.
	MaxDrift time.Duration
	// NowFn reads the wall clock in milliseconds. Nil means the real clock.
	// One function serves both the drift bound and the receiver reading handed
	// to the engine, so the two can never disagree about the local time.
	NowFn func() int64
}

// Engine is the shared merge engine, wired to one Pango store.
//
// The binding serialises calls on one Instance internally; the mutex here makes
// that model explicit rather than adding a second one on top of it.
type Engine struct {
	mu    sync.Mutex
	rt    *kotvasync.Runtime
	in    *kotvasync.Instance
	eng   *kotvasync.Engine
	clock *kotvasync.Clock

	// guard is a store.HLC kept for its drift predicate alone. Its own folded
	// state is never read: the engine's clock above is the timeline, and a
	// second one consulted in parallel would be a second source of truth about
	// what time it is here. What guard supplies is the bound the engine
	// structurally cannot apply — see Observe.
	guard *store.HLC

	signer kotvasync.Signer
	author string // this node's key, lowercase hex — the substrate's author id
	ns     string
	kinds  kinds
	nowFn  func() int64

	stats Stats
}

// Stats counts what the engine has been asked to do. A fleet running two
// algebras at once is meant to be visible rather than silently half-merged, so
// Legacy in particular is worth surfacing.
type Stats struct {
	// Minted is ops this node authored and signed.
	Minted int `json:"minted"`
	// Ingested is peer ops the engine accepted.
	Ingested int `json:"ingested"`
	// Fresh is how many of those were new to the replica rather than a replay.
	Fresh int `json:"fresh"`
	// Refused is ops or stamps the engine or the drift bound rejected.
	Refused int `json:"refused"`
	// Legacy is peer ops that arrived with no envelope at all, meaning they
	// were authored by a node running the built-in engine.
	Legacy int `json:"legacy"`
	// ResolveErrors counts reads the engine could not answer coherently.
	// Resolve cannot return an error through store.Merger, so a non-zero
	// count here is the only signal that it went wrong.
	ResolveErrors int `json:"resolve_errors"`
}

// Open compiles the engine and wires it to s's identity. Expensive (the wasm
// module is compiled) — call once per process, before serving traffic.
func Open(ctx context.Context, s *store.Store, opt Options) (*Engine, error) {
	signerKey, ok := s.CryptoSigner()
	if !ok {
		return nil, errors.New("substrate: the store has no node identity to sign with")
	}
	author := s.PublicKeyHex()
	if len(author) != authorHexLen {
		return nil, fmt.Errorf(
			"substrate: this node's key is %q, which is not a 32-byte Ed25519 public key in hex",
			author)
	}
	raw, err := hex.DecodeString(author)
	if err != nil {
		return nil, fmt.Errorf("substrate: this node's key is not hex: %w", err)
	}

	ns := opt.NS
	if ns == "" {
		ns = DefaultNS
	}
	nowFn := opt.NowFn
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UnixMilli() }
	}

	var opts []kotvasync.Option
	if opt.CacheDir != "" {
		opts = append(opts, kotvasync.WithCompilationCacheDir(opt.CacheDir))
	}
	rt, err := kotvasync.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("substrate: compiling the merge engine: %w", err)
	}
	in, err := rt.Instance(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("substrate: instantiating the merge engine: %w", err)
	}

	e := &Engine{
		rt:     rt,
		in:     in,
		signer: kotvasync.CryptoSigner{Key: signerKey},
		author: author,
		ns:     ns,
		nowFn:  nowFn,
		// Seeded from this node's own journal so the engine's clock starts
		// where Pango's does. Seeding bypasses the drift bound by design —
		// see store.NewHLCWithClock.
		guard: store.NewHLCWithClock(author, s.MaxJournalledHLC(), opt.MaxDrift, nowFn),
	}
	if e.eng, err = in.NewEngine(); err != nil {
		_ = e.closeLocked(ctx)
		return nil, fmt.Errorf("substrate: opening a replica: %w", err)
	}
	if e.clock, err = in.NewClock(raw); err != nil {
		_ = e.closeLocked(ctx)
		return nil, fmt.Errorf("substrate: opening the engine clock: %w", err)
	}
	// Never hard-code the §4.2 kind numbers: lww_set is 3 and set_add is 1, so
	// a hard-coded number is wrong in the direction that still encodes and
	// describes the opposite operation.
	k, err := in.OpKinds()
	if err != nil {
		_ = e.closeLocked(ctx)
		return nil, fmt.Errorf("substrate: reading the engine's op kinds: %w", err)
	}
	e.kinds = kinds{setAdd: k.SetAdd, lwwSet: k.LWWSet}

	// The whole point of a shared drift bound is that it sits outside the
	// engine's. If a future engine tightened its own bound past Pango's, the
	// guard would be dead code and the poisoning path would be open again, so
	// this refuses to open rather than to run unguarded.
	v, err := in.Version()
	if err != nil {
		_ = e.closeLocked(ctx)
		return nil, fmt.Errorf("substrate: reading the engine version: %w", err)
	}
	if bound := e.guard.MaxDrift(); bound <= time.Duration(v.HLCSkewMS)*time.Millisecond {
		_ = e.closeLocked(ctx)
		return nil, fmt.Errorf(
			"substrate: the drift bound above the engine (%s) is not looser than the engine's own "+
				"§3 bound (%dms); the two guard different paths and collapsing them leaves "+
				"Clock.Observe unguarded", bound, v.HLCSkewMS)
	}
	return e, nil
}

var _ store.Merger = (*Engine)(nil)

// Close releases the engine. Safe to call once.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closeLocked(ctx)
}

func (e *Engine) closeLocked(ctx context.Context) error {
	if e.clock != nil {
		_ = e.clock.Close()
		e.clock = nil
	}
	if e.eng != nil {
		_ = e.eng.Close()
		e.eng = nil
	}
	if e.in != nil {
		_ = e.in.Close(ctx)
		e.in = nil
	}
	if e.rt != nil {
		err := e.rt.Close(ctx)
		e.rt = nil
		return err
	}
	return nil
}

// ── store.Merger ────────────────────────────────────────────────────────────

// Mint expresses a locally authored op as a signed COSE_Sign1 envelope, hex
// encoded, and folds its stamp into the engine's clock.
//
// The signature is detached-then-attached through the binding's Signer
// interface, so the seed never leaves store: no entry point in the engine
// accepts key material, by design.
func (e *Engine) Mint(op store.Op) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if op.Author != e.author {
		e.stats.Refused++
		return "", fmt.Errorf("substrate: op claims author %s but this node is %s",
			op.Author, e.author)
	}
	sop, err := syncOp(op, e.kinds, e.ns)
	if err != nil {
		e.stats.Refused++
		return "", err
	}
	opBytes, err := e.in.EncodeOp(sop)
	if err != nil {
		e.stats.Refused++
		return "", fmt.Errorf("substrate: encoding op %s: %w", op.HLC, err)
	}
	cose, err := e.in.SignOp(opBytes, e.signer)
	if err != nil {
		e.stats.Refused++
		return "", fmt.Errorf("substrate: signing op %s: %w", op.HLC, err)
	}
	// A node's own freshly minted stamp is not a remote claim, so it does not
	// go through the drift bound — it came from this node's clock, which the
	// bound is measured against. It still has to reach the engine's clock, or
	// the engine would consider a stamp it minted itself to be from the future.
	if err := e.observeLocked(op.HLC, false); err != nil {
		e.stats.Refused++
		return "", err
	}
	// Ingesting our own envelope keeps the replica complete: without it the
	// engine would hold every peer's writes and none of ours, and Resolve would
	// answer with a stale winner.
	fresh, err := e.eng.IngestSigned(cose, uint64(e.nowFn()))
	if err != nil {
		e.stats.Refused++
		return "", fmt.Errorf("substrate: ingesting our own op %s: %w", op.HLC, err)
	}
	if fresh {
		e.stats.Fresh++
	}
	e.stats.Minted++
	return hex.EncodeToString(cose), nil
}

// Ingest verifies a peer's envelope, checks it against the op record it claims
// to describe, and merges it. A refusal is an error and the replica is left
// untouched: the engine fails closed rather than merging unverified state.
func (e *Engine) Ingest(op store.Op) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if op.Cose == "" {
		e.stats.Refused++
		return fmt.Errorf("substrate: op %s carries no signed envelope", op.HLC)
	}
	cose, err := hex.DecodeString(op.Cose)
	if err != nil {
		e.stats.Refused++
		return fmt.Errorf("substrate: op %s envelope is not hex: %w", op.HLC, err)
	}

	// Verify and cross-check BEFORE ingesting. An envelope that disagrees with
	// its op record must not reach the replica, because nothing removes it
	// afterwards.
	opBytes, err := e.in.VerifySignedOp(cose)
	if err != nil {
		e.stats.Refused++
		return fmt.Errorf("substrate: op %s envelope did not verify: %w", op.HLC, err)
	}
	sop, err := e.in.DecodeOp(opBytes)
	if err != nil {
		e.stats.Refused++
		return fmt.Errorf("substrate: op %s did not decode: %w", op.HLC, err)
	}
	back, err := fromSyncOp(sop, e.kinds, e.ns)
	if err != nil {
		e.stats.Refused++
		return err
	}
	if err := sameOp(op, back); err != nil {
		e.stats.Refused++
		return err
	}

	// The drift bound, applied without moving anything, so a refusal here is
	// provably side-effect-free.
	if err := e.guard.WithinDrift(op.HLC); err != nil {
		e.stats.Refused++
		return fmt.Errorf("substrate: refusing peer stamp %s: %w", op.HLC, err)
	}

	fresh, err := e.eng.IngestSigned(cose, uint64(e.nowFn()))
	if err != nil {
		e.stats.Refused++
		return fmt.Errorf("substrate: op %s refused: %w", op.HLC, err)
	}
	if err := e.observeLocked(op.HLC, true); err != nil {
		// The op is in the replica and the clock did not move. That is safe —
		// the clock only ever gates FUTURE local stamps — but it is not
		// silent.
		e.stats.Refused++
		return err
	}
	if fresh {
		e.stats.Fresh++
	}
	e.stats.Ingested++
	return nil
}

// Resolve returns the winning payload for a row, the winner's stamp, and whether
// the winner is a deletion. ok is false when the engine holds no opinion — an
// unclassified table, or a row it has never seen.
//
// store.Merger gives this no error return, so a read the engine could not answer
// coherently is counted in Stats().ResolveErrors rather than lost.
func (e *Engine) Resolve(tbl, rowID string) (json.RawMessage, string, bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	payload, hlc, deleted, ok, err := e.resolveLocked(tbl, rowID)
	if err != nil {
		e.stats.ResolveErrors++
		return nil, "", false, false
	}
	return payload, hlc, deleted, ok
}

func (e *Engine) resolveLocked(tbl, rowID string) (json.RawMessage, string, bool, bool, error) {
	target := targetOf(tbl, rowID)
	switch propsync.ClassOf(tbl) {
	case propsync.ClassRegister:
		cell, err := e.eng.LWWCell(target, registerField)
		if err != nil {
			return nil, "", false, false, fmt.Errorf("substrate: reading %s: %w", target, err)
		}
		if cell == nil {
			return nil, "", false, false, nil
		}
		raw, err := untagBytes(cell.Value)
		if err != nil {
			return nil, "", false, false, err
		}
		_, deleted, payload, err := parseRowBody(raw)
		if err != nil {
			return nil, "", false, false, err
		}
		return payload, store.FormatHLC(int64(cell.HLC.Wall), cell.HLC.Counter, cell.HLC.Author),
			deleted, true, nil

	case propsync.ClassUnion:
		// SetMembers is engine-wide: it returns every (target, element) pair,
		// so the filter is here rather than in the call.
		pairs, err := e.eng.SetMembers()
		if err != nil {
			return nil, "", false, false, fmt.Errorf("substrate: reading set members: %w", err)
		}
		var bestHLC string
		var bestPayload json.RawMessage
		var bestDeleted bool
		found := false
		for _, pair := range pairs {
			if len(pair) != 2 {
				continue
			}
			var got string
			if err := json.Unmarshal(pair[0], &got); err != nil || got != target {
				continue
			}
			raw, err := untagBytes(pair[1])
			if err != nil {
				return nil, "", false, false, err
			}
			elemHLC, body, err := splitStamped(raw)
			if err != nil {
				return nil, "", false, false, err
			}
			_, deleted, payload, err := parseRowBody(body)
			if err != nil {
				return nil, "", false, false, err
			}
			// An add-only row is immutable, so there should be exactly one
			// element per target. If two authors minted the same row id with
			// different bodies, the greatest stamp wins — a DETERMINISTIC
			// answer, where the built-in engine's INSERT OR IGNORE keeps
			// whichever arrived first and so can differ per node.
			if !found || elemHLC > bestHLC {
				bestHLC, bestPayload, bestDeleted, found = elemHLC, payload, deleted, true
			}
		}
		if !found {
			return nil, "", false, false, nil
		}
		return bestPayload, bestHLC, bestDeleted, true, nil

	default:
		return nil, "", false, false, nil
	}
}

// UnionMembers returns every element the engine holds for an add-only table, as
// the ops they were minted from, oldest stamp first.
//
// It is the read that matters for money and hours. Pango reads a job's cost as
// SUM(amount_minor) over its entries, so "how many elements does the engine
// actually hold?" is the same question as "is the total right?" — and §4.3
// identifies an element by its VALUE, which is exactly how a total silently
// halves. Nothing else exposes that count, so nothing else could assert it.
func (e *Engine) UnionMembers(tbl string) ([]store.Op, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if propsync.ClassOf(tbl) != propsync.ClassUnion {
		return nil, fmt.Errorf("substrate: %q is not an add-only table", tbl)
	}
	pairs, err := e.eng.SetMembers()
	if err != nil {
		return nil, fmt.Errorf("substrate: reading set members: %w", err)
	}
	prefix := tbl + targetSep
	var out []store.Op
	for _, pair := range pairs {
		if len(pair) != 2 {
			continue
		}
		var target string
		if err := json.Unmarshal(pair[0], &target); err != nil {
			continue
		}
		if !strings.HasPrefix(target, prefix) {
			continue
		}
		_, rowID, err := splitTarget(target)
		if err != nil {
			return nil, err
		}
		raw, err := untagBytes(pair[1])
		if err != nil {
			return nil, err
		}
		hlc, body, err := splitStamped(raw)
		if err != nil {
			return nil, err
		}
		orgID, deleted, payload, err := parseRowBody(body)
		if err != nil {
			return nil, err
		}
		_, _, author, ok := store.ParseHLC(hlc)
		if !ok {
			return nil, fmt.Errorf("substrate: element of %s carries an unreadable stamp %q",
				target, hlc)
		}
		out = append(out, store.Op{
			HLC: hlc, Author: author, OrgID: orgID,
			Tbl: tbl, RowID: rowID, Deleted: deleted, Payload: payload,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HLC < out[j].HLC })
	return out, nil
}

// NoteLegacy records that an op arrived with no envelope: it was authored by a
// node still running the built-in engine. Counted rather than refused, because
// refusing would make a mixed fleet look like a transport failure instead of the
// configuration mistake it is (docs/SYNC.md §10 — the engine is a
// deployment-wide switch, never a gradual rollout).
func (e *Engine) NoteLegacy() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stats.Legacy++
}

// ── clock ───────────────────────────────────────────────────────────────────

// Observe folds a peer's stamp into the engine's clock through the drift bound.
//
// This is the entry point for stamps that are NOT ops and therefore never pass
// the engine's own op-ingest check: a peer's advertised version vector
// (docs/SYNC.md §6) is a bare map of author to newest stamp, and folding one is
// how a node decides what to ask for. It is the whole reason the bound exists
// above the engine.
func (e *Engine) Observe(remote string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.observeLocked(remote, true); err != nil {
		e.stats.Refused++
		return err
	}
	return nil
}

// observeLocked folds remote into the engine's clock. guarded=false is only for
// a stamp this node minted itself.
func (e *Engine) observeLocked(remote string, guarded bool) error {
	if guarded {
		if err := e.guard.WithinDrift(remote); err != nil {
			return fmt.Errorf("substrate: refusing stamp %s: %w", remote, err)
		}
	}
	ms, counter, author, ok := store.ParseHLC(remote)
	if !ok {
		return fmt.Errorf("substrate: %q is not a well-formed HLC stamp", remote)
	}
	if len(author) != authorHexLen {
		return fmt.Errorf("substrate: stamp author %q is not a 32-byte public key in hex", author)
	}
	if err := e.clock.Observe(kotvasync.HLC{
		Wall: uint64(ms), Counter: counter, Author: author,
	}); err != nil {
		return fmt.Errorf("substrate: observing stamp %s: %w", remote, err)
	}
	return nil
}

// ClockWall reads the engine clock's wall component without advancing it.
//
// It exists so a test can prove a refusal was side-effect-free. "An error was
// returned" is also true of an implementation that checks after the damage is
// done, and a hybrid logical clock cannot be moved back to check.
func (e *Engine) ClockWall() (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	h, err := e.clock.Current()
	if err != nil {
		return 0, fmt.Errorf("substrate: reading the engine clock: %w", err)
	}
	return int64(h.Wall), nil
}

// SkewBoundMS is the engine's own §3 op-ingest skew bound, read from the linked
// engine rather than written down here.
func (e *Engine) SkewBoundMS() (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, err := e.in.Version()
	if err != nil {
		return 0, fmt.Errorf("substrate: reading the engine version: %w", err)
	}
	return v.HLCSkewMS, nil
}

// MaxDrift is the bound applied above the engine.
func (e *Engine) MaxDrift() time.Duration { return e.guard.MaxDrift() }

// ── inspection ──────────────────────────────────────────────────────────────

// Stats reports what the engine has been asked to do.
func (e *Engine) Stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats
}

// StateRoot is the §6.1 content address of this replica's observable state. Two
// nodes with equal roots hold equal state — which is what docs/SYNC.md §11's
// "verifying convergence" open question asked for and did not have.
func (e *Engine) StateRoot() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.eng.StateRoot()
}

// VersionVector is the engine's per-author high-water marks, in Pango's own
// stamp spelling so it can be compared against store.Vector directly.
func (e *Engine) VersionVector() (map[string]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	marks, err := e.eng.VersionVector()
	if err != nil {
		return nil, fmt.Errorf("substrate: reading the version vector: %w", err)
	}
	out := make(map[string]string, len(marks))
	for _, m := range marks {
		out[m.Author] = store.FormatHLC(int64(m.HLC.Wall), m.HLC.Counter, m.HLC.Author)
	}
	return out, nil
}

// Version describes the linked engine.
func (e *Engine) Version() (kotvasync.Version, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.in.Version()
}

// sameOp checks that an envelope decodes back to the op record it travelled
// with. A mismatch means the bytes the engine merged and the bytes Pango will
// write to its tables are not the same write, which is silent divergence on one
// node — so it is refused rather than reconciled.
func sameOp(claimed, decoded store.Op) error {
	switch {
	case claimed.HLC != decoded.HLC:
		return fmt.Errorf("substrate: envelope stamps %s, op record says %s",
			decoded.HLC, claimed.HLC)
	case claimed.Author != decoded.Author:
		return fmt.Errorf("substrate: envelope author %s, op record says %s",
			decoded.Author, claimed.Author)
	case claimed.Tbl != decoded.Tbl || claimed.RowID != decoded.RowID:
		return fmt.Errorf("substrate: envelope addresses %s/%s, op record says %s/%s",
			decoded.Tbl, decoded.RowID, claimed.Tbl, claimed.RowID)
	case claimed.OrgID != decoded.OrgID:
		return fmt.Errorf("substrate: envelope carries org %s, op record says %s",
			decoded.OrgID, claimed.OrgID)
	case claimed.Deleted != decoded.Deleted:
		return fmt.Errorf("substrate: envelope says deleted=%t, op record says deleted=%t",
			decoded.Deleted, claimed.Deleted)
	case !bytes.Equal(claimed.Payload, decoded.Payload):
		return fmt.Errorf("substrate: envelope payload differs from the op record's for %s",
			claimed.HLC)
	}
	return nil
}
