package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
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

func probeProviderModel(ctx context.Context, adapter llmadapter.Adapter, secret []byte, model provider.Model) error {
	switch model.EffectiveKind() {
	case provider.KindEmbedding:
		embedder, ok := adapter.(llmadapter.Embedder)
		if !ok {
			return errors.New("adapter does not support embeddings")
		}
		_, err := embedder.Embed(ctx, secret, model.ModelID, []string{"ping"})
		return err
	case provider.KindImage:
		generator, ok := adapter.(llmadapter.ImageGenerator)
		if !ok {
			return errors.New("adapter does not support image generation")
		}
		_, err := generator.GenerateImage(ctx, secret, model.ModelID, "A small solid blue square for a connection test")
		return err
	case provider.KindVideo:
		generator, ok := adapter.(llmadapter.VideoGenerator)
		if !ok {
			return errors.New("adapter does not support video generation")
		}
		_, err := generator.GenerateVideo(ctx, secret, model.ModelID, "A still blue square for a connection test")
		return err
	case provider.KindVision:
		// OCR proxies can be SSE-only. Exercise the same image-bearing inference
		// contract as real use, and verify recognition instead of accepting headers.
		result, err := adapter.Stream(ctx, secret, visionProbeRequest(model.ModelID), func(llmadapter.Delta) error { return nil })
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(model.ModelID), "ocr") {
			digits := strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, result.Message.Content)
			if digits != "123" {
				return &llmadapter.Error{Code: "OCR_VERIFICATION_FAILED", Stage: llmadapter.StageDecode, Message: "OCR did not recognize the test digits"}
			}
		} else if strings.TrimSpace(result.Message.Content) == "" {
			return &llmadapter.Error{Code: "INVALID_RESPONSE", Stage: llmadapter.StageDecode, Message: "vision response is empty"}
		}
		return nil
	default:
		testRequest := llmadapter.Request{Model: model.ModelID, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "ping"}}, MaxTokens: 1, MaxAttempts: 1}
		if model.EffectiveKind() == provider.KindVision {
			testRequest = visionProbeRequest(model.ModelID)
		}
		if tester, ok := adapter.(llmadapter.ConnectionTester); ok {
			return tester.TestConnection(ctx, secret, testRequest)
		}
		_, err := adapter.Complete(ctx, secret, testRequest)
		return err
	}
}

// A synthetic local image avoids sending a user's document during diagnostics.
func visionProbeRequest(modelID string) llmadapter.Request {
	canvas := image.NewRGBA(image.Rect(0, 0, 256, 128))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	for n, rows := range [][]string{{"010", "110", "010", "010", "111"}, {"111", "001", "111", "100", "111"}, {"111", "001", "111", "001", "111"}} {
		for y, row := range rows {
			for x, pixel := range row {
				if pixel == '1' {
					draw.Draw(canvas, image.Rect(32+n*64+x*12, 32+y*12, 44+n*64+x*12, 44+y*12), &image.Uniform{color.Black}, image.Point{}, draw.Src)
				}
			}
		}
	}
	var data bytes.Buffer
	_ = png.Encode(&data, canvas)
	messages := []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "Read the digits in the image. Reply with the digits only."}}
	if strings.Contains(strings.ToLower(modelID), "deepseek-ocr") {
		messages = append([]llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "<image>\nFree OCR."}}, messages...)
	}
	return llmadapter.Request{Model: modelID, Messages: messages, Images: []llmadapter.Image{{MIME: "image/png", Data: data.Bytes()}}, MaxTokens: 64, MaxAttempts: 1}
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
		if p.Protocol == provider.ProtocolVolcSpeech {
			modelID = volcListenModelID(p)
		} else {
			for _, m := range p.Models {
				if m.IsDefault {
					modelID = m.ModelID
					break
				}
			}
		}
	} else if !storedModel(p, modelID) {
		return request.Fail("MODEL_NOT_FOUND", "模型不存在", false)
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
		return request.Ok(dto)
	}
	err = e.withProviderLease(ctx, p, secretlease.OperationProviderTest, func(opCtx context.Context, secret []byte) error {
		adapter, adapterErr := e.adapterForModel(opCtx, p, modelByID(p, modelID))
		if adapterErr != nil {
			return adapterErr
		}
		return probeProviderModel(opCtx, adapter, secret, modelByID(p, modelID))
	})
	dto := diagnosticResult(err, time.Since(started), testedAt)
	return request.Ok(dto)
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
		return request.Fail("STORAGE_UNAVAILABLE", "供应商数据暂时不可用", true)
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
		var discovery llmadapter.Discovery
		discoveryErr := e.withProviderLease(ctx, p, secretlease.OperationModelDiscover, func(opCtx context.Context, secret []byte) error {
			adapter, adapterErr := e.adapter(opCtx, p)
			if adapterErr == nil {
				discovery, adapterErr = adapter.Discover(opCtx, secret)
			}
			return adapterErr
		})
		if discoveryErr != nil {
			// Many OpenAI-compatible endpoints (notably Volcengine Ark Plan at
			// /api/plan/v3) do not expose a GET /models list route and answer
			// 404. That is not a connection failure — chat/completions works
			// fine — so keep the provider's existing models and surface a
			// friendly warning instead of hard-failing the whole sync.
			var ge *llmadapter.Error
			if errors.As(discoveryErr, &ge) && ge.HTTPStatus == http.StatusNotFound {
				return append([]provider.Model(nil), p.Models...), "MODEL_LIST_UNSUPPORTED", nil
			}
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
			return request.Fail("PROVIDER_DISABLED", "供应商未启用", false)
		case errors.Is(err, errProviderCredentialUnavailable):
			return request.Fail("PROVIDER_CREDENTIAL_UNAVAILABLE", "供应商凭据不可用", false)
		case errors.Is(err, errModelDiscoveryEmpty):
			return request.Fail("MODEL_DISCOVERY_EMPTY", "未发现可用模型", false)
		}
		var gatewayErr *llmadapter.Error
		if errors.As(err, &gatewayErr) || networkpolicy.ErrorCode(err) != "" || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			d := diagnosticResult(err, 0, time.Now().UTC())
			return request.Fail(d.ErrorCode, d.SanitizedMessage, d.Retryable)
		}
		return providerFailure(request, err)
	}
	warnings := []string{}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	return request.Ok(map[string]any{"models": updated.Models, "warnings": warnings, "version": updated.Version})
}

func (e *Engine) adapterForModel(ctx context.Context, p provider.Provider, model provider.Model) (llmadapter.Adapter, error) {
	proxyVideo := p.Protocol == provider.ProtocolOpenAICompatible && model.EffectiveKind() == provider.KindVideo && strings.Contains(strings.ToLower(model.ModelID), "seedance")
	if proxyVideo {
		u, err := url.Parse(providerAdapterBaseURL(p.BaseURL))
		// Ark-compatible task APIs use /api/v3, even when the same proxy
		// serves OpenAI image and OCR endpoints below /v1.
		if err == nil && (u.Path == "/v1" || u.Path == "/" || u.Path == "") {
			u.Path, u.RawPath = "/api/v3", ""
			p.BaseURL = u.String()
		}
	}
	adapter, err := e.adapter(ctx, p)
	if err == nil && proxyVideo {
		adapter = e.withVideoProxy(adapter, p)
	}
	return adapter, err
}

func (e *Engine) adapter(ctx context.Context, p provider.Provider) (llmadapter.Adapter, error) {
	if e.adapterFactory != nil {
		return e.adapterFactory(ctx, p)
	}
	key := p.ID + "\x00" + p.BaseURL + "\x00" + string(p.Protocol)
	e.adapterCacheMu.Lock()
	if e.adapterCache == nil {
		e.adapterCache = make(map[string]llmadapter.Adapter)
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

func (e *Engine) newProductionAdapter(ctx context.Context, p provider.Provider) (llmadapter.Adapter, error) {
	// Provider connections are user-configured endpoints. Allow HTTP and
	// localhost so that local model servers (LM Studio, Ollama, etc.) are
	// reachable. The SSRF policy still applies to web fetch/search paths.
	network := e.network
	// LLM wire data includes SSE framing and inline media; keep the web budget separate.
	network.MaxResponseBytes = 8 << 20
	network.Policy = networkpolicy.Policy{AllowHTTP: true, AllowLocalhost: true}
	switch p.Protocol {
	case provider.ProtocolOpenAICompatible:
		return llmadapter.OpenAIEndpoint(ctx, providerAdapterBaseURL(p.BaseURL), network, e.gateway)
	case provider.ProtocolAnthropic:
		return llmadapter.AnthropicEndpoint(ctx, p.BaseURL, network, e.gateway)
	default:
		return nil, errors.New("invalid stored protocol")
	}
}

func providerAdapterBaseURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	clean := strings.TrimSuffix(u.Path, "/")
	for _, suffix := range []string{
		"/images/generations",
		"/videos/generations",
		"/video/generations",
		"/contents/generations/tasks",
	} {
		if strings.HasSuffix(strings.ToLower(clean), suffix) {
			clean = clean[:len(clean)-len(suffix)]
			break
		}
	}
	if clean == "" {
		clean = "/"
	}
	u.Path = clean
	u.RawPath = ""
	return u.String()
}

func (e *Engine) withProviderLease(ctx context.Context, p provider.Provider, operation secretlease.Operation, fn func(context.Context, []byte) error) error {
	return e.withProviderLeaseRef(ctx, p, p.CredentialRef, operation, fn)
}

func (e *Engine) withProviderLeaseRef(ctx context.Context, p provider.Provider, ref string, operation secretlease.Operation, fn func(context.Context, []byte) error) error {
	if operation == secretlease.OperationChat {
		if err := e.CheckCapability(ctx, "llm"); err != nil {
			return err
		}
	}
	if e.leases == nil {
		return errors.New("secret lease unavailable")
	}
	if ref == "" {
		ref = p.CredentialRef
	}
	origin, err := provider.NormalizeOrigin(p.BaseURL)
	if err != nil {
		return err
	}
	ttl := secretlease.MaxTTL
	switch operation {
	case secretlease.OperationChat:
		ttl = secretlease.ChatMaxTTL
	case secretlease.OperationProviderTest:
		ttl = secretlease.TestMaxTTL
	case secretlease.OperationModelDiscover:
		ttl = secretlease.DiscoverMaxTTL
	}
	deadline := time.Now().Add(ttl)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	opCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return e.leases.WithLease(opCtx, secretlease.Request{ProviderID: p.ID, CredentialRef: ref, Origin: origin, Protocol: string(p.Protocol), Operation: operation, Deadline: deadline}, func(secret []byte) error {
		return fn(opCtx, secret)
	})
}

type leaseRotateState struct {
	swaps int
}

func isQuotaRotateError(err error) bool {
	if err == nil {
		return false
	}
	var gatewayErr *llmadapter.Error
	if errors.As(err, &gatewayErr) && gatewayErr.HTTPStatus == 429 {
		return true
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "429") || strings.Contains(s, "insufficient_quota") {
		return true
	}
	return quotaWord.MatchString(s)
}

var quotaWord = regexp.MustCompile(`(?i)\bquota\b`)

func (e *Engine) withRotatingProviderLease(ctx context.Context, p provider.Provider, operation secretlease.Operation, rot *leaseRotateState, emitted func() bool, fn func(context.Context, []byte) error) error {
	refs := p.CredentialRefChain()
	if len(refs) == 0 {
		return e.withProviderLeaseRef(ctx, p, p.CredentialRef, operation, fn)
	}
	if rot == nil {
		rot = &leaseRotateState{}
	}
	var last error
	for i := rot.swaps; i < len(refs) && i <= 3; i++ {
		last = e.withProviderLeaseRef(ctx, p, refs[i], operation, fn)
		if last == nil {
			return nil
		}
		if emitted != nil && emitted() {
			return last
		}
		if !isQuotaRotateError(last) {
			return last
		}
		if i+1 >= len(refs) || i >= 3 {
			return last
		}
		rot.swaps = i + 1
		if rot.swaps > 3 {
			return last
		}
	}
	return last
}

type leaseRotateKey struct{}

type leaseRotateCtx struct {
	provider provider.Provider
	rot      *leaseRotateState
	emitted  func() bool
}

func withLeaseRotate(ctx context.Context, p provider.Provider, rot *leaseRotateState, emitted func() bool) context.Context {
	return context.WithValue(ctx, leaseRotateKey{}, leaseRotateCtx{provider: p, rot: rot, emitted: emitted})
}

func (e *Engine) completeMaybeRotate(ctx context.Context, a llmadapter.Adapter, credential []byte, req llmadapter.Request) (llmadapter.Response, error) {
	resp, err := a.Complete(ctx, credential, req)
	if err == nil || e == nil {
		return resp, err
	}
	shared, ok := ctx.Value(leaseRotateKey{}).(leaseRotateCtx)
	if !ok || shared.rot == nil {
		return resp, err
	}
	if shared.emitted != nil && shared.emitted() {
		return resp, err
	}
	if !isQuotaRotateError(err) {
		return resp, err
	}
	refs := shared.provider.CredentialRefChain()
	for shared.rot.swaps+1 < len(refs) && shared.rot.swaps < 3 {
		shared.rot.swaps++
		var out llmadapter.Response
		last := e.withProviderLeaseRef(ctx, shared.provider, refs[shared.rot.swaps], secretlease.OperationChat, func(op context.Context, secret []byte) error {
			inner, completeErr := a.Complete(op, secret, req)
			out = inner
			return completeErr
		})
		if last == nil {
			return out, nil
		}
		if shared.emitted != nil && shared.emitted() {
			return out, last
		}
		if !isQuotaRotateError(last) {
			return out, last
		}
		err = last
		resp = out
	}
	return resp, err
}

func providerReadyFailure(request bridge.Request, p provider.Provider) *bridge.Response {
	if p.Status != provider.StatusEnabled {
		r := request.Fail("PROVIDER_DISABLED", "供应商未启用", false)
		return &r
	}
	if p.CredentialState != provider.CredentialConfigured || p.CredentialRef == "" {
		r := request.Fail("PROVIDER_CREDENTIAL_UNAVAILABLE", "供应商凭据不可用", false)
		return &r
	}
	return nil
}

func discoveredModels(current provider.Provider, d llmadapter.Discovery) ([]provider.Model, string, bool) {
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
	var ge *llmadapter.Error
	if errors.As(err, &ge) {
		d.ErrorCode = ge.Code
		d.HTTPStatus = ge.HTTPStatus
		switch ge.Stage {
		case llmadapter.StageConnect:
			d.Stage = "connect"
		case llmadapter.StageHTTP:
			d.Stage = "request"
			if ge.HTTPStatus == 401 || ge.HTTPStatus == 403 {
				d.Stage = "authenticate"
			}
		case llmadapter.StageDecode:
			d.Stage = "response"
		}
		d.Retryable = ge.HTTPStatus == 429 || ge.HTTPStatus >= 500
		switch ge.HTTPStatus {
		case 400, 422:
			d.SanitizedMessage = "供应商拒绝请求参数，请核对模型 ID、模型类型和接口要求"
		case 401:
			d.SanitizedMessage = "API Key 无效或已过期"
		case 403:
			d.SanitizedMessage = "当前密钥没有该模型或接口的访问权限"
		case 404:
			d.SanitizedMessage = "接口或模型不存在，请核对基础 URL 和模型 ID"
		case 429:
			d.SanitizedMessage = "供应商限流或额度不足，请检查账户用量"
		default:
			if ge.HTTPStatus >= 500 {
				d.SanitizedMessage = "供应商服务异常，请稍后重试或检查上游模型状态"
			} else if ge.Stage == llmadapter.StageDecode {
				d.SanitizedMessage = "供应商返回内容不符合该模型类型的接口格式"
			}
		}
		switch ge.Code {
		case "MODEL_CHANNEL_UNAVAILABLE":
			d.Retryable = false
			d.SanitizedMessage = "代理当前分组没有该模型的可用通道，请联系供应商开通或修复模型通道"
		case "OCR_VERIFICATION_FAILED":
			d.SanitizedMessage = "OCR 已返回，但未正确识别测试图片，请核对模型及识别配置"
		}
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
