package wrap

// The WRAP object model: the common header, content addressing and signing
// (WRAP 02-objects.md §3.1–3.2, 03-wire-format.md §4.2–4.3, 04-signing.md
// §5.1–5.4).

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"lukechampine.com/blake3"
)

// Errors from decoding and verifying an object (04-signing.md §5.4, §12).
var (
	// ErrUnsupportedVersion means the object's `v` (key 1) is not one this
	// package implements. §4.2 requires the object be ignored rather than
	// best-effort interpreted.
	ErrUnsupportedVersion = errors.New("wrap: unsupported format version")
	// ErrBadSignature means the Ed25519 signature does not verify against
	// the claimed author (§5.4 step 5 / ERR_BAD_SIG).
	ErrBadSignature = errors.New("wrap: signature does not verify")
	// ErrNotIssuer means an Assignment's author is not the WorkOrder's
	// author (§3.6, §5.5 / ERR_NOT_ISSUER): the one authorship rule this
	// package enforces outside of Decode, since it needs the referenced
	// WorkOrder for context a bare decode does not have.
	ErrNotIssuer = errors.New("wrap: assignment author is not the work order's issuer")
	// ErrTooLarge means the envelope exceeds maxEnvelopeSize
	// (03-wire-format.md §4.6 / ERR_TOO_LARGE, 0x0105). Work orders describe
	// work; they do not carry payloads, so an oversized envelope is treated
	// as attacker-supplied rather than a legitimate large object — this is
	// checked first in Decode, before any CBOR parsing, so a caller does not
	// pay the cost of decoding an envelope it is going to reject anyway.
	ErrTooLarge = errors.New("wrap: envelope exceeds maximum size")
)

// maxEnvelopeSize is the size floor 03-wire-format.md §4.6 sets: "An
// implementation MUST reject any WRAP object larger than 65 536 bytes with
// ERR_TOO_LARGE." Checked against the whole envelope Decode receives
// (canonical_bytes plus its detached signature and CBOR array framing) rather
// than canonical_bytes alone, so the check runs before any CBOR parsing at
// all — the point of a size floor is to bound the cost of handling untrusted
// input, and that cost starts at parse time, not once the object is unwrapped.
// The signature plus framing add on the order of 70 bytes on top of
// canonical_bytes, so an object within ~70 bytes of the limit could in theory
// be rejected a step earlier than a canonical_bytes-only reading of §4.6 would
// place it; nothing in this codebase constructs objects anywhere near that
// boundary on purpose.
const maxEnvelopeSize = 65536

// Kind identifies one of WRAP's six object kinds (02-objects.md §3.1). Kinds
// 0x40-0x7f are reserved for profile-specific objects and 0x80+ for future
// core use; an implementation encountering a kind it does not recognise MUST
// ignore it silently (03-wire-format.md §4.4) — see Decode.
type Kind uint64

const (
	KindWorkOrder   Kind = 0x01
	KindOffer       Kind = 0x02
	KindBid         Kind = 0x03
	KindAssignment  Kind = 0x04
	KindProgress    Kind = 0x05
	KindAttestation Kind = 0x06
)

func (k Kind) String() string {
	switch k {
	case KindWorkOrder:
		return "WorkOrder"
	case KindOffer:
		return "Offer"
	case KindBid:
		return "Bid"
	case KindAssignment:
		return "Assignment"
	case KindProgress:
		return "Progress"
	case KindAttestation:
		return "Attestation"
	default:
		return fmt.Sprintf("Kind(0x%02x)", uint64(k))
	}
}

// FormatVersion is the WRAP wire format version this package implements
// (03-wire-format.md §4.2). An object carrying a different version MUST be
// ignored, not best-effort interpreted (§4.2) — see Decode.
const FormatVersion uint64 = 0

// Common header keys (02-objects.md §3.2). Key 0 is forbidden
// (03-wire-format.md §4.5); key 3 (id) is never itself encoded — it is
// derived from, and verified against, the canonical bytes of everything
// else (§4.3).
const (
	keyV      uint64 = 1
	keyKind   uint64 = 2
	keyID     uint64 = 3
	keyAuthor uint64 = 4
	keyTS     uint64 = 5
)

// idMultihashPrefix identifies BLAKE3-256 in the multihash-style id
// (03-wire-format.md §4.3: id = 0x1e ‖ BLAKE3-256(canonical_bytes)).
const idMultihashPrefix = 0x1e

// preimageTag domain-separates a WRAP object signature from any other
// protocol that happens to sign raw CBOR (04-signing.md §5.2).
const preimageTag = "WRAP-v0/object"

// Object is one signed, content-addressed WRAP object: the common header
// plus kind-specific fields (keys 6 and above).
type Object struct {
	Kind   Kind
	Author [32]byte // Ed25519 public key of the signer
	TS     string   // hybrid logical clock stamp (06-merge.md §7.2)
	Fields M        // kind-specific fields (keys 6+); may be nil

	// ID and Sig are derived, not transmitted as part of the object map
	// (§4.3, §5.3): id is recomputed by every receiver from canonical_bytes,
	// and the signature travels alongside canonical_bytes in the envelope,
	// never inside it. They are populated by Sign and by Decode/Verify.
	ID  []byte
	Sig []byte
}

// canonicalMap builds the CBOR map to encode: the common header (keys 1,2,4,5)
// plus Fields (keys 6+), explicitly excluding key 3 (id) and never including
// a signature — canonical_bytes is exactly this (§4.3, §5.2).
func (o *Object) canonicalMap() M {
	m := M{
		keyV:      FormatVersion,
		keyKind:   uint64(o.Kind),
		keyAuthor: append([]byte(nil), o.Author[:]...),
		keyTS:     o.TS,
	}
	for k, v := range o.Fields {
		m[k] = v
	}
	return m
}

// CanonicalBytes returns the deterministic CBOR encoding that both the
// object id and its signature are computed over.
func (o *Object) CanonicalBytes() ([]byte, error) {
	return Encode(o.canonicalMap())
}

// ComputeID returns the content address of canonicalBytes (§4.3).
func ComputeID(canonicalBytes []byte) []byte {
	sum := blake3.Sum256(canonicalBytes)
	id := make([]byte, 0, 1+len(sum))
	id = append(id, idMultihashPrefix)
	id = append(id, sum[:]...)
	return id
}

func preimage(canonicalBytes []byte) []byte {
	p := make([]byte, 0, len(preimageTag)+1+len(canonicalBytes))
	p = append(p, preimageTag...)
	p = append(p, 0x00)
	p = append(p, canonicalBytes...)
	return p
}

// Sign computes canonical_bytes, derives ID, and signs the object with priv,
// setting o.Author from priv's public key. It fails if priv's public key does
// not match an already-set non-zero o.Author (a caller must not silently sign
// as someone else).
func (o *Object) Sign(priv ed25519.PrivateKey) error {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return errors.New("wrap: sign: invalid Ed25519 private key")
	}
	var zero [32]byte
	if o.Author != zero && [32]byte(pub) != o.Author {
		return errors.New("wrap: sign: priv does not match o.Author")
	}
	copy(o.Author[:], pub)

	canon, err := o.CanonicalBytes()
	if err != nil {
		return err
	}
	o.ID = ComputeID(canon)
	o.Sig = ed25519.Sign(priv, preimage(canon))
	return nil
}

// Envelope is the two-element wire form WRAP transmits: canonical_bytes plus
// the detached signature (04-signing.md §5.3: `[canonical_bytes, signature]`).
func (o *Object) Envelope() ([]byte, error) {
	canon, err := o.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	if len(o.Sig) != ed25519.SignatureSize {
		return nil, errors.New("wrap: envelope: object is not signed")
	}
	return Encode([]any{canon, o.Sig})
}

// Decode parses and verifies a signed envelope. It checks the §4.6 size floor
// first — before canonical decoding, key-0 rejection (handled by the CBOR
// layer), format version, id recomputation, and signature, which is the order
// 04-signing.md §5.4 requires for those five — because the size floor exists
// to bound the cost of handling untrusted input, which only holds if it is
// checked before that input is parsed at all. It does NOT apply the per-kind
// authorship rule (§5.5) — that needs context (e.g. the WorkOrder an
// Assignment refers to) that a bare decode does not have; see
// VerifyAssignmentAuthor.
//
// An unrecognised format version, per §4.2, causes the object to be ignored
// (returns ErrUnsupportedVersion) rather than best-effort interpreted. An
// unrecognised Kind is returned successfully — WRAP requires it be ignored
// silently at the point a caller dispatches on kind, not rejected here
// (§4.4); ignore Kind values this package does not know how to render.
func Decode(envelope []byte) (*Object, error) {
	if len(envelope) > maxEnvelopeSize {
		return nil, ErrTooLarge
	}
	v, err := DecodeCBOR(envelope)
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return nil, errors.New("wrap: decode: envelope must be a 2-element array")
	}
	canon, ok := arr[0].([]byte)
	if !ok {
		return nil, errors.New("wrap: decode: envelope[0] (canonical_bytes) must be a byte string")
	}
	sig, ok := arr[1].([]byte)
	if !ok || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("wrap: decode: envelope[1] (signature) must be a 64-byte string")
	}

	objVal, err := DecodeCBOR(canon)
	if err != nil {
		return nil, fmt.Errorf("wrap: decode: canonical_bytes: %w", err)
	}
	m, ok := objVal.(map[any]any)
	if !ok {
		return nil, errors.New("wrap: decode: object is not a map")
	}
	if _, present := m[uint64(0)]; present {
		return nil, ErrForbiddenKey
	}

	v1, ok := getUint(m, keyV)
	if !ok {
		return nil, errors.New("wrap: decode: missing v (key 1)")
	}
	if v1 != FormatVersion {
		return nil, ErrUnsupportedVersion
	}
	kindU, ok := getUint(m, keyKind)
	if !ok {
		return nil, errors.New("wrap: decode: missing kind (key 2)")
	}
	author, ok := getBytes(m, keyAuthor)
	if !ok || len(author) != ed25519.PublicKeySize {
		return nil, errors.New("wrap: decode: missing or malformed author (key 4)")
	}
	ts, ok := getString(m, keyTS)
	if !ok {
		return nil, errors.New("wrap: decode: missing ts (key 5)")
	}

	o := &Object{Kind: Kind(kindU), TS: ts, Fields: M{}}
	copy(o.Author[:], author)
	for k, val := range m {
		ku, ok := k.(uint64)
		if !ok || ku < 6 {
			continue // common header field, or a non-uint key (never valid here)
		}
		o.Fields[ku] = val
	}

	gotID := ComputeID(canon)
	o.ID = gotID
	o.Sig = sig

	if !ed25519.Verify(ed25519.PublicKey(author), preimage(canon), sig) {
		return nil, ErrBadSignature
	}
	return o, nil
}
