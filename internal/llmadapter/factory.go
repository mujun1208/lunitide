package llmadapter

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// OpenAIEndpoint constructs the mandatory network-policy connector and applies
// OpenAI-compatible base path semantics: a base already ending in a version
// segment (/v1, /v3, …) is not given a second /v1. Volcengine Ark Plan uses
// /api/plan/v3; appending /v1 there produced /api/plan/v3/v1/chat/completions.
func OpenAIEndpoint(ctx context.Context, rawBase string, network networkpolicy.Options, gateway Options) (*OpenAI, error) {
	c, err := networkpolicy.New(ctx, rawBase, versionPath(rawBase), network)
	if err != nil {
		return nil, err
	}
	return NewOpenAI(c, gateway), nil
}

// AnthropicEndpoint targets the Messages API below /v1 with the same
// idempotent version-path handling.
func AnthropicEndpoint(ctx context.Context, rawBase string, network networkpolicy.Options, gateway Options) (*Anthropic, error) {
	c, err := networkpolicy.New(ctx, rawBase, versionPath(rawBase), network)
	if err != nil {
		return nil, err
	}
	return NewAnthropic(c, gateway), nil
}

// OpenAIResponsesEndpoint targets POST {base}/{version}/responses with the
// same version-path rule: Ark's /api/v3 and /api/plan/v3 already carry the
// version, OpenAI and Bailian bases (/v1, /compatible-mode/v1) do too.
func OpenAIResponsesEndpoint(ctx context.Context, rawBase string, network networkpolicy.Options, gateway Options) (*OpenAIResponses, error) {
	c, err := networkpolicy.New(ctx, rawBase, versionPath(rawBase), network)
	if err != nil {
		return nil, err
	}
	return NewOpenAIResponses(c, gateway), nil
}

func versionPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "v1"
	}
	if isAPIVersionSegment(path.Base(strings.TrimSuffix(u.Path, "/"))) {
		return ""
	}
	return "v1"
}

func isAPIVersionSegment(s string) bool {
	if len(s) < 2 || (s[0] != 'v' && s[0] != 'V') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
