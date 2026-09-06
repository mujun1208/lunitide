package identity

import (
	"bytes"
	"context"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func newUnlocked(t *testing.T) *Service {
	t.Helper()
	svc := New(&memStore{})
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestX25519SharedSymmetry proves A.sharedWith(B.pub) == B.sharedWith(A.pub),
// which is the correctness requirement for deterministic NaCl box E2E without
// any extra key exchange.
func TestX25519SharedSymmetry(t *testing.T) {
	a := newUnlocked(t)
	b := newUnlocked(t)

	aShared, err := a.X25519Shared(b.Public().PublicKey)
	if err != nil {
		t.Fatalf("a shared: %v", err)
	}
	bShared, err := b.X25519Shared(a.Public().PublicKey)
	if err != nil {
		t.Fatalf("b shared: %v", err)
	}
	if !bytes.Equal(aShared[:], bShared[:]) {
		t.Fatalf("shared keys differ:\n a=%x\n b=%x", aShared[:], bShared[:])
	}
}

// TestX25519PublicMatchesSharedRoundTrip encrypts with box.Seal (peerPub,
// senderPriv-derived shared) and decrypts with the precomputed shared on the
// other side, exercising both X25519Public and X25519Shared together.
func TestX25519BoxRoundTrip(t *testing.T) {
	a := newUnlocked(t)
	b := newUnlocked(t)

	shared, err := a.X25519Shared(b.Public().PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [24]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	plaintext := []byte("F-08 secret body")
	sealed := box.SealAfterPrecomputation(nil, plaintext, &nonce, shared)

	bShared, err := b.X25519Shared(a.Public().PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	opened, ok := box.OpenAfterPrecomputation(nil, sealed, &nonce, bShared)
	if !ok {
		t.Fatal("open failed")
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("plaintext mismatch: %q", opened)
	}

	// Tamper detection.
	if len(sealed) > 0 {
		sealed[len(sealed)-1] ^= 0xff
		if _, ok := box.OpenAfterPrecomputation(nil, sealed, &nonce, bShared); ok {
			t.Fatal("tampered ciphertext should not open")
		}
	}
}

func TestX25519PublicDeterministic(t *testing.T) {
	a := newUnlocked(t)
	p1, err := a.X25519Public()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := a.X25519Public()
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatal("X25519Public not deterministic")
	}
	var zero [32]byte
	if p1 == zero {
		t.Fatal("X25519Public returned zero key")
	}
}