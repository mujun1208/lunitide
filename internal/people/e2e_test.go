package people

// F-08 unit tests for the message-body NaCl box helpers. These exercise the
// seal/open path directly against the identity derivation API without the TCP
// transport, covering round-trip, tamper rejection, and the plaintext
// fallback when the peer public key is unavailable.

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
)

type idMemStore struct {
	rec identity.Record
	ok  bool
}

func (m *idMemStore) LoadIdentity(context.Context) (identity.Record, bool, error) {
	return m.rec, m.ok, nil
}
func (m *idMemStore) InsertIdentity(_ context.Context, rec identity.Record) error {
	m.rec, m.ok = rec, true
	return nil
}
func (m *idMemStore) UpdateIdentity(_ context.Context, rec identity.Record) error {
	m.rec = rec
	return nil
}
func (m *idMemStore) RebindLegacySubject(context.Context, string, string) error { return nil }
func (m *idMemStore) UpsertSelfContact(context.Context, identity.Record) error  { return nil }

func newIdent(t *testing.T) *identity.Service {
	t.Helper()
	svc := identity.New(&idMemStore{})
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func svcWith(ident *identity.Service) *Service {
	return &Service{identity: ident}
}

func TestSealOpenBodyRoundTrip(t *testing.T) {
	identA := newIdent(t)
	identB := newIdent(t)
	a := svcWith(identA)
	b := svcWith(identB)

	plain := "端到端保密正文"
	cipherB64, nonceB64, ok := a.sealBody(identB.Public().PublicKey, plain)
	if !ok {
		t.Fatal("sealBody returned ok=false")
	}
	if cipherB64 == "" || nonceB64 == "" {
		t.Fatal("empty cipher/nonce")
	}

	got, ok := b.openBody(identA.Public().PublicKey, cipherB64, nonceB64)
	if !ok {
		t.Fatal("openBody failed")
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

func TestOpenBodyRejectsTamper(t *testing.T) {
	identA := newIdent(t)
	identB := newIdent(t)
	a := svcWith(identA)
	b := svcWith(identB)

	cipherB64, nonceB64, ok := a.sealBody(identB.Public().PublicKey, "hi")
	if !ok {
		t.Fatal("sealBody ok=false")
	}
	// Flip a base64 char to corrupt the ciphertext.
	corrupted := []byte(cipherB64)
	if corrupted[0] == 'A' {
		corrupted[0] = 'B'
	} else {
		corrupted[0] = 'A'
	}
	if _, ok := b.openBody(identA.Public().PublicKey, string(corrupted), nonceB64); ok {
		t.Fatal("tampered ciphertext should not open")
	}
}

func TestSealBodyFallbackNoPeerKey(t *testing.T) {
	a := svcWith(newIdent(t))
	if _, _, ok := a.sealBody("", "body"); ok {
		t.Fatal("sealBody should fall back (ok=false) with empty peer key")
	}
	if _, _, ok := a.sealBody("not-a-valid-hex-key", "body"); ok {
		t.Fatal("sealBody should fall back (ok=false) with invalid peer key")
	}
}

func TestOpenBodyWrongPeerFails(t *testing.T) {
	identA := newIdent(t)
	identB := newIdent(t)
	identC := newIdent(t)
	a := svcWith(identA)
	c := svcWith(identC)

	cipherB64, nonceB64, ok := a.sealBody(identB.Public().PublicKey, "for B only")
	if !ok {
		t.Fatal("sealBody ok=false")
	}
	// C tries to open a message that was sealed for B: authentication fails.
	if _, ok := c.openBody(identA.Public().PublicKey, cipherB64, nonceB64); ok {
		t.Fatal("wrong recipient should not open")
	}
}