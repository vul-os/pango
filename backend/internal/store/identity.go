package store

// Per-node identity. Every node generates an Ed25519 keypair on first run and
// keeps the seed in settings.
//
// The public key is not decoration: it is the HLC tie-break field (§7), so a
// node's identity and its position in the total order are the same value. That
// removes the failure mode where a node is renamed, or restored from a backup
// under a new node id, and its historical writes reorder themselves against
// everyone else's.
//
// The seed never leaves this package. Signing is offered as a service
// (CryptoSigner) rather than by handing out key material, so moving the key to
// an HSM or an agent later is a change here and at no call site.
//
// DUPLICATION: a near-copy of FlowStock's backend/internal/store/identity.go.
// Deliberate for now, tracked in docs/SYNC.md §11 ("Duplicated sync
// substrate"), which also records why the two may not import each other.

import (
	"crypto"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
)

// ErrCorruptIdentity means the stored key seed is unreadable. It is fatal on
// open rather than silently regenerated: a node that quietly mints a new
// identity would fork its own history and lose its enrolment with every peer.
var ErrCorruptIdentity = errors.New("stored node identity is corrupt")

// ensureIdentity loads this node's keypair from settings, generating one on a
// brand-new database. Called from Open.
func (s *Store) ensureIdentity() error {
	seedHex, err := s.getSetting("node_privkey")
	if err != nil {
		return err
	}
	if seedHex == "" {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
		pub := priv.Public().(ed25519.PublicKey)
		if err := s.SetSetting("node_privkey", hex.EncodeToString(priv.Seed())); err != nil {
			return err
		}
		if err := s.SetSetting("node_pubkey", hex.EncodeToString(pub)); err != nil {
			return err
		}
		s.priv, s.pub = priv, pub
		return nil
	}
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return ErrCorruptIdentity
	}
	s.priv = ed25519.NewKeyFromSeed(seed)
	s.pub = s.priv.Public().(ed25519.PublicKey)
	return nil
}

// PublicKeyHex is this node's Ed25519 public key, hex-encoded. It is the node's
// identity on the wire and the HLC tie-break value.
func (s *Store) PublicKeyHex() string {
	if s.pub == nil {
		return ""
	}
	return hex.EncodeToString(s.pub)
}

// Sign returns a hex Ed25519 signature over msg using this node's private key.
func (s *Store) Sign(msg []byte) string {
	if s.priv == nil {
		return ""
	}
	return hex.EncodeToString(ed25519.Sign(s.priv, msg))
}

// VerifySig checks a hex signature against a hex public key. An empty key or
// signature returns false — the caller cannot accidentally verify nothing
// against nothing and read it as success.
func VerifySig(pubHex string, msg []byte, sigHex string) bool {
	if pubHex == "" || sigHex == "" {
		return false
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// CryptoSigner exposes this node's identity as a crypto.Signer: a custodian
// that answers signature requests without surrendering the key. It is the shape
// the DMTAP-SYNC binding takes.
func (s *Store) CryptoSigner() (crypto.Signer, bool) {
	if s.priv == nil {
		return nil, false
	}
	return s.priv, true
}

// PrivateSeedHexForTest exposes the seed so tests can assert it never escapes
// through an API surface. Nothing in the product calls it, and it must stay
// that way.
func (s *Store) PrivateSeedHexForTest() string {
	if s.priv == nil {
		return ""
	}
	return hex.EncodeToString(s.priv.Seed())
}
