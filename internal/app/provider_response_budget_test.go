package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func providerBudgetSSE(protocol provider.Protocol, size int) string {
	tail := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"verified\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	if protocol == provider.ProtocolAnthropic {
		tail = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"verified\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	var wire strings.Builder
	wire.Grow(size)
	// Valid SSE keepalives count toward the wire budget, but never decoded text.
	for remaining := size - len(tail); remaining > 0; {
		if remaining < 4 {
			wire.WriteString(strings.Repeat("\n", remaining))
			break
		}
		n := min(remaining-4, 4096)
		wire.WriteString(": " + strings.Repeat("x", n) + "\n\n")
		remaining -= n + 4
	}
	wire.WriteString(tail)
	return wire.String()
}

func TestProductionAdapterResponseWireBudget(t *testing.T) {
	for _, protocol := range []provider.Protocol{provider.ProtocolOpenAICompatible, provider.ProtocolAnthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			e := NewEngineWithGateway(nil, "test", nil)
			before := e.network
			if before.MaxResponseBytes != 1<<20 {
				t.Fatalf("ordinary network budget changed: %d", before.MaxResponseBytes)
			}
			var calls atomic.Int32
			var wire atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					MaxTokens int  `json:"max_tokens"`
					Stream    bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.MaxTokens != 4096 || !request.Stream {
					t.Errorf("output budget or stream mode changed: %+v err=%v", request, err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, wire.Load().(string))
			}))
			defer server.Close()
			p := provider.Provider{ID: "budget-fixture", BaseURL: server.URL, Protocol: protocol}
			adapter, err := e.adapter(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name string
				size int
				fail bool
			}{
				{"above_web_budget", (1 << 20) + 1, false},
				{"at_provider_budget", 8 << 20, false},
				{"over_provider_budget", (8 << 20) + 1, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					body := providerBudgetSSE(protocol, tc.size)
					if len(body) != tc.size {
						t.Fatalf("fixture size=%d want=%d", len(body), tc.size)
					}
					wire.Store(body)
					previousCalls := calls.Load()
					cached, err := e.adapter(context.Background(), p)
					if err != nil || cached != adapter {
						t.Fatalf("production adapter cache miss: %v", err)
					}
					out, err := cached.Stream(context.Background(), nil, llmadapter.Request{Model: "synthetic", MaxTokens: 4096}, func(llmadapter.Delta) error { return nil })
					if calls.Load() != previousCalls+1 {
						t.Fatal("stream was retried")
					}
					if tc.fail {
						var adapterError *llmadapter.Error
						if !errors.As(err, &adapterError) || adapterError.Code != "RESPONSE_BODY_TOO_LARGE" || adapterError.Stage != llmadapter.StageStream {
							t.Fatalf("wire limit error=%v", err)
						}
					} else if err != nil || out.Message.Content != "verified" {
						t.Fatalf("valid provider response failed: text=%q err=%v", out.Message.Content, err)
					}
					if !reflect.DeepEqual(e.network, before) {
						t.Fatal("production adapter mutated shared network options")
					}
				})
			}
			if len(e.adapterCache) != 1 {
				t.Fatalf("cache entries=%d", len(e.adapterCache))
			}
			for _, invalid := range []provider.Provider{
				{ID: p.ID, BaseURL: server.URL + "?key=not-allowed", Protocol: protocol},
				{ID: p.ID, BaseURL: server.URL, Protocol: "invalid"},
			} {
				if _, err := e.adapter(context.Background(), invalid); err == nil {
					t.Fatal("invalid provider construction reused a cached adapter")
				}
				if len(e.adapterCache) != 1 || !reflect.DeepEqual(e.network, before) {
					t.Fatal("failed validation changed the cache or network options")
				}
			}
		})
	}
}

func TestProductionAdapterRetainsSSELineAndEventLimits(t *testing.T) {
	for _, protocol := range []provider.Protocol{provider.ProtocolOpenAICompatible, provider.ProtocolAnthropic} {
		for _, tc := range []struct{ name, wire, code string }{
			{"line", ": " + strings.Repeat("x", 256) + "\n\n", "RESPONSE_LINE_TOO_LARGE"},
			{"event", strings.Repeat(": "+strings.Repeat("x", 126)+"\n", 5) + "\n", "RESPONSE_EVENT_TOO_LARGE"},
		} {
			t.Run(string(protocol)+"/"+tc.name, func(t *testing.T) {
				e := NewEngineWithGateway(nil, "test", nil)
				e.network.MaxSSELineBytes, e.network.MaxSSEEventBytes = 256, 512
				before := e.network
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, tc.wire)
				}))
				defer server.Close()
				adapter, err := e.newProductionAdapter(context.Background(), provider.Provider{BaseURL: server.URL, Protocol: protocol})
				if err != nil {
					t.Fatal(err)
				}
				_, err = adapter.Stream(context.Background(), nil, llmadapter.Request{Model: "synthetic", MaxTokens: 4096}, nil)
				var adapterError *llmadapter.Error
				if !errors.As(err, &adapterError) || adapterError.Code != tc.code || adapterError.Stage != llmadapter.StageStream {
					t.Fatalf("SSE guard changed: %v", err)
				}
				if !reflect.DeepEqual(e.network, before) {
					t.Fatal("SSE options were changed")
				}
			})
		}
	}
}

type providerBudgetTransport func(*http.Request) (*http.Response, error)

func (f providerBudgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderBudgetLeavesOrdinaryNetworkLimitUnchanged(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", nil)
	before := e.network
	if _, err := e.newProductionAdapter(context.Background(), provider.Provider{BaseURL: "http://127.0.0.1:1", Protocol: provider.ProtocolOpenAICompatible}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.network, before) {
		t.Fatal("shared network options changed")
	}
	// Literal public IP plus a synthetic transport avoids DNS and network I/O.
	connector, err := networkpolicy.New(context.Background(), "https://8.8.8.8", "", e.network)
	if err != nil {
		t.Fatal(err)
	}
	connector.Client.Transport = providerBudgetTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1))), Request: r}, nil
	})
	req, err := connector.NewRequest(context.Background(), http.MethodGet, "ordinary-web-fetch", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connector.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); networkpolicy.ErrorCode(err) != networkpolicy.CodeResponseTooLarge {
		t.Fatalf("ordinary web response no longer capped at 1 MiB: %v", err)
	}
}
