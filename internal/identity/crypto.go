package identity

// F-08: controlled Curve25519 derivation from the Ed25519 identity key, for
// NaCl box (Curve25519 + XSalsa20-Poly1305) end-to-end message encryption in
// the People module. Private key bytes never leave this package (S-01 sealing
// discipline): callers receive only the precomputed box shared key.

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"strings"

	"filippo.io/edwards25519"
	"golang.org/x/crypto/nacl/box"
)

// X25519Public returns this identity's Curve25519 public key, converted
// deterministically from the Ed25519 identity public key (Edwards point ->
// Montgomery u-coordinate).
func (s *Service) X25519Public() ([32]byte, error) {
	var out [32]byte
	if s == nil {
		return out, ErrUnavailable
	}
	s.mu.Lock()
	pubHex := s.rec.PublicKey
	s.mu.Unlock()
	return ed25519PublicToX25519(pubHex)
}

// X25519Shared derives the NaCl box shared key between this identity's
// Curve25519 private key and the peer's Curve25519 public key (converted from
// the peer's Ed25519 identity key hex). The result is symmetric:
// A.X25519Shared(B.pub) == B.X25519Shared(A.pub), and is directly usable with
// box.SealAfterPrecomputation / box.OpenAfterPrecomputation. The private
// scalar is zeroed before returning.
func (s *Service) X25519Shared(peerEd25519PubHex string) (*[32]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	privHex := s.rec.PrivateKey
	s.mu.Unlock()
	var priv [32]byte
	if err := ed25519PrivateToX25519(privHex, &priv); err != nil {
		return nil, err
	}
	defer zero32(&priv)
	peerPub, err := ed25519PublicToX25519(peerEd25519PubHex)
	if err != nil {
		return nil, err
	}
	shared := new([32]byte)
	box.Precompute(shared, &peerPub, &priv)
	return shared, nil
}

// ed25519PublicToX25519 converts an Ed25519 public key (hex) to its
// Curve25519 (Montgomery) public key using filippo.io/edwards25519.
func ed25519PublicToX25519(pubHex string) ([32]byte, error) {
	var out [32]byte
	raw, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(pubHex)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return out, ErrUnavailable
	}
	p, err := new(edwards25519.Point).SetBytes(raw)
	if err != nil {
		return out, err
	}
	copy(out[:], p.BytesMontgomery())
	return out, nil
}

// ed25519PrivateToX25519 converts an Ed25519 private key (hex, 64 bytes =
// seed||pub) to its Curve25519 private scalar: SHA512(seed)[:32] with the
// standard clamping. The transient SHA512 buffer is zeroed.
func ed25519PrivateToX25519(privHex string, out *[32]byte) error {
	raw, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(privHex)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return ErrUnavailable
	}
	h := sha512.Sum512(raw[:32]) // raw[:32] is the Ed25519 seed
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	copy(out[:], h[:32])
	for i := range h {
		h[i] = 0
	}
	return nil
}

func zero32(b *[32]byte) {
	for i := range b {
		b[i] = 0
	}
}