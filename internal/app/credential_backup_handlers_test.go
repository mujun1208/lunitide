package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

type backupLifecycleStub struct {
	providerRepositoryStub
	current provider.Provider
}

func (s *backupLifecycleStub) UpdateCredentialRequest(_ context.Context, _, _ string, _ any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	if s.current.ID != id || s.current.Version != expected {
		return s.current, provider.ErrConflict
	}
	updated, err := apply(s.current)
	if err != nil {
		return s.current, err
	}
	updated.Version++
	s.current = updated
	return updated, nil
}

func (s *backupLifecycleStub) UpdateRequest(ctx context.Context, k, actor string, request any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return s.UpdateCredentialRequest(ctx, k, actor, request, id, expected, apply)
}

func (s *backupLifecycleStub) DeleteCoordinatedRequest(context.Context, string, string, any, string, int64, *secret.Ref) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func (s *backupLifecycleStub) ClaimCredentialCleanup(context.Context, string, time.Time, time.Duration, int) ([]providerapp.ClaimedEvent, error) {
	return nil, nil
}

func (s *backupLifecycleStub) CompleteCredentialCleanup(context.Context, string, string, time.Time) error {
	return nil
}

func (s *backupLifecycleStub) RetryCredentialCleanup(context.Context, string, string, time.Time, string) error {
	return nil
}

func TestBackupAddKeepsPrimaryAndCounts(t *testing.T) {
	primary := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	backup := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	stub := &backupLifecycleStub{current: provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "Demo", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com/v1", CredentialRef: primary, Version: 1,
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "m", DisplayName: "M", IsDefault: true}},
	}}
	payload, _ := json.Marshal(map[string]any{
		"providerId": stub.current.ID, "credentialRef": backup,
		"origin": "https://example.com", "protocol": "openai_compatible",
		"backup": map[string]any{"providerId": stub.current.ID, "expectedVersion": 1},
	})
	req := validRequest("internal.provider.backup.with-credential", string(payload))
	req.IdempotencyKey = ulid.Make().String()
	resp := handleProviderBackupWithCredential(NewEngine(stub, "test"), context.Background(), req)
	if !resp.OK {
		t.Fatalf("backup.add %#v", resp.Error)
	}
	if stub.current.CredentialRef != primary {
		t.Fatalf("primary changed to %s", stub.current.CredentialRef)
	}
	if stub.current.BackupCount() != 1 || stub.current.CredentialRefBackups[0] != backup {
		t.Fatalf("backups=%v", stub.current.CredentialRefBackups)
	}
}

func TestBackupAddRejectsFifthKey(t *testing.T) {
	stub := &backupLifecycleStub{current: provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "Demo", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com/v1", CredentialRef: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Version: 1,
		CredentialRefBackups: []string{"01ARZ3NDEKTSV4RRFFQ69G5FA1", "01ARZ3NDEKTSV4RRFFQ69G5FA2", "01ARZ3NDEKTSV4RRFFQ69G5FA3", "01ARZ3NDEKTSV4RRFFQ69G5FA4"},
		CredentialState:      provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "m", DisplayName: "M", IsDefault: true}},
	}}
	payload, _ := json.Marshal(map[string]any{
		"providerId": stub.current.ID, "credentialRef": "01ARZ3NDEKTSV4RRFFQ69G5FA5",
		"origin": "https://example.com", "protocol": "openai_compatible",
		"backup": map[string]any{"providerId": stub.current.ID, "expectedVersion": 1},
	})
	req := validRequest("internal.provider.backup.with-credential", string(payload))
	req.IdempotencyKey = ulid.Make().String()
	resp := handleProviderBackupWithCredential(NewEngine(stub, "test"), context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROVIDER_BACKUP_LIMIT" {
		t.Fatalf("D-F4 %#v", resp)
	}
}
