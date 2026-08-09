package gateway

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// OpenAIEndpoint constructs the mandatory network-policy connector and applies
// OpenAI-compatible base path semantics: a base already ending in /v1 is not
// given a second /v1 segment.
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

func versionPath(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && strings.EqualFold(path.Base(strings.TrimSuffix(u.Path, "/")), "v1") {
		return ""
	}
	return "v1"
}
