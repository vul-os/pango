// Package substrate binds Pango to the shared merge engine — the compiled
// Rust algebra published as github.com/vul-os/kotva/bindings/go and executed
// through wazero, so CGO_ENABLED=0 and single-static-binary cross-compilation
// are preserved.
//
// It implements store.Merger (docs/SYNC.md §10), which Pango built as a seam
// before the substrate existed. Installing it makes the shared engine the merge
// authority for this deployment. Leaving it out keeps the built-in HLC engine.
// The choice is made at boot and never mixed: two engines with different total
// orders cannot share a replica set, and the failure is silent divergence
// rather than an error.
//
// # The mapping, and the selection test behind each choice
//
// Pango's replicated state is a set of SQL tables, each row addressed by a
// ULID `id`, each write journalled as one store.Op. docs/SYNC.md §3 already
// divides those tables into two merge rules, and internal/sync/classes.go holds
// that division as data. This package maps those two rules onto the shared
// algebra's two ownership classes, one for one:
//
//	sync.ClassRegister  ->  §4.4 LWW register  (kind lww_set)
//	sync.ClassUnion     ->  §4.3 add-only set  (kind set_add)
//
// The class numbers are read from the engine at open time via Instance.OpKinds
// and never written down here. They are not stable across the algebra's kinds
// in the way a reader expects — lww_set is 3 and set_add is 1 — so a hard-coded
// number is wrong in exactly the direction that still encodes, still ingests,
// and describes the opposite operation.
//
// ## Registers: reference data and the single-writer aggregates
//
//	target  "<tbl>/<row_id>"
//	field   "row"
//	value   bstr, rowBody(op) — see below
//
// Granularity is per ROW, not per column, because Pango journals a whole row
// per write: store.Journal marshals the entire domain struct as the op payload
// and sync/apply.go upserts every column from it. A per-column mapping would
// claim a merge granularity the product does not actually have, and would
// converge differently from the built-in engine on two concurrent edits to
// different columns of one row. §4.1.1 places granularity in the address space,
// so "one field named row" is the honest address for a row-grained register.
//
// Selection test for §4.4 (is last-writer-wins the right rule?): yes for every
// table here. Each is either reference data, where the newest human edit is
// what everyone should see, or an aggregate whose owning organisation is the
// single writer (docs/SYNC.md §5) — so a conflicting write is a genuine race
// between that organisation's own devices, resolved the same way on every node.
//
// ## Add-only sets: money, hours, evidence, blobs
//
//	target  "<tbl>/<row_id>"
//	value   bstr, stamp(op) ‖ rowBody(op)
//	        where stamp = wall(8 BE) ‖ counter(4 BE) ‖ author(32 raw)
//
// THE STAMP IS LOAD-BEARING. §4.3 identifies a set element by its VALUE: two
// adds carrying identical bytes are one element with two add-tags, not two
// elements. Pango reads a job's cost as SUM(amount_minor) over its entries,
// so two entries that collapsed into one would read as half the money — on
// every replica, converged, with no error anywhere. Stamping the element with
// the op's own (wall, counter, author) makes element identity equal OP identity,
// which is what Pango's oplog primary key already is, so no two distinct ops
// can collapse no matter how alike their payloads are.
//
// The row_id in the target is not a substitute for the stamp. It makes the
// common case safe, and it is exactly the case a test would cover; the stamp is
// what keeps the guarantee if the target space is ever coarsened (grouping cost
// entries under their job, say, to make a per-job read one lookup).
//
// Selection test for §4.3 (is add-only the right rule?): yes — these rows are
// immutable by design and a correction is a new row with a negative amount, not
// an edit (docs/SYNC.md §4). No set_remove is ever minted, so every set here is
// grow-only and merge is plain union.
//
// ## Deliberate non-choices
//
// Recorded as decisions rather than left to be inferred from absence.
//
// NO §4.5 death certificate, anywhere. Selection test: is there any user action
// that restores this thing using the same ordinary operation that created it?
// Yes, for every deletion Pango has — a deleted unit can be re-created with
// the same key, and docs/SYNC.md §3 has a test that keeps that true. A
// certificate dominates every later write to the object, so using one for an
// ordinary soft delete would make the re-created row invisible on every replica
// at once, with no error. Pango's `deleted` flag replicates as an ordinary
// register write and is dominated by the next one, which is the point.
//
// NO §4.6 PN-counter for money or hours. A counter would converge on the same
// total and discard the entries, and the entries ARE the audit trail: who
// recorded what, when, and what corrected it. §4.6's own closing note permits
// SUM-over-a-union instead, which is what Pango already does at read time.
//
// NO §4.7 sequence and NO §4.8 movable tree. Nothing in the schema is an
// ordered list or a reparentable hierarchy; unit-under-building is a foreign
// key that never moves.
//
// NS is not used as the tenancy boundary. org_id is Pango's tenancy boundary
// (§4.2 of ARCHITECTURE.md) and it is enforced on the apply path by
// Engine.orgKnown, which drops an op for an organisation this node does not
// hold. Carrying org_id inside the value keeps one engine per node rather than
// one per organisation, which matters because Engine.SetMembers is engine-wide.
// The cost is that §7's cross-namespace reference check does no work for
// Pango; that is a real gap, and it is a gap in a check that duplicates one
// Pango already performs in SQL.
//
// # Where the rest of this is written down
//
//   - internal/sync/classes.go — which table is in which class, held against
//     the live schema by classes_test.go.
//   - docs/SYNC.md §3 — the same division in prose, with the reasoning.
//   - vectors_test.go — the frozen conformance vectors, which prove the engine
//     computes the algebra. substrate_test.go proves Pango hands it the right
//     ops; the two are different claims and neither implies the other.
package substrate

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	kotvasync "github.com/vul-os/kotva/bindings/go"
	"github.com/vul-os/pango/backend/internal/store"
	propsync "github.com/vul-os/pango/backend/internal/sync"
)

// EngineName identifies this algebra in anything that has to refuse a peer
// running the other one. It is deliberately not Pango's own engine name: the
// built-in HLC engine ties on the author key spelled as Pango spells it and
// this engine ties on the same 32 bytes, so the two DO agree on the order — but
// they disagree about element identity in an add-only set, and that is enough
// that a replica set must be all one or all the other.
const EngineName = "dmtap-sync-v0"

// registerField is the single field name a row-grained register writes to. See
// the package doc on why granularity is per row.
const registerField = "row"

// targetSep separates table from row id in an op's target.
const targetSep = "/"

// authorHexLen is the hex length of a 32-byte Ed25519 public key.
const authorHexLen = 64

// stampLen is wall(8) + counter(4) + author(32).
const stampLen = 8 + 4 + 32

// kinds holds the §4.2 kind numbers this package uses, read from the engine.
type kinds struct{ setAdd, lwwSet uint8 }

func targetOf(tbl, rowID string) string { return tbl + targetSep + rowID }

// splitTarget is the inverse. A row id may not contain the separator; Pango
// ids are Crockford base32 (store.NewID) so it never does, and an op whose
// target claims otherwise is refused rather than split on the wrong slash.
func splitTarget(target string) (tbl, rowID string, err error) {
	tbl, rowID, ok := strings.Cut(target, targetSep)
	if !ok || tbl == "" || rowID == "" {
		return "", "", fmt.Errorf("substrate: %q is not a <table>/<row-id> target", target)
	}
	if strings.Contains(rowID, targetSep) {
		return "", "", fmt.Errorf("substrate: target %q has more than one separator", target)
	}
	return tbl, rowID, nil
}

// rowBody is the replicated body of an op: everything about the row that is not
// already in the address or the stamp.
//
//	orgLen(2 BE) ‖ org_id ‖ deleted(1) ‖ payload
//
// Fixed-width heads and one length prefix, so the payload needs no escaping and
// the decode is total. org_id is in here rather than in NS — see the package
// doc. `deleted` is here rather than being a §4.5 certificate — likewise.
func rowBody(op store.Op) ([]byte, error) {
	if len(op.OrgID) > 0xffff {
		return nil, fmt.Errorf("substrate: org id is %d bytes, too long to encode", len(op.OrgID))
	}
	b := make([]byte, 0, 2+len(op.OrgID)+1+len(op.Payload))
	var n [2]byte
	binary.BigEndian.PutUint16(n[:], uint16(len(op.OrgID)))
	b = append(b, n[:]...)
	b = append(b, op.OrgID...)
	if op.Deleted {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return append(b, op.Payload...), nil
}

// parseRowBody is rowBody's inverse.
func parseRowBody(b []byte) (orgID string, deleted bool, payload json.RawMessage, err error) {
	if len(b) < 3 {
		return "", false, nil, fmt.Errorf(
			"substrate: row body is %d bytes, too short to carry its header", len(b))
	}
	n := int(binary.BigEndian.Uint16(b[0:2]))
	if len(b) < 2+n+1 {
		return "", false, nil, fmt.Errorf(
			"substrate: row body claims a %d-byte org id but is only %d bytes", n, len(b))
	}
	orgID = string(b[2 : 2+n])
	flag := b[2+n]
	if flag > 1 {
		return "", false, nil, fmt.Errorf("substrate: row body deleted flag is %d, not 0 or 1", flag)
	}
	return orgID, flag == 1, json.RawMessage(b[3+n:]), nil
}

// stamp renders an op's HLC as fixed-width bytes: wall(8 BE) ‖ counter(4 BE) ‖
// author(32 raw). Every field fixed-width, so no length prefix is needed and the
// prefix of a stamped element is unambiguously the stamp.
func stamp(ms int64, counter uint32, authorHex string) ([]byte, error) {
	author, err := hex.DecodeString(authorHex)
	if err != nil || len(author) != 32 {
		return nil, fmt.Errorf(
			"substrate: op author %q is not a 32-byte Ed25519 public key in hex", authorHex)
	}
	if ms < 0 {
		return nil, fmt.Errorf("substrate: op wall clock is negative (%d)", ms)
	}
	b := make([]byte, 0, stampLen)
	var wall [8]byte
	binary.BigEndian.PutUint64(wall[:], uint64(ms))
	b = append(b, wall[:]...)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], counter)
	b = append(b, c[:]...)
	return append(b, author...), nil
}

// splitStamped separates a stamped set element back into its stamp and its body,
// and returns the stamp in Pango's own spelling so a caller can compare it
// against the op's HLC.
func splitStamped(raw []byte) (hlcStamp string, body []byte, err error) {
	if len(raw) < stampLen {
		return "", nil, fmt.Errorf(
			"substrate: set element is %d bytes, too short to carry its stamp", len(raw))
	}
	ms := binary.BigEndian.Uint64(raw[0:8])
	counter := binary.BigEndian.Uint32(raw[8:12])
	author := hex.EncodeToString(raw[12:stampLen])
	return store.FormatHLC(int64(ms), counter, author), raw[stampLen:], nil
}

// untagBytes reads a tagged byte string back out of an engine value. The binding
// offers kotvasync.Bytes to build one and no inverse, because the engine's
// contract is that the bytes are the semantics and it will not guess which of
// the four tags a caller meant.
func untagBytes(tagged json.RawMessage) ([]byte, error) {
	var v struct {
		Bstr *string `json:"bstr"`
	}
	if err := json.Unmarshal(tagged, &v); err != nil || v.Bstr == nil {
		return nil, fmt.Errorf("substrate: engine value is not a tagged byte string: %s", tagged)
	}
	b, err := hex.DecodeString(*v.Bstr)
	if err != nil {
		return nil, fmt.Errorf("substrate: engine value is not hex: %w", err)
	}
	return b, nil
}

// syncOp maps one Pango op onto the shared algebra.
//
// A table with no merge rule is refused rather than defaulted. The built-in
// engine can afford to ignore an op for a table it has never heard of (a newer
// peer's schema — sync/apply.go says so and drops it), because ignoring it
// changes nothing. Minting one into the engine cannot be undone, so an
// unclassified table fails closed here.
func syncOp(op store.Op, k kinds, ns string) (kotvasync.Op, error) {
	tbl, rowID := op.Tbl, op.RowID
	if tbl == "" || rowID == "" {
		return kotvasync.Op{}, fmt.Errorf(
			"substrate: op has no table or row id (tbl=%q row=%q)", tbl, rowID)
	}
	ms, counter, author, ok := store.ParseHLC(op.HLC)
	if !ok {
		return kotvasync.Op{}, fmt.Errorf("substrate: op stamp %q is not a well-formed HLC", op.HLC)
	}
	if author != op.Author {
		return kotvasync.Op{}, fmt.Errorf(
			"substrate: op claims author %s but its stamp names %s", op.Author, author)
	}
	if len(author) != authorHexLen {
		return kotvasync.Op{}, fmt.Errorf(
			"substrate: op author %q is not a 32-byte public key in hex", author)
	}
	body, err := rowBody(op)
	if err != nil {
		return kotvasync.Op{}, err
	}
	hlc := kotvasync.HLC{Wall: uint64(ms), Counter: counter, Author: author}

	switch propsync.ClassOf(tbl) {
	case propsync.ClassRegister:
		field := registerField
		return kotvasync.Op{
			Kind:   k.lwwSet,
			NS:     ns,
			Target: targetOf(tbl, rowID),
			Field:  &field,
			Value:  kotvasync.Bytes(body),
			HLC:    hlc,
		}, nil
	case propsync.ClassUnion:
		st, err := stamp(ms, counter, author)
		if err != nil {
			return kotvasync.Op{}, err
		}
		return kotvasync.Op{
			Kind:   k.setAdd,
			NS:     ns,
			Target: targetOf(tbl, rowID),
			Value:  kotvasync.Bytes(append(st, body...)),
			HLC:    hlc,
		}, nil
	default:
		return kotvasync.Op{}, fmt.Errorf(
			"substrate: table %q has no merge rule in internal/sync/classes.go, so there is no "+
				"ownership class to mint it into", tbl)
	}
}

// fromSyncOp is syncOp's inverse: it reads a Pango op back out of an engine
// op, so an envelope arriving from a peer can be checked against the op record
// it claims to describe rather than trusted to agree with it.
func fromSyncOp(sop kotvasync.Op, k kinds, ns string) (store.Op, error) {
	if sop.NS != ns {
		return store.Op{}, fmt.Errorf("substrate: op is in namespace %q, this node is in %q",
			sop.NS, ns)
	}
	tbl, rowID, err := splitTarget(sop.Target)
	if err != nil {
		return store.Op{}, err
	}
	if len(sop.HLC.Author) != authorHexLen {
		return store.Op{}, fmt.Errorf(
			"substrate: op author %q is not a 32-byte public key in hex", sop.HLC.Author)
	}
	if sop.HLC.Wall > uint64(store.MaxWallMS) {
		return store.Op{}, fmt.Errorf("substrate: op wall clock %d is out of Pango's stamp width",
			sop.HLC.Wall)
	}
	hlc := store.FormatHLC(int64(sop.HLC.Wall), sop.HLC.Counter, sop.HLC.Author)

	raw, err := untagBytes(sop.Value)
	if err != nil {
		return store.Op{}, err
	}

	class := propsync.ClassOf(tbl)
	switch {
	case class == propsync.ClassRegister && sop.Kind == k.lwwSet:
		if sop.Field == nil || *sop.Field != registerField {
			return store.Op{}, fmt.Errorf(
				"substrate: register op for %s writes field %v, not %q",
				sop.Target, sop.Field, registerField)
		}
	case class == propsync.ClassUnion && sop.Kind == k.setAdd:
		var elemStamp string
		elemStamp, raw, err = splitStamped(raw)
		if err != nil {
			return store.Op{}, err
		}
		// The element's stamp and the op's own HLC are two recordings of one
		// fact. A disagreement means the element identity the engine deduped on
		// is not the identity Pango's oplog deduped on, which is exactly the
		// divergence the stamp exists to prevent — so it is refused, not
		// tolerated.
		if elemStamp != hlc {
			return store.Op{}, fmt.Errorf(
				"substrate: op %s stamps its element %s but claims %s", sop.Target, elemStamp, hlc)
		}
	default:
		return store.Op{}, fmt.Errorf(
			"substrate: op for %s has kind %d, which is not the %s class table %q belongs to",
			sop.Target, sop.Kind, class, tbl)
	}

	orgID, deleted, payload, err := parseRowBody(raw)
	if err != nil {
		return store.Op{}, err
	}
	return store.Op{
		HLC:     hlc,
		Author:  sop.HLC.Author,
		OrgID:   orgID,
		Tbl:     tbl,
		RowID:   rowID,
		Deleted: deleted,
		Payload: payload,
	}, nil
}
