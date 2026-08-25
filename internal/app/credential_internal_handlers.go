package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
)

type credentialMutationPayload struct {
	Create        *createProviderPayload `json:"create,omitempty"`
	Update        *updateProviderPayload `json:"update,omitempty"`
	ProviderID    string                 `json:"providerId"`
	CredentialRef string                 `json:"credentialRef"`
	Origin        string                 `json:"origin"`
	Protocol      string                 `json:"protocol"`
}

func handleProviderCreateWithCredential(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p credentialMutationPayload
	if decodePayload(request.Payload, &p) != nil || p.Create == nil || !canonicalULID(p.ProviderID) || !canonicalULID(p.CredentialRef) {
		return invalidProviderPayload(request, "provider.create")
	}
	cp := *p.Create
	cp.CredentialSubmissionID = nil
	baseURL, err := provider.NormalizeBaseURL(cp.BaseURL)
	origin, originErr := provider.NormalizeOrigin(cp.BaseURL)
	models, ok := publicModels(cp.Models)
	ref := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol}
	validatedRef, refErr := ref.Validate()
	if err != nil || originErr != nil || refErr != nil || validatedRef != ref || p.Protocol != string(cp.Protocol) || p.Origin != origin || !ok || !publicNameValid(cp.Name) {
		return invalidProviderPayload(request, "provider.create")
	}
	status := provider.StatusEnabled
	if cp.Status != nil {
		status = *cp.Status
	}
	item := provider.Provider{ID: p.ProviderID, Name: cp.Name, Protocol: cp.Protocol, BaseURL: baseURL, Models: models, Status: status, CredentialState: provider.CredentialConfigured, CredentialRef: p.CredentialRef}
	if item.Validate() != nil {
		return invalidProviderPayload(request, "provider.create")
	}
	created, err := e.providers.CreateRequest(ctx, request.IdempotencyKey, providerMutationActor, p, item)
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, publicProvider(created))
}

func handleProviderUpdateWithCredential(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p credentialMutationPayload
	if decodePayload(request.Payload, &p) != nil || p.Update == nil || p.Update.ID != p.ProviderID {
		return invalidProviderPayload(request, "provider.update")
	}
	ref := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol}
	if _, err := ref.Validate(); err != nil {
		return invalidProviderPayload(request, "provider.update")
	}
	up := *p.Update
	up.CredentialSubmissionID = nil
	lifecycle, ok := e.providers.(credentialLifecycleService)
	if !ok {
		return providerFailure(request, errors.New("credential lifecycle unavailable"))
	}
	updated, err := lifecycle.UpdateCredentialRequest(ctx, request.IdempotencyKey, providerMutationActor, p, up.ID, up.ExpectedVersion, func(item provider.Provider) (provider.Provider, error) {
		return applyProviderPatchWithCredential(item, up, ref)
	})
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, publicProvider(updated))
}

func handleProviderDeleteCoordinated(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Credential      *struct {
			CredentialRef string `json:"credentialRef"`
			ProviderID    string `json:"providerId"`
			Origin        string `json:"origin"`
			Protocol      string `json:"protocol"`
		} `json:"credential,omitempty"`
	}
	if decodePayload(request.Payload, &p) != nil || !canonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return invalidProviderPayload(request, "internal provider.delete")
	}
	var ref *secret.Ref
	if p.Credential != nil {
		v := secret.Ref{CredentialRef: p.Credential.CredentialRef, ProviderID: p.Credential.ProviderID, Origin: p.Credential.Origin, Protocol: p.Credential.Protocol}
		if _, err := v.Validate(); err != nil || v.ProviderID != p.ID {
			return invalidProviderPayload(request, "internal provider.delete")
		}
		ref = &v
	}
	lifecycle, ok := e.providers.(credentialLifecycleService)
	if !ok {
		return providerFailure(request, errors.New("credential lifecycle unavailable"))
	}
	if _, err := lifecycle.DeleteCoordinatedRequest(ctx, request.IdempotencyKey, providerMutationActor, p, p.ID, p.ExpectedVersion, ref); err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, map[string]any{"deleted": true})
}

func handleCredentialCleanupClaim(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Owner string `json:"owner"`
		Limit int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Owner == "" || p.Limit < 1 || p.Limit > 20 {
		return invalidProviderPayload(r, "cleanup claim")
	}
	l, ok := e.providers.(credentialLifecycleService)
	if !ok {
		return providerFailure(r, errors.New("credential lifecycle unavailable"))
	}
	events, err := l.ClaimCredentialCleanup(ctx, p.Owner, time.Now().UTC(), 30*time.Second, p.Limit)
	if err != nil {
		return providerFailure(r, err)
	}
	result := make([]map[string]any, 0, len(events))
	for _, event := range events {
		result = append(result, map[string]any{"id": event.ID, "payload": json.RawMessage(event.Payload)})
	}
	return bridge.Success(r.ID, result)
}
func handleCredentialCleanupComplete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return cleanupDisposition(e, ctx, r, false)
}
func handleCredentialCleanupRetry(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return cleanupDisposition(e, ctx, r, true)
}
func cleanupDisposition(e *Engine, ctx context.Context, r bridge.Request, retry bool) bridge.Response {
	var p struct {
		ID    string `json:"id"`
		Owner string `json:"owner"`
		Error string `json:"error,omitempty"`
	}
	if decodePayload(r.Payload, &p) != nil || p.ID == "" || p.Owner == "" || len(p.Error) > 1000 {
		return invalidProviderPayload(r, "cleanup disposition")
	}
	l, ok := e.providers.(credentialLifecycleService)
	if !ok {
		return providerFailure(r, errors.New("credential lifecycle unavailable"))
	}
	var err error
	if retry {
		if p.Error == "" {
			p.Error = "cleanup failed"
		}
		err = l.RetryCredentialCleanup(ctx, p.ID, p.Owner, time.Now().UTC().Add(time.Minute), p.Error)
	} else {
		err = l.CompleteCredentialCleanup(ctx, p.ID, p.Owner, time.Now().UTC())
	}
	if err != nil {
		return providerFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]bool{"ok": true})
}

var _ = json.Valid

func handleProviderResolve(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(request.Payload, &p) != nil || !canonicalULID(p.ID) {
		return invalidProviderPayload(request, "internal.provider.resolve")
	}
	v, err := e.providers.Get(ctx, p.ID)
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, map[string]any{"id": v.ID, "protocol": v.Protocol, "baseUrl": v.BaseURL, "credentialRef": v.CredentialRef})
}
func handleCredentialBindingResolve(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !canonicalULID(p.ID) {
		return invalidProviderPayload(r, "binding resolve")
	}
	resolver, ok := e.providers.(interface {
		ResolveCredentialBinding(context.Context, string) (secret.Ref, bool, error)
	})
	if !ok {
		return providerFailure(r, errors.New("credential binding resolver unavailable"))
	}
	ref, configured, err := resolver.ResolveCredentialBinding(ctx, p.ID)
	if err != nil {
		return providerFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"configured": configured, "credentialRef": ref.CredentialRef, "providerId": ref.ProviderID, "origin": ref.Origin, "protocol": ref.Protocol})
}
func handleCredentialAdoptionResolve(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		CredentialRef string `json:"credentialRef"`
		ProviderID    string `json:"providerId"`
		Origin        string `json:"origin"`
		Protocol      string `json:"protocol"`
	}
	if decodePayload(request.Payload, &p) != nil {
		return invalidProviderPayload(request, "internal adoption")
	}
	resolver, available := e.providers.(interface {
		IsCredentialReferenceAdopted(context.Context, secret.Ref) (bool, error)
	})
	if !available {
		return providerFailure(request, errors.New("adoption resolver unavailable"))
	}
	ok, err := resolver.IsCredentialReferenceAdopted(ctx, secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol})
	if err != nil {
		return providerFailure(request, err)
	}
	return bridge.Success(request.ID, map[string]bool{"adopted": ok})
}
