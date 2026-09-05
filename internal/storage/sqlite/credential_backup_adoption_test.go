package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
)

func TestBackupCredentialRefIsAdopted(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "backup-adopt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "Demo", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com/v1", CredentialRef: "primary-ref",
		CredentialRefBackups: []string{"backup-ref"},
		CredentialState:      provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "m", DisplayName: "M", IsDefault: true}},
	}
	if _, err = store.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = store.db.ExecContext(context.Background(), `INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at) VALUES(?,?,?,?,?,?)`,
		"backup-ref", item.ID, "https://example.com", "openai_compatible", "receipt-backup", formatTime(now)); err != nil {
		t.Fatal(err)
	}
	ok, err := store.IsCredentialReferenceAdopted(context.Background(), secret.Ref{
		CredentialRef: "backup-ref", ProviderID: item.ID, Origin: "https://example.com", Protocol: "openai_compatible",
	})
	if err != nil || !ok {
		t.Fatalf("backup ref adopted=%v err=%v", ok, err)
	}
}
