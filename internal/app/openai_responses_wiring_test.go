package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// TestProductionAdapterRoutesOpenAIResponsesProtocol proves a stored
// openai_responses provider is built into the Responses adapter: the
// connection test and chat land on POST {base}/responses with a Responses
// body, and pasted full endpoints are normalised before the route is added.
func TestProductionAdapterRoutesOpenAIResponsesProtocol(t *testing.T) {
	var paths atomic.Value
	paths.Store([]string(nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths.Store(append(paths.Load().([]string), r.URL.Path))
		body, _ := io.ReadAll(r.Body)
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil || wire["input"] == nil || wire["messages"] != nil {
			t.Errorf("responses body expected, got %s (err=%v)", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	e := NewEngineWithGateway(nil, "test", nil)
	for _, base := range []string{server.URL + "/api/plan/v3", server.URL + "/api/plan/v3/responses"} {
		adapter, err := e.newProductionAdapter(context.Background(), provider.Provider{ID: "ark-plan", BaseURL: base, Protocol: provider.ProtocolOpenAIResponses})
		if err != nil {
			t.Fatalf("base %q: %v", base, err)
		}
		responses, ok := adapter.(*llmadapter.OpenAIResponses)
		if !ok {
			t.Fatalf("base %q built %T, want *llmadapter.OpenAIResponses", base, adapter)
		}
		req := llmadapter.Request{Model: "doubao-seed-2.1", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "ping"}}, MaxTokens: 1, MaxAttempts: 1}
		if err := responses.TestConnection(context.Background(), []byte("k"), req); err != nil {
			t.Fatalf("base %q: test connection: %v", base, err)
		}
		out, err := adapter.Complete(context.Background(), []byte("k"), req)
		if err != nil || out.Message.Content != "pong" {
			t.Fatalf("base %q: complete out=%+v err=%v", base, out, err)
		}
	}
	got := paths.Load().([]string)
	if len(got) != 4 {
		t.Fatalf("expected 4 upstream calls, got %v", got)
	}
	for _, p := range got {
		if p != "/api/plan/v3/responses" {
			t.Fatalf("responses protocol must hit {base}/responses exactly once per call, got %v", got)
		}
	}
}
