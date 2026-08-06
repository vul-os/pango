package wrap

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
)

func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// tsCounter hands out distinct HLC-shaped stamps within a test process, so
// two objects built moments apart in the same test are never accidentally
// identical (and therefore never accidentally content-addressed to the same
// id) purely because this helper reused static test data.
var tsCounter uint64

func nextTS() string {
	tsCounter++
	return fmt.Sprintf("178450000%04d-0000-abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", tsCounter)
}

func testWorkOrder(t *testing.T, issuer [32]byte) *Object {
	t.Helper()
	job := domain.Job{
		ID:          "01JOB",
		OrgID:       "01ORG",
		BuildingID:  "01BLD",
		Title:       "Leak in unit 4B",
		Description: "Tenant reports water under the sink",
		Category:    "plumbing",
	}
	building := domain.Building{ID: "01BLD", Name: "Riverside Court"}
	lat, lon := -33.9, 18.4
	building.Lat, building.Lon = &lat, &lon

	return JobToWorkOrder(job, building, "4B", issuer, JobToWorkOrderOptions{
		TS:      nextTS(),
		Expires: 2000000000,
		Licence: "za:pirb",
	})
}

// TestIDRecomputationMatches: the id computed at signing time and the id a
// receiver recomputes from the wire bytes must be identical — that is the
// whole point of content addressing (03-wire-format.md §4.3).
func TestIDRecomputationMatches(t *testing.T) {
	pub, priv := genKey(t)
	var author [32]byte
	copy(author[:], pub)

	wo := testWorkOrder(t, author)
	if err := wo.Sign(priv); err != nil {
		t.Fatal(err)
	}
	mintedID := append([]byte(nil), wo.ID...)

	env, err := wo.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(env)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ID, mintedID) {
		t.Fatalf("recomputed id %x != minted id %x", got.ID, mintedID)
	}
	// And the id must be exactly 0x1e || BLAKE3-256(canonical_bytes) — a
	// 33-byte value with the multihash prefix as its first byte.
	if len(mintedID) != 33 || mintedID[0] != 0x1e {
		t.Fatalf("id has the wrong shape: %d bytes, first byte 0x%02x", len(mintedID), mintedID[0])
	}
}

// TestSignatureVerifyPass: a correctly signed envelope decodes and verifies.
func TestSignatureVerifyPass(t *testing.T) {
	pub, priv := genKey(t)
	var author [32]byte
	copy(author[:], pub)

	wo := testWorkOrder(t, author)
	if err := wo.Sign(priv); err != nil {
		t.Fatal(err)
	}
	env, err := wo.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(env)
	if err != nil {
		t.Fatalf("Decode of a validly signed envelope failed: %v", err)
	}
	if got.Kind != KindWorkOrder {
		t.Errorf("kind = %v, want WorkOrder", got.Kind)
	}
}

// TestSignatureVerifyFail: flipping a byte in the signature must be
// detected, not silently accepted.
func TestSignatureVerifyFail(t *testing.T) {
	pub, priv := genKey(t)
	var author [32]byte
	copy(author[:], pub)

	wo := testWorkOrder(t, author)
	if err := wo.Sign(priv); err != nil {
		t.Fatal(err)
	}
	env, err := wo.Envelope()
	if err != nil {
		t.Fatal(err)
	}

	// The envelope is [canonical_bytes, sig] as a CBOR array; the last 64
	// bytes are the raw signature (bstr header + 64-byte payload, and the
	// payload is what we want to corrupt).
	tampered := append([]byte(nil), env...)
	tampered[len(tampered)-1] ^= 0xff

	if _, err := Decode(tampered); err != ErrBadSignature {
		t.Fatalf("got %v, want ErrBadSignature", err)
	}
}

// TestSignatureVerifyFailWrongKey: a signature that verifies fine against
// its OWN signer must still fail once the object claims a different author,
// since the preimage — and therefore the signature — covers the author
// field itself.
func TestSignatureVerifyFailWrongKey(t *testing.T) {
	_, priv := genKey(t)
	otherPub, _ := genKey(t)
	var wrongAuthor [32]byte
	copy(wrongAuthor[:], otherPub)

	wo := testWorkOrder(t, wrongAuthor)
	// Sign with a DIFFERENT key than the claimed author — Sign refuses this
	// directly, so build the forgery by hand instead, the way an attacker
	// tampering with a captured object would have to.
	canon, err := wo.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, preimage(canon))
	env, err := Encode([]any{canon, sig})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(env); err != ErrBadSignature {
		t.Fatalf("got %v, want ErrBadSignature (signature does not match the claimed author)", err)
	}
}

// TestNonIssuerAssignmentRejected is the authorship rule the task calls out
// explicitly (02-objects.md §3.6, 04-signing.md §5.5): an Assignment MUST be
// rejected unless its author is the WorkOrder's own author.
func TestNonIssuerAssignmentRejected(t *testing.T) {
	issuerPub, issuerPriv := genKey(t)
	performerPub, performerPriv := genKey(t)
	imposterPub, imposterPriv := genKey(t)
	var issuer, performer, imposter [32]byte
	copy(issuer[:], issuerPub)
	copy(performer[:], performerPub)
	copy(imposter[:], imposterPub)

	wo := testWorkOrder(t, issuer)
	if err := wo.Sign(issuerPriv); err != nil {
		t.Fatal(err)
	}

	// The legitimate case: the issuer assigns to the performer.
	good := Assignment{Order: wo.ID, Performer: performer}
	goodObj := good.ToObject(issuer, nextTS())
	if err := goodObj.Sign(issuerPriv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssignmentAuthor(goodObj, wo); err != nil {
		t.Fatalf("legitimate issuer-authored assignment was rejected: %v", err)
	}

	// The attack: a performer (or any third party) signs their own
	// Assignment for the same work order, trying to assign the job to
	// themselves.
	forged := Assignment{Order: wo.ID, Performer: performer}
	forgedObj := forged.ToObject(performer, nextTS())
	if err := forgedObj.Sign(performerPriv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssignmentAuthor(forgedObj, wo); err != ErrNotIssuer {
		t.Fatalf("got %v, want ErrNotIssuer for a performer-authored assignment", err)
	}

	// An unrelated third party doesn't fare any better.
	imposterObj := Assignment{Order: wo.ID, Performer: performer}.ToObject(imposter, nextTS())
	if err := imposterObj.Sign(imposterPriv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssignmentAuthor(imposterObj, wo); err != ErrNotIssuer {
		t.Fatalf("got %v, want ErrNotIssuer for a third-party-authored assignment", err)
	}
}

// TestAssignmentOrderMismatchRejected: VerifyAssignmentAuthor alone is not
// enough — an Assignment must also refer to THIS work order, or a
// same-issuer Assignment for an unrelated order would incorrectly pass.
func TestAssignmentOrderMismatchRejected(t *testing.T) {
	issuerPub, issuerPriv := genKey(t)
	var issuer [32]byte
	copy(issuer[:], issuerPub)

	woA := testWorkOrder(t, issuer)
	if err := woA.Sign(issuerPriv); err != nil {
		t.Fatal(err)
	}
	woB := testWorkOrder(t, issuer) // a different work order, same issuer
	if err := woB.Sign(issuerPriv); err != nil {
		t.Fatal(err)
	}

	assignment := Assignment{Order: woB.ID}.ToObject(issuer, nextTS()) // refers to B
	if err := assignment.Sign(issuerPriv); err != nil {
		t.Fatal(err)
	}

	if err := VerifyAssignmentAuthor(assignment, woA); err != nil {
		t.Fatalf("author check alone should pass (same issuer): %v", err)
	}
	if err := VerifyAssignmentOrder(assignment, woA); err == nil {
		t.Fatal("expected VerifyAssignmentOrder to reject an assignment referring to a different work order")
	}
	if err := VerifyAssignmentOrder(assignment, woB); err != nil {
		t.Fatalf("assignment genuinely refers to woB: %v", err)
	}
}

// TestUnknownKindIgnoredSilently: an object of a kind this package has no
// typed model for MUST still decode and verify successfully — WRAP requires
// unknown kinds be ignored silently by whatever dispatches on Kind, not
// rejected at the decode/signature layer (03-wire-format.md §4.4).
func TestUnknownKindIgnoredSilently(t *testing.T) {
	pub, priv := genKey(t)
	var author [32]byte
	copy(author[:], pub)

	o := &Object{Kind: Kind(0x55), Author: author, TS: "1784500000000-0000-abc", Fields: M{6: "some future object"}}
	if err := o.Sign(priv); err != nil {
		t.Fatal(err)
	}
	env, err := o.Envelope()
	if err != nil {
		t.Fatal(err)
	}

	got, err := Decode(env)
	if err != nil {
		t.Fatalf("decoding an object of an unrecognised kind must still succeed: %v", err)
	}
	if got.Kind != Kind(0x55) {
		t.Fatalf("kind = %v, want 0x55", got.Kind)
	}

	// A dispatcher over the known kinds ignores it silently: no branch
	// matches, nothing errors, nothing is even logged as a rejection.
	switch got.Kind {
	case KindWorkOrder, KindOffer, KindBid, KindAssignment, KindProgress, KindAttestation:
		t.Fatalf("kind 0x55 unexpectedly matched a known kind")
	default:
		// silently ignored — exactly the required behaviour
	}
}

// TestUnsupportedVersionIgnored: §4.2 requires an object whose v a receiver
// does not support be ignored rather than best-effort interpreted.
func TestUnsupportedVersionIgnored(t *testing.T) {
	pub, priv := genKey(t)
	var author [32]byte
	copy(author[:], pub)

	o := &Object{Kind: KindWorkOrder, Author: author, TS: "1784500000000-0000-abc", Fields: M{6: "trades/v1", 7: "future format"}}
	canon := M{keyV: uint64(1), keyKind: uint64(o.Kind), keyAuthor: append([]byte(nil), o.Author[:]...), keyTS: o.TS}
	for k, v := range o.Fields {
		canon[k] = v
	}
	bytesv, err := Encode(canon)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, preimage(bytesv))
	env, err := Encode([]any{bytesv, sig})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Decode(env); err != ErrUnsupportedVersion {
		t.Fatalf("got %v, want ErrUnsupportedVersion", err)
	}
}

// TestJobWorkOrderRoundTrip: mapping a job to a WorkOrder and back recovers
// what the profile actually carries.
func TestJobWorkOrderRoundTrip(t *testing.T) {
	pub, priv := genKey(t)
	var issuer [32]byte
	copy(issuer[:], pub)

	job := domain.Job{
		ID:          "01JOBABC",
		OrgID:       "01ORGXYZ",
		BuildingID:  "01BLDQRS",
		Title:       "Leak in unit 4B",
		Description: "Tenant reports water under the sink",
		Category:    "plumbing",
	}
	building := domain.Building{ID: "01BLDQRS", Name: "Riverside Court", Address: "12 River Rd"}
	lat, lon := -33.9, 18.4
	building.Lat, building.Lon = &lat, &lon

	wo := JobToWorkOrder(job, building, "4B", issuer, JobToWorkOrderOptions{Expires: 2000000000})
	if err := wo.Sign(priv); err != nil {
		t.Fatal(err)
	}
	env, err := wo.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(env)
	if err != nil {
		t.Fatal(err)
	}

	back, parsedWO, err := JobFromWorkOrder(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != job.Title {
		t.Errorf("title = %q, want %q", back.Title, job.Title)
	}
	if back.Description != job.Description {
		t.Errorf("description = %q, want %q", back.Description, job.Description)
	}
	if back.Category != job.Category {
		t.Errorf("category = %q, want %q", back.Category, job.Category)
	}
	if back.ID != job.ID {
		t.Errorf("recovered job id = %q, want %q (via refs)", back.ID, job.ID)
	}
	if parsedWO.Profile != ProfileTrades {
		t.Errorf("profile = %q, want %q", parsedWO.Profile, ProfileTrades)
	}
	if len(parsedWO.Places) != 1 || parsedWO.Places[0].Role != "site" {
		t.Fatalf("places = %+v, want one Place with role=site", parsedWO.Places)
	}
	if parsedWO.Places[0].Lat == nil || *parsedWO.Places[0].Lat != lat {
		t.Errorf("place lat = %v, want %v", parsedWO.Places[0].Lat, lat)
	}
	if parsedWO.Refs[RefBuildingID] != job.BuildingID {
		t.Errorf("refs[building_id] = %q, want %q", parsedWO.Refs[RefBuildingID], job.BuildingID)
	}
}

// specMaxEnvelopeSize is 03-wire-format.md §4.6's own literal — "MUST reject
// any WRAP object larger than 65 536 bytes" — spelled out independently of
// object.go's maxEnvelopeSize constant. TestDecodeSizeFloor below builds its
// boundary envelopes from this literal, not from maxEnvelopeSize, and
// separately asserts the two agree: a test that measured the boundary
// relative to the very constant under test would stay green even if that
// constant drifted away from the spec (e.g. someone "fixes" a false-positive
// rejection by loosening the limit instead of the actual bug) — only a
// hardcoded, independent expected value catches that.
const specMaxEnvelopeSize = 65536

// TestDecodeSizeFloor pins the §4.6 / ERR_TOO_LARGE size floor Decode
// enforces at its exact boundary rather than at some comfortably-oversized
// value: an off-by-one on a "MUST reject any object larger than N" rule is
// the classic bug, and only a test built at N itself, not N+lots, can catch
// admitting N+1 or rejecting N.
//
// This is a plain committed unit test with no dependency on the WRAP
// conformance vector corpus (see vectors_test.go's package doc) — that
// corpus's own reject-oversize case is unreachable from a pango-only
// checkout, and the upstream vectors this repo can actually load carry no
// vector with that id at all, so nothing else in this package would notice
// if the size floor regressed.
func TestDecodeSizeFloor(t *testing.T) {
	if maxEnvelopeSize != specMaxEnvelopeSize {
		t.Fatalf("maxEnvelopeSize = %d, want %d per 03-wire-format.md §4.6", maxEnvelopeSize, specMaxEnvelopeSize)
	}

	t.Run("exactly at the limit is not rejected for size", func(t *testing.T) {
		env := make([]byte, specMaxEnvelopeSize)
		_, err := Decode(env)
		if errors.Is(err, ErrTooLarge) {
			t.Fatalf("a %d-byte envelope (exactly the §4.6 limit) was rejected as ErrTooLarge; the floor must admit the limit itself, not just values strictly below it", len(env))
		}
	})

	t.Run("one byte over the limit is rejected", func(t *testing.T) {
		env := make([]byte, specMaxEnvelopeSize+1)
		_, err := Decode(env)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("a %d-byte envelope (§4.6 limit + 1) got %v, want ErrTooLarge", len(env), err)
		}
	})

	t.Run("a normal small envelope still decodes", func(t *testing.T) {
		pub, priv := genKey(t)
		var author [32]byte
		copy(author[:], pub)

		wo := testWorkOrder(t, author)
		if err := wo.Sign(priv); err != nil {
			t.Fatal(err)
		}
		env, err := wo.Envelope()
		if err != nil {
			t.Fatal(err)
		}
		if len(env) >= specMaxEnvelopeSize {
			t.Fatalf("test envelope is %d bytes, expected well under the §4.6 limit so this actually exercises the happy path, not the boundary", len(env))
		}
		if _, err := Decode(env); err != nil {
			t.Fatalf("a small, validly signed envelope must decode: %v", err)
		}
	})
}
