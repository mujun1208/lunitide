package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

func prepareElectronCredentialCandidate(t *testing.T) (*Store, providerapp.ElectronCredentialPlan, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "providers.json")
	cipherCanary := "CIPHERTEXT-CANARY-NOT-A-REAL-BLOB"
	raw := `{"version":"0.2.1","providers":[{"id":"legacy-adopt","name":"Adopt","protocol":"openai","baseUrl":"https://example.test/v1","models":["model-a"],"defaultModel":"model-a","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z","encryptedApiKey":"` + cipherCanary + `"}]}`
	if err := os.WriteFile(source, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := inspectElectronFile(source)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err = s.runInspectedElectronProviderMetadata(ctx, in); err != nil {
		s.Close()
		t.Fatal(err)
	}
	tuples := []providerapp.ElectronCredentialTuple{{SourceFingerprint: in.fingerprint, ItemFingerprint: in.itemFP[0]}}
	plans, err := s.PlanElectronCredentials(ctx, tuples)
	if err != nil || len(plans) != 1 {
		s.Close()
		t.Fatalf("plans=%#v err=%v", plans, err)
	}
	return s, plans[0], cipherCanary
}

func TestElectronCredentialAdoptionHappyReplayAndMetadataOnly(t *testing.T) {
	s, plan, cipherCanary := prepareElectronCredentialCandidate(t)
	defer s.Close()
	ctx := context.Background()
	in := providerapp.ElectronCredentialAdoption{ElectronCredentialPlan: plan, CredentialRef: ulid.Make().String()}
	receipt, err := s.AdoptElectronCredential(ctx, "electron-adopt-replay", in)
	if err != nil || receipt == "" {
		t.Fatalf("receipt=%q err=%v", receipt, err)
	}
	replay, err := s.AdoptElectronCredential(ctx, "electron-adopt-replay", in)
	if err != nil || replay != receipt {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	var state, ref, migrationState, storedReceipt string
	if err = s.db.QueryRow(`SELECT p.credential_state,p.credential_ref,i.credential_migration_state,i.credential_receipt_id FROM providers p JOIN provider_metadata_migration_items i ON i.provider_id=p.id WHERE p.id=?`, plan.ProviderID).Scan(&state, &ref, &migrationState, &storedReceipt); err != nil {
		t.Fatal(err)
	}
	if state != "configured" || ref != in.CredentialRef || migrationState != "adopted" || storedReceipt != receipt {
		t.Fatalf("unexpected state %q %q %q %q", state, ref, migrationState, storedReceipt)
	}
	for _, table := range []string{"providers", "provider_metadata_migration_items", "credential_adoptions", "audit_events", "outbox_events", "idempotency_records"} {
		rows, err := s.db.Query(`SELECT * FROM ` + table)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(values)
			if strings.Contains(string(encoded), cipherCanary) || strings.Contains(string(encoded), "PLAINTEXT-CANARY") {
				t.Fatalf("sensitive canary in %s", table)
			}
		}
		rows.Close()
	}
}

func TestElectronCredentialAdoptionManualCredentialWinsCAS(t *testing.T) {
	s, plan, _ := prepareElectronCredentialCandidate(t)
	defer s.Close()
	ctx := context.Background()
	manual := ulid.Make().String()
	if _, err := s.db.Exec(`UPDATE providers SET credential_ref=?,credential_state='configured',version=version+1 WHERE id=?`, manual, plan.ProviderID); err != nil {
		t.Fatal(err)
	}
	in := providerapp.ElectronCredentialAdoption{ElectronCredentialPlan: plan, CredentialRef: ulid.Make().String()}
	if _, err := s.AdoptElectronCredential(ctx, "electron-adopt-cas", in); err == nil {
		t.Fatal("stale adoption accepted")
	}
	var got string
	if err := s.db.QueryRow(`SELECT credential_ref FROM providers WHERE id=?`, plan.ProviderID).Scan(&got); err != nil || got != manual {
		t.Fatalf("manual credential overwritten: %q %v", got, err)
	}
	if err := s.DispositionElectronCredential(ctx, plan.ElectronCredentialTuple, "superseded"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.db.QueryRow(`SELECT credential_migration_state FROM provider_metadata_migration_items WHERE source_fingerprint=? AND item_fingerprint=?`, plan.SourceFingerprint, plan.ItemFingerprint).Scan(&state); err != nil || state != "superseded" {
		t.Fatalf("state=%q err=%v", state, err)
	}
}

func TestElectronCredentialPlanningAcceptsTwoSourceAggregate(t *testing.T) {
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tuple := providerapp.ElectronCredentialTuple{SourceFingerprint: strings.Repeat("a", 64), ItemFingerprint: strings.Repeat("b", 64)}
	tuples := make([]providerapp.ElectronCredentialTuple, maxElectronProviders+1)
	for i := range tuples {
		tuples[i] = tuple
	}
	if plans, err := s.PlanElectronCredentials(context.Background(), tuples); err != nil || len(plans) != 0 {
		t.Fatalf("two-source aggregate rejected: plans=%d err=%v", len(plans), err)
	}
	if _, err := s.PlanElectronCredentials(context.Background(), make([]providerapp.ElectronCredentialTuple, maxElectronCredentialTuples+1)); err == nil {
		t.Fatal("unbounded Electron credential tuple aggregate accepted")
	}
}
