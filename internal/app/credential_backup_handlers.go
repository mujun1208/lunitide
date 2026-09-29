package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
)

var errBackupLimit = errors.New("at most four backup credentials are allowed")
var errBackupIndex = errors.New("backup index is out of range")

type backupMutationPayload struct {
	Backup        *backupAddPublicPayload `json:"backup,omitempty"`
	ProviderID    string                  `json:"providerId"`
	CredentialRef string                  `json:"credentialRef"`
	Origin        string                  `json:"origin"`
	Protocol      string                  `json:"protocol"`
}

type backupAddPublicPayload struct {
	ProviderID             string  `json:"providerId"`
	ExpectedVersion        int64   `json:"expectedVersion"`
	CredentialSubmissionID *string `json:"credentialSubmissionId,omitempty"`
}

func handleProviderCredentialBackupAdd(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload backupAddPublicPayload
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ProviderID) || payload.ExpectedVersion < 1 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "provider.credential.backup.add 参数无效", false)
	}
	if payload.CredentialSubmissionID == nil || !canonicalULID(*payload.CredentialSubmissionID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "备用 Key 必须先提交凭据令牌", false)
	}
	return credentialSubmissionDisabled(request)
}

func handleProviderCredentialBackupRemove(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		ProviderID      string `json:"providerId"`
		Index           int    `json:"index"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ProviderID) || payload.ExpectedVersion < 1 || payload.Index < 0 || payload.Index > 3 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "provider.credential.backup.remove 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	updated, err := e.providers.UpdateRequest(ctx, request.IdempotencyKey, providerMutationActor, payload, payload.ProviderID, payload.ExpectedVersion, func(item provider.Provider) (provider.Provider, error) {
		if payload.Index >= len(item.CredentialRefBackups) {
			return item, errBackupIndex
		}
		item.CredentialRefBackups = append(item.CredentialRefBackups[:payload.Index], item.CredentialRefBackups[payload.Index+1:]...)
		item.CredentialBackupCount = len(item.CredentialRefBackups)
		return item, nil
	})
	if err != nil {
		if errors.Is(err, errBackupIndex) {
			return request.Fail("PROVIDER_BACKUP_INDEX", "备用 Key 序号无效", false)
		}
		return providerFailure(request, err)
	}
	return request.Ok(publicProvider(updated))
}

func handleProviderBackupWithCredential(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p backupMutationPayload
	if decodePayload(request.Payload, &p) != nil || p.Backup == nil || p.Backup.ProviderID != p.ProviderID || !canonicalULID(p.ProviderID) || !canonicalULID(p.CredentialRef) {
		return invalidProviderPayload(request, "provider.credential.backup.add")
	}
	ref := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol}
	if _, err := ref.Validate(); err != nil {
		return invalidProviderPayload(request, "provider.credential.backup.add")
	}
	lifecycle, ok := e.providers.(credentialLifecycleService)
	if !ok {
		return providerFailure(request, errors.New("credential lifecycle unavailable"))
	}
	primary := ""
	updated, err := lifecycle.UpdateCredentialRequest(ctx, request.IdempotencyKey, providerMutationActor, p, p.Backup.ProviderID, p.Backup.ExpectedVersion, func(item provider.Provider) (provider.Provider, error) {
		primary = item.CredentialRef
		if item.CredentialRef == p.CredentialRef {
			return item, errors.New("backup credential must differ from the primary")
		}
		for _, existing := range item.CredentialRefBackups {
			if existing == p.CredentialRef {
				return item, errors.New("backup credential already present")
			}
		}
		if len(item.CredentialRefBackups) >= 4 {
			return item, errBackupLimit
		}
		item.CredentialRefBackups = append(append([]string{}, item.CredentialRefBackups...), p.CredentialRef)
		item.CredentialBackupCount = len(item.CredentialRefBackups)
		return item, nil
	})
	if err != nil {
		if errors.Is(err, errBackupLimit) {
			return request.Fail("PROVIDER_BACKUP_LIMIT", "最多 4 把备用 Key", false)
		}
		return providerFailure(request, err)
	}
	if updated.CredentialRef != primary {
		return request.Fail("PROVIDER_BACKUP_PRIMARY_CHANGED", "备用 Key 不得改写第一把凭据", false)
	}
	return request.Ok(publicProvider(updated))
}
