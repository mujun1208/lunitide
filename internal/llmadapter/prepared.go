package llmadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/lunitide/lunitide/internal/modelfit"
)

const maxPreparedRequestBytes = 5 << 20

type PreparedRequest struct {
	Body      json.RawMessage
	Digest    string
	Target    modelfit.TargetIdentity
	Effective modelfit.EffectiveParameters
	Stream    bool
}

type ResponseMetadata struct {
	ModelReturned     *string
	ModelRevision     *string
	ProviderRequestID *string
}

type AttemptHooks interface {
	BeforeSend(context.Context, PreparedRequest) (attemptID string, err error)
	MarkDispatched(context.Context, string) error
	AfterAttempt(context.Context, string, AttemptResult) error
}

type AttemptResult struct {
	Dispatched     bool
	State          string
	Usage          Usage
	UsageIntegrity string
	ResponseDigest string
	Metadata       ResponseMetadata
}

type AttemptClassInput struct {
	HTTPStatus int
	Kind       string
	Arguments  json.RawMessage
}

type AttemptClass struct {
	Retryable      bool
	ExecuteTools   bool
	UsageIntegrity string
	SideEffect     string
	HTTPSends      int
}

func ClassifyAttempt(in AttemptClassInput) AttemptClass {
	out := AttemptClass{SideEffect: "none"}
	switch in.Kind {
	case "rate":
		out.Retryable = retryableStatus(in.HTTPStatus)
	case "disconnect":
		out.Retryable = true
		out.UsageIntegrity = "unknown"
	case "truncated_sse":
		out.UsageIntegrity = "unknown"
	case "missing_usage":
		out.UsageIntegrity = "missing"
	case "cancel_before_send":
		out.HTTPSends = 0
	}
	return out
}

type CatalogTool struct {
	Name       string
	Version    string
	Schema     json.RawMessage
	Attachment bool
	Trust      string
}

type CompiledToolCatalog struct {
	Digest string
	Tools  []CatalogTool
}

func CompileToolCatalog(taskID, modelID string, tools []CatalogTool, modelText string) CompiledToolCatalog {
	_ = modelText
	out := append([]CatalogTool(nil), tools...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := range out {
		if out[i].Attachment {
			out[i].Trust = "untrusted"
		}
	}
	h := sha256.New()
	_, _ = h.Write([]byte(taskID))
	h.Write([]byte{0})
	_, _ = h.Write([]byte(modelID))
	for _, tool := range out {
		_, _ = h.Write([]byte(tool.Name))
		h.Write([]byte{0})
		_, _ = h.Write([]byte(tool.Version))
		h.Write([]byte{0})
		_, _ = h.Write(tool.Schema)
		h.Write([]byte{0})
	}
	return CompiledToolCatalog{Digest: hex.EncodeToString(h.Sum(nil)), Tools: out}
}

func PrepareChat(req Request, target modelfit.TargetIdentity, replay modelfit.ReplayDecision, profile modelfit.ModelProfile, stream bool) (PreparedRequest, error) {
	_ = replay
	eff, err := modelfit.CompileParameters(profile, modelfit.ModelIntent{
		Mode:           req.Mode,
		StrictTools:    req.StrictTools,
		OutputTokenCap: int64(req.MaxTokens),
	})
	if err != nil {
		return PreparedRequest{}, err
	}
	req.Effective = &eff
	req.Target = target
	req = attachEfficientRequest(req, Options{})
	wn := buildWireNames(req.Tools, openAIToolNameMax)
	return freezeOpenAIRequest(req, wn, stream, false, preparedByteCap(Options{}), target, eff)
}

func preparedByteCap(o Options) int {
	o = defaults(o)
	if o.MaxRequestBytes > 0 && o.MaxRequestBytes < maxPreparedRequestBytes {
		return o.MaxRequestBytes
	}
	return maxPreparedRequestBytes
}

func freezeOpenAIRequest(in Request, wn *wireNames, stream bool, rawImageURL bool, maxBytes int, target modelfit.TargetIdentity, eff modelfit.EffectiveParameters) (PreparedRequest, error) {
	return freezeOpenAIPayload(buildOpenAIRequest(in, wn, stream, rawImageURL), maxBytes, target, eff, stream)
}

func freezeOpenAIPayload(p openAIRequest, maxBytes int, target modelfit.TargetIdentity, eff modelfit.EffectiveParameters, stream bool) (PreparedRequest, error) {
	raw, err := marshalBoundedBytes(p, maxBytes)
	if err != nil {
		return PreparedRequest{}, err
	}
	sum := sha256.Sum256(raw)
	return PreparedRequest{
		Body:      raw,
		Digest:    hex.EncodeToString(sum[:]),
		Target:    target,
		Effective: eff,
		Stream:    stream,
	}, nil
}
