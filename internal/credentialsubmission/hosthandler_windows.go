//go:build windows

package credentialsubmission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

// EngineCaller is the authenticated private Host-to-Engine channel.
type EngineCaller interface {
	Call(context.Context, bridge.Request) (bridge.Response, error)
}
type HostHandler struct {
	Coordinator *Coordinator
	Engine      EngineCaller
	Secrets     secret.Service
	cleanupOnce sync.Once
	cleanupWake chan struct{}
}

type submitPayload struct {
	Scope struct {
		ProviderID       string `json:"providerId,omitempty"`
		DraftFingerprint string `json:"draftFingerprint,omitempty"`
	} `json:"scope"`
	Protocol   provider.Protocol `json:"protocol,omitempty"`
	Origin     string            `json:"origin,omitempty"`
	Request    json.RawMessage   `json:"request"`
	Credential string            `json:"credential"`
}

func (h *HostHandler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	if h.Coordinator == nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_METHOD_NOT_ALLOWED", "请求的方法不在白名单中", false)
	}
	if r.Method == "provider.create" || r.Method == "provider.update" {
		var payload map[string]json.RawMessage
		if decodeStrictLocal(r.Payload, &payload) != nil || payload == nil {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "供应商参数无效", false)
		}
		if _, supplied := payload["credentialSubmissionId"]; !supplied {
			response, err := h.Engine.Call(ctx, r)
			if err != nil {
				return bridge.Failure(r.ID, r.TraceID, "ENGINE_UNAVAILABLE", "核心引擎暂时不可用", true)
			}
			return response
		}
		var submissionID string
		if json.Unmarshal(payload["credentialSubmissionId"], &submissionID) != nil || submissionID == "" {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "凭据令牌无效", false)
		}
		delete(payload, "credentialSubmissionId")
		canonical, _ := json.Marshal(payload)
		response, err := h.Mutate(ctx, submissionID, canonical, r)
		if err != nil {
			return bridge.Failure(r.ID, r.TraceID, "CREDENTIAL_MUTATION_UNCERTAIN", "供应商写入结果尚待恢复", true)
		}
		return response
	}
	if r.Method == "provider.delete" {
		return h.deleteProvider(ctx, r)
	}
	if r.Method != "provider.credential.submit" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_METHOD_NOT_ALLOWED", "请求的方法不在白名单中", false)
	}
	var p submitPayload
	if decodeStrictLocal(r.Payload, &p) != nil || len(p.Request) == 0 || p.Credential == "" || ((p.Scope.ProviderID == "") == (p.Scope.DraftFingerprint == "")) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "凭据提交参数无效", false)
	}
	// Drop the plaintext-bearing request buffer after strict decoding. Go
	// strings/decoder allocations may be copied or retained by the runtime, so
	// this minimizes lifetime but cannot guarantee zero residual process memory.
	r.Payload = nil
	var boundRequest map[string]json.RawMessage
	if decodeStrictLocal(p.Request, &boundRequest) != nil || boundRequest == nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "凭据绑定请求无效", false)
	}
	delete(boundRequest, "credentialSubmissionId")
	canonicalRequest, err := json.Marshal(boundRequest)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "凭据绑定请求无效", false)
	}
	// Existing-provider tuple fields come from the exact request being bound;
	// top-level renderer fields are authoritative only for draft creation.
	protocol, origin := p.Protocol, p.Origin
	if p.Scope.ProviderID != "" {
		protocol, origin, err = h.updateTarget(ctx, p.Scope.ProviderID, canonicalRequest)
		if err != nil {
			return bridge.Failure(r.ID, r.TraceID, "CREDENTIAL_SUBMISSION_REJECTED", "凭据提交失败", false)
		}
	}
	credential := []byte(p.Credential)
	p.Credential = ""
	defer secret.Zero(credential)
	sub, err := h.Coordinator.Submit(ctx, SubmitInput{Scope: Scope{ProviderID: p.Scope.ProviderID, DraftFingerprint: p.Scope.DraftFingerprint}, Protocol: protocol, Origin: origin, Request: canonicalRequest, Credential: credential})
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "CREDENTIAL_SUBMISSION_REJECTED", "凭据提交失败", false)
	}
	return bridge.Success(r.ID, map[string]any{"credentialSubmissionId": sub.SubmissionID, "providerId": sub.ProviderID, "expiresAt": sub.ExpiresAt, "expiresInSeconds": max(1, int(time.Until(sub.ExpiresAt).Seconds()))})
}

func (h *HostHandler) updateTarget(ctx context.Context, providerID string, request []byte) (provider.Protocol, string, error) {
	var p struct {
		ID              string             `json:"id"`
		Protocol        *provider.Protocol `json:"protocol"`
		BaseURL         *string            `json:"baseUrl"`
		ExpectedVersion int64              `json:"expectedVersion"`
	}
	if json.Unmarshal(request, &p) != nil || p.ID != providerID || p.ExpectedVersion < 1 {
		return "", "", errors.New("update request does not match scope")
	}
	current, err := (RPCResolver{Engine: h.Engine}).ResolveProvider(ctx, providerID)
	if err != nil || current.ID != providerID {
		return "", "", errors.New("provider identity unavailable")
	}
	protocol := current.Protocol
	if p.Protocol != nil {
		protocol = *p.Protocol
	}
	if protocol != provider.ProtocolOpenAICompatible && protocol != provider.ProtocolAnthropic {
		return "", "", errors.New("invalid target protocol")
	}
	baseURL := current.BaseURL
	if p.BaseURL != nil {
		baseURL = *p.BaseURL
	}
	origin, err := provider.NormalizeOrigin(baseURL)
	return protocol, origin, err
}

func (h *HostHandler) deleteProvider(ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodeStrictLocal(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "供应商参数无效", false)
	}
	internal := map[string]any{"id": p.ID, "expectedVersion": p.ExpectedVersion}
	r.Method = "internal.provider.delete.coordinated"
	r.Payload, _ = json.Marshal(internal)
	response, err := h.Engine.Call(ctx, r)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "CREDENTIAL_MUTATION_UNCERTAIN", "供应商删除结果尚待恢复", true)
	}
	if response.OK {
		h.ScheduleCleanup()
	}
	return response
}

// StartCleanupWorker runs bounded startup/runtime cleanup with cancellation and
// backoff. Mutation success is never changed by a later secret-store failure.
func (h *HostHandler) StartCleanupWorker(ctx context.Context) {
	h.cleanupOnce.Do(func() {
		h.cleanupWake = make(chan struct{}, 1)
		go func() {
			timer := time.NewTimer(0)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				case <-h.cleanupWake:
				}
				drainCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := h.Coordinator.CleanupExpired(drainCtx)
				if cleanupErr := h.DrainCleanup(drainCtx); err == nil {
					err = cleanupErr
				}
				cancel()
				delay := 30 * time.Second
				if err != nil {
					delay = 5 * time.Second
				}
				timer.Reset(delay)
			}
		}()
	})
}

func (h *HostHandler) ScheduleCleanup() {
	if h.cleanupWake == nil {
		return
	}
	select {
	case h.cleanupWake <- struct{}{}:
	default:
	}
}

func (h *HostHandler) DrainCleanup(ctx context.Context) error {
	if h.Secrets == nil {
		return errors.New("secret service unavailable")
	}
	owner := ulid.Make().String()
	for {
		resp, err := RPCResolver{Engine: h.Engine}.call(ctx, "internal.credential-cleanup.claim", map[string]any{"owner": owner, "limit": 20})
		if err != nil {
			return err
		}
		b, _ := json.Marshal(resp.Payload)
		var events []struct {
			ID      string          `json:"id"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(b, &events) != nil {
			return errors.New("invalid cleanup response")
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			var p struct {
				CredentialRef string `json:"credentialRef"`
				ProviderID    string `json:"providerId"`
				Origin        string `json:"origin"`
				Protocol      string `json:"protocol"`
			}
			if json.Unmarshal(event.Payload, &p) != nil {
				_, err = (RPCResolver{Engine: h.Engine}).call(ctx, "internal.credential-cleanup.retry", map[string]string{"id": event.ID, "owner": owner, "error": "invalid cleanup binding"})
				if err != nil {
					return fmt.Errorf("retry malformed cleanup %s: %w", event.ID, err)
				}
				continue
			}
			ref := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol}
			current, e := RPCResolver{Engine: h.Engine}.IsCredentialReferenceAdopted(ctx, ref)
			if e == nil && !current {
				e = h.Secrets.Delete(ctx, ref)
			}
			method := "internal.credential-cleanup.complete"
			payload := map[string]string{"id": event.ID, "owner": owner}
			if e != nil || current {
				method = "internal.credential-cleanup.retry"
				payload["error"] = "reference still current or authoritative result unavailable"
			}
			_, err = (RPCResolver{Engine: h.Engine}).call(ctx, method, payload)
			if err != nil {
				return fmt.Errorf("cleanup disposition %s: %w", event.ID, err)
			}
		}
	}
}

// Mutate executes the strict reserve/mutate/confirm/adopt/consume protocol. The
// caller supplies the exact canonical request bytes returned in submitPayload.request.
func (h *HostHandler) Mutate(ctx context.Context, submissionID string, canonical []byte, request bridge.Request) (bridge.Response, error) {
	reservation, err := h.Coordinator.Reserve(ctx, submissionID, canonical)
	if err != nil {
		return bridge.Response{}, err
	}
	var public map[string]any
	if json.Unmarshal(request.Payload, &public) != nil {
		return bridge.Response{}, errors.New("invalid mutation payload")
	}
	delete(public, "credentialSubmissionId")
	internal := map[string]any{"providerId": reservation.ProviderID, "credentialRef": reservation.Ref.CredentialRef, "origin": reservation.Ref.Origin, "protocol": reservation.Ref.Protocol}
	if request.Method == "provider.create" {
		internal["create"] = public
		request.Method = "internal.provider.create.with-credential"
	} else {
		internal["update"] = public
		request.Method = "internal.provider.update.with-credential"
	}
	request.Payload, _ = json.Marshal(internal)
	response, callErr := h.Engine.Call(ctx, request)
	if callErr != nil {
		return bridge.Response{}, callErr
	}
	if !response.OK {
		_ = h.Coordinator.CleanupExpired(context.Background())
		return response, nil
	}
	// The successful Engine transaction has already committed both the new
	// credential binding and any old-reference cleanup event. Wake cleanup
	// before local journal finalization so an Adopt/Consume failure cannot delay
	// durable cleanup until the periodic worker pass.
	h.ScheduleCleanup()
	// A successful Engine response is backed by an exact adoption receipt in the
	// same SQLite transaction; authoritative recovery performs the same query.
	if _, err = h.Coordinator.Adopt(ctx, submissionID, canonical); err != nil {
		return bridge.Response{}, err
	}
	if _, err = h.Coordinator.Consume(ctx, submissionID, canonical); err != nil {
		return bridge.Response{}, err
	}
	return response, nil
}
func decodeStrictLocal(raw []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}

// RPCResolver exposes only authoritative binding/adoption fields over the
// authenticated private channel and never transports credential plaintext.
type RPCResolver struct{ Engine EngineCaller }

func (r RPCResolver) ResolveProvider(ctx context.Context, id string) (provider.Provider, error) {
	return r.resolveFull(ctx, id)
}
func (r RPCResolver) resolveBinding(ctx context.Context, id string) (secret.Ref, bool, error) {
	response, err := r.call(ctx, "internal.provider.credential-binding.resolve", map[string]string{"id": id})
	if err != nil {
		return secret.Ref{}, false, err
	}
	b, _ := json.Marshal(response.Payload)
	var p struct {
		Configured    bool   `json:"configured"`
		CredentialRef string `json:"credentialRef"`
		ProviderID    string `json:"providerId"`
		Origin        string `json:"origin"`
		Protocol      string `json:"protocol"`
	}
	err = json.Unmarshal(b, &p)
	return secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ProviderID, Origin: p.Origin, Protocol: p.Protocol}, p.Configured, err
}
func (r RPCResolver) resolveFull(ctx context.Context, id string) (provider.Provider, error) {
	response, err := r.call(ctx, "internal.provider.resolve", map[string]string{"id": id})
	if err != nil {
		return provider.Provider{}, err
	}
	b, _ := json.Marshal(response.Payload)
	var p struct {
		ID            string            `json:"id"`
		Protocol      provider.Protocol `json:"protocol"`
		BaseURL       string            `json:"baseUrl"`
		CredentialRef string            `json:"credentialRef"`
	}
	err = json.Unmarshal(b, &p)
	return provider.Provider{ID: p.ID, Protocol: p.Protocol, BaseURL: p.BaseURL, CredentialRef: p.CredentialRef}, err
}
func (r RPCResolver) IsCredentialReferenceAdopted(ctx context.Context, ref secret.Ref) (bool, error) {
	response, err := r.call(ctx, "internal.provider.credential-adoption.resolve", map[string]string{"credentialRef": ref.CredentialRef, "providerId": ref.ProviderID, "origin": ref.Origin, "protocol": ref.Protocol})
	if err != nil {
		return false, err
	}
	b, _ := json.Marshal(response.Payload)
	var result struct {
		Adopted bool `json:"adopted"`
	}
	err = json.Unmarshal(b, &result)
	return result.Adopted, err
}
func (r RPCResolver) call(ctx context.Context, method string, payload any) (bridge.Response, error) {
	b, _ := json.Marshal(payload)
	req := bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: method, SentAt: time.Now().UTC(), Payload: b, DeadlineMS: 3000}
	response, err := r.Engine.Call(ctx, req)
	if err == nil && !response.OK {
		err = errors.New("authoritative resolver rejected request")
	}
	return response, err
}
