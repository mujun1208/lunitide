package people

// F-08: message-body end-to-end encryption using NaCl box (Curve25519 +
// XSalsa20-Poly1305), layered ON TOP OF the existing per-frame AES-256-GCM
// channel encryption. The box shared key is derived deterministically from the
// Ed25519 identity keys of both peers (this side via identity.X25519Shared,
// peer public key reused from Contact.PublicKey), so no extra key exchange is
// needed. If the peer public key is unavailable or derivation fails, senders
// fall back to plaintext Body and receivers accept plaintext, preserving
// backward compatibility with pre-F-08 peers.

import (
	"crypto/rand"
	"encoding/base64"

	"golang.org/x/crypto/nacl/box"
)

// bodyEncV is the message-body encryption scheme version carried on the wire.
const bodyEncV = 1

// sealBody encrypts plaintext for the peer identified by peerEd25519PubHex.
// Returns base64 ciphertext and base64 24-byte nonce. ok=false means the
// caller should fall back to sending plaintext Body (e.g. missing peer key).
func (s *Service) sealBody(peerEd25519PubHex, plaintext string) (cipherB64, nonceB64 string, ok bool) {
	if s == nil || s.identity == nil || peerEd25519PubHex == "" {
		return "", "", false
	}
	shared, err := s.identity.X25519Shared(peerEd25519PubHex)
	if err != nil || shared == nil {
		return "", "", false
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", false
	}
	sealed := box.SealAfterPrecomputation(nil, []byte(plaintext), &nonce, shared)
	return base64.StdEncoding.EncodeToString(sealed),
		base64.StdEncoding.EncodeToString(nonce[:]), true
}

// openBody decrypts a box-sealed body from the peer identified by
// peerEd25519PubHex. ok=false means authentication/decryption failed and the
// message must be discarded (tamper protection).
func (s *Service) openBody(peerEd25519PubHex, cipherB64, nonceB64 string) (plaintext string, ok bool) {
	if s == nil || s.identity == nil || peerEd25519PubHex == "" {
		return "", false
	}
	sealed, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return "", false
	}
	rawNonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil || len(rawNonce) != 24 {
		return "", false
	}
	shared, err := s.identity.X25519Shared(peerEd25519PubHex)
	if err != nil || shared == nil {
		return "", false
	}
	var nonce [24]byte
	copy(nonce[:], rawNonce)
	opened, ok := box.OpenAfterPrecomputation(nil, sealed, &nonce, shared)
	if !ok {
		return "", false
	}
	return string(opened), true
}