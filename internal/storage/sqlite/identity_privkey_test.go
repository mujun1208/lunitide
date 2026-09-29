package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/secret"
)

// fakeSecrets is an in-memory stand-in for the DPAPI credential store. The real
// store is Windows-only; this keeps the seal/resolve seam testable everywhere.
type fakeSecrets struct {
	mu    sync.Mutex
	vault map[string][]byte
}

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{vault: map[string][]byte{}} }

func (f *fakeSecrets) key(ref secret.Ref) string {
	r, err := ref.Validate()
	if err != nil {
		return ref.CredentialRef
	}
	return r.CredentialRef + "|" + r.ProviderID + "|" + r.Origin + "|" + r.Protocol
}

func (f *fakeSecrets) Put(_ context.Context, ref secret.Ref, plaintext []byte) error {
	if _, err := ref.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(plaintext))
	copy(cp, plaintext)
	f.vault[f.key(ref)] = cp
	return nil
}

func (f *fakeSecrets) WithSecret(_ context.Context, ref secret.Ref, cb func([]byte) error) error {
	if _, err := ref.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	v, ok := f.vault[f.key(ref)]
	f.mu.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cb(cp)
}

func (f *fakeSecrets) Delete(_ context.Context, ref secret.Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vault, f.key(ref))
	return nil
}

func openIdentityStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The real ed25519 private key length is 64 bytes = 128 hex chars, matching the
// column CHECK. A deterministic "ab" x64 key is a valid stand-in for storage.
const testPrivKeyHex = "abababab" + "abababab" + "abababab" + "abababab" +
	"abababab" + "abababab" + "abababab" + "abababab" +
	"abababab" + "abababab" + "abababab" + "abababab" +
	"abababab" + "abababab" + "abababab" + "abababab"

func newIdentityRecord() identity.Record {
	return identity.Record{
		SubjectID:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		PublicKey:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		PrivateKey:  testPrivKeyHex,
		Nickname:    "月汐用户",
		Status:      "online",
		PairingCode: "123456",
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",
	}
}

func rawStoredPrivateKey(t *testing.T, s *Store) string {
	t.Helper()
	var got string
	if err := s.db.QueryRowContext(context.Background(), `SELECT private_key FROM local_identity WHERE singleton=1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestIdentityPrivateKeySealedOnInsert(t *testing.T) {
	s := openIdentityStore(t)
	s.WithIdentitySecrets(newFakeSecrets())
	ctx := context.Background()
	if err := s.InsertIdentity(ctx, newIdentityRecord()); err != nil {
		t.Fatal(err)
	}
	// Column must hold the sentinel, never the real key.
	if raw := rawStoredPrivateKey(t, s); raw != privateKeySentinel {
		t.Fatalf("column private_key = %q; want sentinel", raw)
	}
	// Load resolves the real key back out of DPAPI.
	rec, ok, err := s.LoadIdentity(ctx)
	if err != nil || !ok {
		t.Fatalf("LoadIdentity ok=%v err=%v", ok, err)
	}
	if rec.PrivateKey != testPrivKeyHex {
		t.Fatalf("resolved private key = %q; want plaintext", rec.PrivateKey)
	}
}

func TestIdentityPrivateKeyLegacyPlaintextMigratesOnLoad(t *testing.T) {
	s := openIdentityStore(t)
	ctx := context.Background()
	// Insert WITHOUT a secret store: legacy plaintext row (real key in column).
	if err := s.InsertIdentity(ctx, newIdentityRecord()); err != nil {
		t.Fatal(err)
	}
	if raw := rawStoredPrivateKey(t, s); raw != testPrivKeyHex {
		t.Fatalf("legacy column = %q; want plaintext", raw)
	}
	// Now wire the secret store and load: the row must migrate to the sentinel.
	s.WithIdentitySecrets(newFakeSecrets())
	rec, ok, err := s.LoadIdentity(ctx)
	if err != nil || !ok {
		t.Fatalf("LoadIdentity ok=%v err=%v", ok, err)
	}
	if rec.PrivateKey != testPrivKeyHex {
		t.Fatalf("resolved private key = %q; want plaintext", rec.PrivateKey)
	}
	if raw := rawStoredPrivateKey(t, s); raw != privateKeySentinel {
		t.Fatalf("column after migration = %q; want sentinel", raw)
	}
	// A second load comes straight from DPAPI and still works.
	rec2, ok2, err2 := s.LoadIdentity(ctx)
	if err2 != nil || !ok2 || rec2.PrivateKey != testPrivKeyHex {
		t.Fatalf("second load ok=%v err=%v key=%q", ok2, err2, rec2.PrivateKey)
	}
}

func TestIdentityPrivateKeyNoStoreKeepsPlaintext(t *testing.T) {
	s := openIdentityStore(t)
	ctx := context.Background()
	if err := s.InsertIdentity(ctx, newIdentityRecord()); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.LoadIdentity(ctx)
	if err != nil || !ok {
		t.Fatalf("LoadIdentity ok=%v err=%v", ok, err)
	}
	if rec.PrivateKey != testPrivKeyHex {
		t.Fatalf("private key = %q; want plaintext when no store wired", rec.PrivateKey)
	}
	if strings.TrimSpace(rawStoredPrivateKey(t, s)) != testPrivKeyHex {
		t.Fatal("column must stay plaintext when no secret store is wired")
	}
}