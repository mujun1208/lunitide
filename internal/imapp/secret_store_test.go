package imapp

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/secret"
)

// fakeIMStore emulates the sqlite store's secret write rule: a nil or empty
// InboundAppSecret leaves the stored value untouched.
type fakeIMStore struct{ rows map[Kind]Channel }

func newFakeIMStore() *fakeIMStore { return &fakeIMStore{rows: map[Kind]Channel{}} }

func (f *fakeIMStore) ListIMChannels(context.Context) ([]Channel, error) {
	out := make([]Channel, 0, len(f.rows))
	for k, ch := range f.rows {
		ch.Kind = k
		out = append(out, ch)
	}
	return out, nil
}

func (f *fakeIMStore) UpsertIMChannel(_ context.Context, kind Kind, patch ChannelPatch) ([]Channel, error) {
	ch := f.rows[kind]
	ch.Kind = kind
	if patch.InboundEnabled != nil {
		ch.InboundEnabled = *patch.InboundEnabled
	}
	if patch.InboundAllowlist != nil {
		ch.InboundAllowlist = *patch.InboundAllowlist
	}
	if patch.InboundAppID != nil {
		ch.InboundAppID = *patch.InboundAppID
	}
	if patch.InboundAppSecret != nil && *patch.InboundAppSecret != "" {
		ch.InboundAppSecret = *patch.InboundAppSecret
	}
	f.rows[kind] = ch
	out, _ := f.ListIMChannels(context.Background())
	return out, nil
}

// storedSecret reads the raw column value, bypassing the service's unseal.
func (f *fakeIMStore) storedSecret(kind Kind) string { return f.rows[kind].InboundAppSecret }

// fakeSecrets is an in-memory stand-in for the DPAPI store.
type fakeSecrets struct{ vault map[string][]byte }

func newFakeSecrets() *fakeSecrets { return &fakeSecrets{vault: map[string][]byte{}} }

func (f *fakeSecrets) key(ref secret.Ref) string { return ref.CredentialRef }

func (f *fakeSecrets) Put(_ context.Context, ref secret.Ref, b []byte) error {
	f.vault[f.key(ref)] = append([]byte(nil), b...)
	return nil
}

func (f *fakeSecrets) WithSecret(_ context.Context, ref secret.Ref, cb func([]byte) error) error {
	b, ok := f.vault[f.key(ref)]
	if !ok {
		return os.ErrNotExist
	}
	return cb(b)
}

func (f *fakeSecrets) Delete(_ context.Context, ref secret.Ref) error {
	delete(f.vault, f.key(ref))
	return nil
}

func TestInboundSecretRefValidates(t *testing.T) {
	// The real DPAPI store calls Ref.Validate on every Put/WithSecret; an
	// Origin that does not parse as a base URL would make saving a secret
	// fail only in production. Assert the synthesized ref is valid here.
	for _, kind := range []Kind{KindFeishu, KindWeCom, KindDingTalk} {
		if _, err := imSecretRef(kind).Validate(); err != nil {
			t.Fatalf("%s ref invalid: %v", kind, err)
		}
	}
}

func TestInboundSecretNeverStoredAsPlaintext(t *testing.T) {
	// Saving through Settings must leave no recoverable secret in the row.
	store := newFakeIMStore()
	svc := New(store).WithSecrets(newFakeSecrets())
	ctx := context.Background()

	appID := "cli_app_123"
	secretText := "s3cr3t-value-should-never-hit-the-db"
	enabled := true
	allow := "colleague@example.com"
	if _, err := svc.Set(ctx, KindFeishu, ChannelPatch{
		InboundEnabled:   &enabled,
		InboundAllowlist: &allow,
		InboundAppID:     &appID,
		InboundAppSecret: &secretText,
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	if got := store.storedSecret(KindFeishu); strings.Contains(got, secretText) {
		t.Fatalf("plaintext secret landed in the row: %q", got)
	}
	if got := store.storedSecret(KindFeishu); got != dpapiSecretMarker {
		t.Fatalf("row secret = %q; want the DPAPI marker", got)
	}

	// The worker still gets the real value back.
	ch, err := svc.LookupSecret(ctx, KindFeishu)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if ch.Secret() != secretText {
		t.Fatalf("worker secret = %q; want %q", ch.Secret(), secretText)
	}

	// The Settings-facing view never carries the secret and still reports it exists.
	pub := ch.Public()
	if pub.InboundAppSecret != "" || !pub.InboundHasSecret {
		t.Fatalf("public view leaks or hides the secret: %#v", pub)
	}
}

func TestLegacyPlaintextRowMigratesOnFirstRead(t *testing.T) {
	// A row written before this change holds the plaintext directly. The
	// first LookupSecret must seal it and blank the database copy, so an
	// attacker reading the db afterwards finds only the marker.
	store := newFakeIMStore()
	vault := newFakeSecrets()
	legacy := "legacy-plaintext-secret"
	store.rows[KindFeishu] = Channel{
		Kind:             KindFeishu,
		InboundEnabled:   true,
		InboundAppID:     "cli_legacy",
		InboundAppSecret: legacy,
	}
	svc := New(store).WithSecrets(vault)
	ctx := context.Background()

	ch, err := svc.LookupSecret(ctx, KindFeishu)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if ch.Secret() != legacy {
		t.Fatalf("migrated secret = %q; want %q", ch.Secret(), legacy)
	}
	if got := store.storedSecret(KindFeishu); got != dpapiSecretMarker {
		t.Fatalf("legacy plaintext still in the row after read: %q", got)
	}
	// A second read now comes from DPAPI and still works.
	again, err := svc.LookupSecret(ctx, KindFeishu)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if again.Secret() != legacy {
		t.Fatalf("second read = %q; want %q", again.Secret(), legacy)
	}
}

func TestMarkerWithoutAssetStaysIdleNotError(t *testing.T) {
	// If the credential file vanished under a marker row, the worker should
	// simply see no secret rather than a hard failure that logs on every tick.
	store := newFakeIMStore()
	store.rows[KindFeishu] = Channel{Kind: KindFeishu, InboundEnabled: true, InboundAppID: "cli", InboundAppSecret: dpapiSecretMarker}
	svc := New(store).WithSecrets(newFakeSecrets())
	ch, err := svc.LookupSecret(context.Background(), KindFeishu)
	if err != nil {
		t.Fatalf("lookup should not error: %v", err)
	}
	if ch.Secret() != "" {
		t.Fatalf("expected empty secret, got %q", ch.Secret())
	}
}

func TestNoSecretServiceKeepsLegacyBehavior(t *testing.T) {
	// Unit callers that never wire a store keep the old in-column behavior,
	// so the seam is opt-in.
	store := newFakeIMStore()
	svc := New(store)
	ctx := context.Background()
	appID, sec, en := "cli", "plain", true
	if _, err := svc.Set(ctx, KindFeishu, ChannelPatch{InboundEnabled: &en, InboundAppID: &appID, InboundAppSecret: &sec}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if store.storedSecret(KindFeishu) != "plain" {
		t.Fatalf("without a secret service the column keeps the value: %q", store.storedSecret(KindFeishu))
	}
}
