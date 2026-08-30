package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/voice/volcsauc"
)

type providerModelSyncPayload struct {
	ProviderID      string `json:"providerId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type providerTestPayload struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

var (
	errProviderDisabled              = errors.New("provider disabled")
	errProviderCredentialUnavailable = errors.New("provider credential unavailable")
	errModelDiscoveryEmpty           = errors.New("model discovery empty")
)

type diagnosticDTO struct {
	Status           string `json:"status"`
	Stage            string `json:"stage"`
	HTTPStatus       int    `json:"httpStatus,omitempty"`
	LatencyMS        int64  `json:"latencyMs"`
	Retryable        bool   `json:"retryable"`
	ErrorCode        string `json:"errorCode,omitempty"`
	SanitizedMessage string `json:"sanitizedMessage,omitempty"`
	TestedAt         string `json:"testedAt"`
}

func handleProviderTest(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload providerTestPayload
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ProviderID) || (payload.ModelID != "" && !modelIDValid(payload.ModelID)) {
		return invalidProviderPayload(request, "provider.test")
	}
	p, err := e.providers.Get(ctx, payload.ProviderID)
	if err != nil {
		return providerFailure(request, err)
	}
	if failure := providerReadyFailure(request, p); failure != nil {
		return *failure
	}
	modelID := payload.ModelID
	if modelID == "" {
		for _, m := range p.Models {
			if m.IsDefault {
				modelID = m.ModelID
				break
			}
		}
	} else if !storedModel(p, modelID) {
		return bridge.Failure(request.ID, request.TraceID, "MODEL_NOT_FOUND", "模型不存在", false)
	}
	started, testedAt := time.Now(), time.Now().UTC()
	if p.Protocol == provider.ProtocolVolcSpeech {
		err = e.withProviderLease(ctx, p, secretlease.OperationProviderTest, func(opCtx context.Context, secret []byte) error {
			return volcsauc.Probe(opCtx, volcsauc.ConfigFromSecret(p.BaseURL, modelID, string(secret)))
		})
		dto := diagnosticResult(err, time.Since(started), testedAt)
		if err != nil {
			dto.SanitizedMessage = volcsauc.SanitizeProbeError(err)
			dto.Stage = "connect"
			var he *volcsauc.HandshakeError
			if errors.As(err, &he) && he.Status != 0 {
				dto.HTTPStatus = he.Status
				if he.Status == 401 || he.Status == 403 {
					dto.Stage = "authenticate"
				}
			}
		}
		return bridge.Success(request.ID, dto)
	}
	err = e.withProviderLease(ctx, p, secretlease.OperationProviderTest, func(opCtx context.Context, secret []byte) error {
		adapter, adapterErr := e.adapter(opCtx, p)
		if adapterErr != nil {
			return adapterErr
		}
		testRequest := gateway.Request{Model: modelID, Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "ping"}}, MaxTokens: 1, MaxAttempts: 1}
		if tester, ok := adapter.(gateway.ConnectionTester); ok {
			return tester.TestConnection(opCtx, secret, testRequest)
		}
		_, adapterErr = adapter.Complete(opCtx, secret, testRequest)
		return adapterErr
	})
	dto := diagnosticResult(err, time.Since(started), testedAt)
	return bridge.Success(request.ID, dto)
}

func handleProviderModelSync(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var payload providerModelSyncPayload
	if decodePayload(request.Payload, &payload) != nil || !canonicalULID(payload.ProviderID) || payload.ExpectedVersion < 1 {
		return invalidProviderPayload(request, "provider.model.sync")
	}
	if failure := requireIdempotency(request); failure != nil {
		return *failure
	}
	syncer, ok := e.providers.(interface {
		SyncModelsDiscovery(context.Context, string, string, any, string, int64, func(provider.Provider) ([]provider.Model, string, error)) (provider.Provider, string, error)
	})
	if !ok {
		return bridge.Failure(request.ID, request.TraceID, "STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
	}
	updated, warning, err := syncer.SyncModelsDiscovery(ctx, request.IdempotencyKey, providerMutationActor, payload, payload.ProviderID, payload.ExpectedVersion, func(p provider.Provider) ([]provider.Model, string, error) {
		if p.Status != provider.StatusEnabled {
			return nil, "", errProviderDisabled
		}
		if p.CredentialState != provider.CredentialConfigured || p.CredentialRef == "" {
			return nil, "", errProviderCredentialUnavailable
		}
		if p.Protocol == provider.ProtocolAnthropic || p.Protocol == provider.ProtocolVolcSpeech {
			return append([]provider.Model(nil), p.Models...), "MODEL_DISCOVERY_UNSUPPORTED", nil
		}
		var discovery gateway.Discovery
		discoveryErr := e.withProviderLease(ctx, p, secretlease.OperationModelDiscover, func(opCtx context.Context, secret []byte) error {
			adapter, adapterErr := e.adapter(opCtx, p)
			if adapterErr == nil {
				discovery, adapterErr = adapter.Discover(opCtx, secret)
			}
			return adapterErr
		})
		if discoveryErr != nil {
			return nil, "", discoveryErr
		}
		models, warning, valid := discoveredModels(p, discovery)
		if !valid {
			return nil, "", errModelDiscoveryEmpty
		}
		return models, warning, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errProviderDisabled):
			return bridge.Failure(request.ID, request.TraceID, "PROVIDER_DISABLED", "供应商未启用", false)
		case errors.Is(err, errProviderCredentialUnavailable):
			return bridge.Failure(request.ID, request.TraceID, "PROVIDER_CREDENTIAL_UNAVAILABLE", "供应商凭据不可用", false)
		case errors.Is(err, errModelDiscoveryEmpty):
			return bridge.Failure(request.ID, request.TraceID, "MODEL_DISCOVERY_EMPTY", "未发现可用模型", false)
		}
		var gatewayErr *gateway.Error
		if errors.As(err, &gatewayErr) || networkpolicy.ErrorCode(err) != "" || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			d := diagnosticResult(err, 0, time.Now().UTC())
			return bridge.Failure(request.ID, request.TraceID, d.ErrorCode, d.SanitizedMessage, d.Retryable)
		}
		return providerFailure(request, err)
	}
	warnings := []string{}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	return bridge.Success(request.ID, map[string]any{"models": updated.Models, "warnings": warnings, "version": updated.Version})
}

func (e *Engine) adapter(ctx context.Context, p provider.Provider) (gateway.Adapter, error) {
	if e.adapterFactory != nil {
		return e.adapterFactory(ctx, p)
	}
	key := p.ID + "\x00" + p.BaseURL + "\x00" + string(p.Protocol)
	e.adapterCacheMu.Lock()
	if e.adapterCache == nil {
		e.adapterCache = make(map[string]gateway.Adapter)
	}
	if cached, ok := e.adapterCache[key]; ok {
		e.adapterCacheMu.Unlock()
		return cached, nil
	}
	e.adapterCacheMu.Unlock()
	created, err := e.newProductionAdapter(ctx, p)
	if err != nil {
		return nil, err
	}
	e.adapterCacheMu.Lock()
	if existing, ok := e.adapterCache[key]; ok {
		e.adapterCacheMu.Unlock()
		return existing, nil
	}
	e.adapterCache[key] = created
	e.adapterCacheMu.Unlock()
	return created, nil
}

func (e *Engine) newProductionAdapter(ctx context.Context, p provider.Provider) (gateway.Adapter, error) {
	// Provider connections are user-configured endpoints. Allow HTTP and
	// localhost so that local model servers (LM Studio, Ollama, etc.) are
	// reachable. The SSRF policy still applies to web fetch/search paths.
	network := e.network
	network.Policy = networkpolicy.Policy{AllowHTTP: true, AllowLocalhost: true}
	switch p.Protocol {
	case provider.ProtocolOpenAICompatible:
		return gateway.OpenAIEndpoint(ctx, p.BaseURL, network, e.gateway)
	case provider.ProtocolAnthropic:
		return gateway.AnthropicEndpoint(ctx, p.BaseURL, network, e.gateway)
	default:
		return nil, errors.New("invalid stored protocol")
	}
}

func (e *Engine) withProviderLease(ctx context.Context, p provider.Provider, operation secretlease.Operation, fn func(context.Context, []byte) error) error {
	if e.leases == nil {
		return errors.New("secret lease unavailable")
	}
	origin, err := provider.NormalizeOrigin(p.BaseURL)
	if err != nil {
		return err
	}
	ttl := secretlease.MaxTTL
	switch operation {
	case secretlease.OperationChat:
		ttl = secretlease.ChatMaxTTL
	case secretlease.OperationProviderTest, secretlease.OperationModelDiscover:
		ttl = secretlease.TestMaxTTL
	}
	deadline := time.Now().Add(ttl)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	opCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return e.leases.WithLease(opCtx, secretlease.Request{ProviderID: p.ID, CredentialRef: p.CredentialRef, Origin: origin, Protocol: string(p.Protocol), Operation: operation, Deadline: deadline}, func(secret []byte) error {
		return fn(opCtx, secret)
	})
}

func providerReadyFailure(request bridge.Request, p provider.Provider) *bridge.Response {
	if p.Status != provider.StatusEnabled {
		r := bridge.Failure(request.ID, request.TraceID, "PROVIDER_DISABLED", "供应商未启用", false)
		return &r
	}
	if p.CredentialState != provider.CredentialConfigured || p.CredentialRef == "" {
		r := bridge.Failure(request.ID, request.TraceID, "PROVIDER_CREDENTIAL_UNAVAILABLE", "供应商凭据不可用", false)
		return &r
	}
	return nil
}

func discoveredModels(current provider.Provider, d gateway.Discovery) ([]provider.Model, string, bool) {
	if d.Unsupported {
		return append([]provider.Model(nil), current.Models...), "MODEL_DISCOVERY_UNSUPPORTED", len(current.Models) > 0
	}
	set := make(map[string]struct{})
	for _, m := range d.Models {
		if modelIDValid(m.ID) {
			set[m.ID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 50 {
		ids = ids[:50]
	}
	if len(ids) == 0 {
		return nil, "", false
	}
	oldDefault := ""
	prior := make(map[string]provider.Model, len(current.Models))
	for _, m := range current.Models {
		prior[m.ModelID] = m
		if m.IsDefault {
			oldDefault = m.ModelID
		}
	}
	retained := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		retained[id] = struct{}{}
	}
	if _, exists := retained[oldDefault]; !exists {
		oldDefault = ids[0]
	}
	out := make([]provider.Model, len(ids))
	for i, id := range ids {
		next := provider.Model{ModelID: id, DisplayName: id, IsDefault: id == oldDefault, Kind: provider.KindLLM}
		if old, ok := prior[id]; ok {
			next.DisplayName = old.DisplayName
			next.ContextWindow = old.ContextWindow
			next.Kind = old.EffectiveKind()
			next.SupportsVision = old.SupportsVision
			next.KindDefault = old.KindDefault
		}
		out[i] = next
	}
	return out, "", true
}

func modelIDValid(id string) bool { return provider.ModelIDValid(id) }
func storedModel(p provider.Provider, id string) bool {
	for _, m := range p.Models {
		if m.ModelID == id {
			return true
		}
	}
	return false
}

func diagnosticResult(err error, latency time.Duration, testedAt time.Time) diagnosticDTO {
	d := diagnosticDTO{Status: "passed", Stage: "response", LatencyMS: max(0, latency.Milliseconds()), Retryable: false, TestedAt: testedAt.Format(time.RFC3339Nano)}
	if err == nil {
		return d
	}
	d.Status, d.ErrorCode, d.SanitizedMessage = "failed", "UPSTREAM_UNAVAILABLE", "供应商连接测试失败"
	var ge *gateway.Error
	if errors.As(err, &ge) {
		d.ErrorCode = ge.Code
		d.HTTPStatus = ge.HTTPStatus
		switch ge.Stage {
		case gateway.StageConnect:
			d.Stage = "connect"
		case gateway.StageHTTP:
			d.Stage = "request"
			if ge.HTTPStatus == 401 || ge.HTTPStatus == 403 {
				d.Stage = "authenticate"
			}
		case gateway.StageDecode:
			d.Stage = "response"
		}
		d.Retryable = ge.HTTPStatus == 429 || ge.HTTPStatus >= 500
		return d
	}
	if errors.Is(err, context.DeadlineExceeded) {
		d.Stage = "connect"
		d.ErrorCode = "TIMEOUT"
		d.Retryable = true
		d.SanitizedMessage = "供应商连接测试超时"
		return d
	}
	if errors.Is(err, context.Canceled) {
		d.Stage = "connect"
		d.ErrorCode = "CANCELLED"
		d.SanitizedMessage = "供应商连接测试已取消"
		return d
	}
	code := networkpolicy.ErrorCode(err)
	if code != "" {
		d.ErrorCode = string(code)
		d.Stage = "connect"
		d.Retryable = code == networkpolicy.CodeTimeout || code == networkpolicy.CodeConnectionRefused || code == networkpolicy.CodeDNSError
	} else {
		d.Stage = "resolve"
		d.ErrorCode = "INTERNAL_ERROR"
		d.SanitizedMessage = "连接诊断暂时不可用"
	}
	return d
}
