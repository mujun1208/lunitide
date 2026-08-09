package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

const providerMutationActor = "desktop-host"

var errInvalidProviderPatch = errors.New("invalid provider patch")

type createProviderPayload struct {
	Name                   string            `json:"name"`
	Protocol               provider.Protocol `json:"protocol"`
	BaseURL                string            `json:"baseUrl"`
	Models                 []modelPayload    `json:"models"`
	CredentialSubmissionID *string           `json:"credentialSubmissionId"`
	Status                 *provider.Status  `json:"status"`
}

type updateProviderPayload struct {
	ID                     string             `json:"id"`
	Name                   *string            `json:"name"`
	Protocol               *provider.Protocol `json:"protocol"`
	BaseURL                *string            `json:"baseUrl"`
	Models                 *[]modelPayload    `json:"models"`
	CredentialSubmissionID *string            `json:"credentialSubmissionId"`
	Status                 *provider.Status   `json:"status"`
	ExpectedVersion        int64              `json:"expectedVersion"`
}

type modelPayload struct {
	ModelID     string `json:"modelId"`
	DisplayName string `json:"displayName"`
	IsDefault   *bool  `json:"isDefault"`
}

func handleProviderCreate(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload createProviderPayload
	if decodePayload(request.Payload, &payload) != nil || !publicNameValid(payload.Name) || payload.Protocol == "" || strings.TrimSpace(payload.BaseURL) == "" {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "provider.create 参数无效", false)
	}
	models, modelsOK := publicModels(payload.Models)
	if !modelsOK {
		return invalidProviderPayload(request, "provider.create")
	}
	if payload.Status != nil && *payload.Status != provider.StatusEnabled && *payload.Status != provider.StatusDisabled {
		return invalidProviderPayload(request, "provider.create")
	}
	if payload.CredentialSubmissionID != nil {
		if !canonicalULID(*payload.CredentialSubmissionID) {
			return invalidProviderPayload(request, "provider.create")
		}
		return credentialSubmissionDisabled(request)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	baseURL, err := provider.NormalizeBaseURL(payload.BaseURL)
	if err != nil {
		return invalidProviderPayload(request, "provider.create")
	}
	status := provider.StatusEnabled
	if payload.Status != nil {
		status = *payload.Status
	}
	item := provider.Provider{ID: ulid.Make().String(), Name: payload.Name, Protocol: payload.Protocol, BaseURL: baseURL, Models: models, Status: status, CredentialState: provider.CredentialMissing}
	if item.Validate() != nil {
		return invalidProviderPayload(request, "provider.create")
	}
	item.ID = ""
	created, err := e.providers.CreateRequest(ctx, request.IdempotencyKey, providerMutationActor, payload, item)
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, publicProvider(created))
}

func handleProviderGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		ID string `json:"id"`
	}
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ID) {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "provider.get 参数无效", false)
	}
	item, err := e.providers.Get(ctx, payload.ID)
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, publicProvider(item))
}

func handleProviderUpdate(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload updateProviderPayload
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ID) || payload.ExpectedVersion < 1 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "provider.update 参数无效", false)
	}
	if payload.CredentialSubmissionID != nil {
		if !canonicalULID(*payload.CredentialSubmissionID) {
			return invalidProviderPayload(request, "provider.update")
		}
		return credentialSubmissionDisabled(request)
	}
	if payload.Name != nil && !publicNameValid(*payload.Name) {
		return invalidProviderPayload(request, "provider.update")
	}
	if payload.Status != nil && *payload.Status != provider.StatusEnabled && *payload.Status != provider.StatusDisabled {
		return invalidProviderPayload(request, "provider.update")
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	updated, err := e.providers.UpdateRequest(ctx, request.IdempotencyKey, providerMutationActor, payload, payload.ID, payload.ExpectedVersion, func(item provider.Provider) (provider.Provider, error) {
		return applyProviderPatch(item, payload)
	})
	if errors.Is(err, errInvalidProviderPatch) {
		return invalidProviderPayload(request, "provider.update")
	}
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, publicProvider(updated))
}

func applyProviderPatch(item provider.Provider, payload updateProviderPayload) (provider.Provider, error) {
	return applyProviderPatchInternal(item, payload, nil)
}

func applyProviderPatchWithCredential(item provider.Provider, payload updateProviderPayload, ref secret.Ref) (provider.Provider, error) {
	return applyProviderPatchInternal(item, payload, &ref)
}

func applyProviderPatchInternal(item provider.Provider, payload updateProviderPayload, replacement *secret.Ref) (provider.Provider, error) {
	if payload.Name != nil {
		item.Name = *payload.Name
	}
	if payload.Models != nil {
		models, ok := publicModels(*payload.Models)
		if !ok {
			return item, errInvalidProviderPatch
		}
		item.Models = models
	}
	if payload.Status != nil {
		item.Status = *payload.Status
	}
	oldFingerprint, _ := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if payload.Protocol != nil {
		item.Protocol = *payload.Protocol
	}
	if payload.BaseURL != nil && *payload.BaseURL != item.BaseURL {
		if _, normalizeErr := provider.NormalizeOrigin(*payload.BaseURL); normalizeErr != nil {
			return item, errInvalidProviderPatch
		}
		item.BaseURL = *payload.BaseURL
	}
	newFingerprint, fingerprintErr := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if fingerprintErr != nil {
		return item, errInvalidProviderPatch
	}
	if oldFingerprint != newFingerprint {
		if item.CredentialRef != "" && replacement == nil {
			return item, provider.ErrCredentialReentryRequired
		}
		item.CredentialState = provider.CredentialRequiresReentry
	}
	item.BaseURL, _ = provider.NormalizeBaseURL(item.BaseURL)
	if replacement != nil {
		origin, err := provider.NormalizeOrigin(item.BaseURL)
		if err != nil || replacement.ProviderID != item.ID || replacement.Protocol != string(item.Protocol) || replacement.Origin != origin {
			return item, errInvalidProviderPatch
		}
		item.CredentialRef = replacement.CredentialRef
		item.CredentialState = provider.CredentialConfigured
	}
	if item.Validate() != nil {
		return item, errInvalidProviderPatch
	}
	return item, nil
}

func handleProviderDelete(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ID) || payload.ExpectedVersion < 1 {
		return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", "provider.delete 参数无效", false)
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	if _, err := e.providers.DeleteRequest(ctx, request.IdempotencyKey, providerMutationActor, payload, payload.ID, payload.ExpectedVersion); err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, map[string]any{"deleted": true})
}

func canonicalULID(value string) bool {
	id, err := ulid.ParseStrict(value)
	return err == nil && id.String() == value
}

func credentialSubmissionDisabled(request bridge.Request) bridge.Response {
	return bridge.Failure(request.ID, request.TraceID, "CREDENTIAL_SUBMISSION_NOT_ENABLED", "凭据提交链路尚未启用", false)
}

func credentialSubmissionRequired(request bridge.Request) bridge.Response {
	return bridge.Failure(request.ID, request.TraceID, "CREDENTIAL_SUBMISSION_REQUIRED", "地址或协议变更需要同时提交新凭据", false)
}

func requireIdempotency(request bridge.Request) *bridge.Response {
	if strings.TrimSpace(request.IdempotencyKey) != "" {
		return nil
	}
	response := bridge.Failure(request.ID, request.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	return &response
}

func invalidProviderPayload(request bridge.Request, method string) bridge.Response {
	return bridge.Failure(request.ID, request.TraceID, "BRIDGE_SCHEMA_INVALID", method+" 参数无效", false)
}

func publicNameValid(value string) bool {
	return strings.TrimSpace(value) == value && value != "" && len(value) <= 500
}

func publicModels(models []modelPayload) ([]provider.Model, bool) {
	if len(models) < 1 || len(models) > 50 {
		return nil, false
	}
	result := make([]provider.Model, len(models))
	for index, model := range models {
		if model.IsDefault == nil || !provider.ModelIDValid(model.ModelID) || strings.TrimSpace(model.DisplayName) != model.DisplayName || model.DisplayName == "" || len(model.DisplayName) > 200 {
			return nil, false
		}
		result[index] = provider.Model{ModelID: model.ModelID, DisplayName: model.DisplayName, IsDefault: *model.IsDefault}
	}
	return result, true
}
