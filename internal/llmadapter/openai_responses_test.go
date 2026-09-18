package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/modelfit"
)

func responsesFake(bodies ...string) *fakeConnector {
	f := &fakeConnector{}
	for _, b := range bodies {
		f.responses = append(f.responses, response(200, b))
	}
	return f
}

func TestResponsesCompleteMapsInputToolsAndUsage(t *testing.T) {
	f := responsesFake(`{"id":"resp_1","status":"completed","output":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"think "}],"content":[{"type":"reasoning_text","text":"more"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},
		{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":1}"}
	],"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14,"input_tokens_details":{"cached_tokens":6}}}`)
	a := NewOpenAIResponses(f, Options{})
	out, err := a.Complete(context.Background(), []byte("k"), Request{
		Model:     "doubao-seed-2.1",
		MaxTokens: 4,
		Messages: []Message{
			{Role: RoleSystem, Content: "be terse"},
			{Role: RoleUser, Content: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Name: "lookup", Arguments: json.RawMessage(`{"q":0}`)}}},
			{Role: RoleTool, ToolCallID: "call_0", Content: "42"},
		},
		Tools: []ToolDefinition{{Name: "lookup", Description: "d", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Message.Content != "hello" || out.Reasoning != "think more" || out.FinishReason != FinishReasonToolCalls {
		t.Fatalf("out=%+v", out)
	}
	if len(out.Message.ToolCalls) != 1 || out.Message.ToolCalls[0].ID != "call_1" || out.Message.ToolCalls[0].Name != "lookup" {
		t.Fatalf("tool calls=%+v", out.Message.ToolCalls)
	}
	if out.Usage.InputTokens != 10 || out.Usage.TotalTokens != 14 || out.Usage.CachedInputTokens != 6 || !out.Usage.CacheUsageReported {
		t.Fatalf("usage=%+v", out.Usage)
	}
	req := f.requests[0]
	if req.URL.Path != "/v1/responses" || !f.authSeen[0] {
		t.Fatalf("path=%q auth=%v", req.URL.Path, f.authSeen)
	}
	body, _ := io.ReadAll(req.Body)
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["max_output_tokens"] != float64(responsesMinOutputTokens) {
		t.Fatalf("max_output_tokens must be clamped to %d, body=%s", responsesMinOutputTokens, body)
	}
	if wire["store"] != false || wire["stream"] != nil {
		t.Fatalf("store/stream body=%s", body)
	}
	input := wire["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("input=%v", input)
	}
	if item := input[0].(map[string]any); item["role"] != "system" || item["content"] != "be terse" {
		t.Fatalf("system item=%v", item)
	}
	if item := input[2].(map[string]any); item["type"] != "function_call" || item["call_id"] != "call_0" || item["arguments"] != `{"q":0}` {
		t.Fatalf("function_call item=%v", item)
	}
	if item := input[3].(map[string]any); item["type"] != "function_call_output" || item["call_id"] != "call_0" || item["output"] != "42" {
		t.Fatalf("function_call_output item=%v", item)
	}
	tools := wire["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "lookup" || tool["function"] != nil {
		t.Fatalf("responses tools are flat, got %v", tool)
	}
	for _, forbidden := range []string{"messages", "max_tokens", "reasoning_content", "enable_thinking", "thinking"} {
		if _, ok := wire[forbidden]; ok {
			t.Fatalf("chat-completions field %q leaked into responses body", forbidden)
		}
	}
}

func TestResponsesDeepModeUsesReasoningEffortNotArkThinking(t *testing.T) {
	f := responsesFake(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	_, err := NewOpenAIResponses(f, Options{}).Complete(context.Background(), nil, Request{
		Model:     "m",
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		Effective: &modelfit.EffectiveParameters{ThinkingType: "enabled", Effort: "high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["thinking"]; ok {
		t.Fatalf("OpenAI Responses rejects Ark thinking: %s", body)
	}
	reasoning, _ := wire["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("deep mode must send reasoning.effort: %s", body)
	}
}

func TestResponsesImagesBecomeInputImageParts(t *testing.T) {
	f := responsesFake(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a cat"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	_, err := NewOpenAIResponses(f, Options{}).Complete(context.Background(), nil, Request{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: "what is this"}},
		Images:   []Image{{MIME: "image/jpeg", Data: []byte{1, 2, 3}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	if !strings.Contains(string(body), `"type":"input_image"`) || !strings.Contains(string(body), `"image_url":"data:image/jpeg;base64,AQID"`) || !strings.Contains(string(body), `"type":"input_text"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestResponsesStreamAssemblesTextReasoningToolsAndUsage(t *testing.T) {
	frames := []string{
		`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"r"}}`,
		`event: response.reasoning_summary_text.delta` + "\n" + `data: {"type":"response.reasoning_summary_text.delta","delta":"hmm"}`,
		`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":"hel"}`,
		`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":"lo"}`,
		`event: response.output_item.added` + "\n" + `data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_9","name":"lookup","arguments":""}}`,
		`event: response.function_call_arguments.delta` + "\n" + `data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"q\":"}`,
		`event: response.function_call_arguments.delta` + "\n" + `data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"7}"}`,
		`event: response.function_call_arguments.done` + "\n" + `data: {"type":"response.function_call_arguments.done","output_index":1,"arguments":"{\"q\":7}"}`,
		`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message"},{"type":"function_call","call_id":"call_9","name":"lookup","arguments":"{\"q\":7}"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`,
	}
	f := responsesFake(strings.Join(frames, "\n\n") + "\n\n")
	var text, reasoning string
	var tools []ToolCall
	var usage *Usage
	out, err := NewOpenAIResponses(f, Options{}).Stream(context.Background(), nil, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Tools: []ToolDefinition{{Name: "lookup", Schema: json.RawMessage(`{}`)}}}, func(d Delta) error {
		text += d.Text
		reasoning += d.Reasoning
		if d.ToolCall != nil {
			tools = append(tools, *d.ToolCall)
		}
		if d.Usage != nil {
			usage = d.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" || reasoning != "hmm" || out.Message.Content != "hello" || out.Reasoning != "hmm" {
		t.Fatalf("text=%q reasoning=%q out=%+v", text, reasoning, out)
	}
	if len(tools) != 1 || tools[0].ID != "call_9" || string(tools[0].Arguments) != `{"q":7}` || len(out.Message.ToolCalls) != 1 {
		t.Fatalf("tools=%+v out=%+v", tools, out.Message.ToolCalls)
	}
	if usage == nil || usage.TotalTokens != 5 || out.Usage.TotalTokens != 5 {
		t.Fatalf("usage=%+v out=%+v", usage, out.Usage)
	}
	if out.FinishReason != FinishReasonToolCalls {
		t.Fatalf("finish=%q", out.FinishReason)
	}
	body, _ := io.ReadAll(f.requests[0].Body)
	if !strings.Contains(string(body), `"stream":true`) {
		t.Fatalf("stream flag missing: %s", body)
	}
}

func TestResponsesStreamIncompleteIsLength(t *testing.T) {
	frames := `data: {"type":"response.output_text.delta","delta":"par"}` + "\n\n" +
		`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":1,"output_tokens":16,"total_tokens":17}}}` + "\n\n"
	out, err := NewOpenAIResponses(responsesFake(frames), Options{}).Stream(context.Background(), nil, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	if err != nil || out.Message.Content != "par" || out.FinishReason != FinishReasonLength {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestResponsesStreamEndsWithoutTerminalIsIncomplete(t *testing.T) {
	frames := `data: {"type":"response.output_text.delta","delta":"par"}` + "\n\n"
	_, err := NewOpenAIResponses(responsesFake(frames), Options{}).Stream(context.Background(), nil, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	var ge *Error
	if !errors.As(err, &ge) || ge.Code != "STREAM_INCOMPLETE" {
		t.Fatalf("err=%v", err)
	}
}

func TestResponsesStreamInBandErrorIsClassified(t *testing.T) {
	frames := `event: error` + "\n" + `data: {"type":"error","error":{"type":"invalid_request_error","code":"model_not_found","message":"CANARY"}}` + "\n\n"
	_, err := NewOpenAIResponses(responsesFake(frames), Options{}).Stream(context.Background(), nil, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	var ge *Error
	if !errors.As(err, &ge) || ge.Code == "" || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err=%v", err)
	}
}

func TestResponsesTestConnectionAndDiscover(t *testing.T) {
	f := responsesFake(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}]}`, `{"data":[{"id":"b"},{"id":"a"}]}`)
	a := NewOpenAIResponses(f, Options{})
	if err := a.TestConnection(context.Background(), []byte("k"), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "ping"}}, MaxTokens: 1}); err != nil {
		t.Fatal(err)
	}
	if f.requests[0].URL.Path != "/v1/responses" {
		t.Fatalf("path=%q", f.requests[0].URL.Path)
	}
	probe, _ := io.ReadAll(f.requests[0].Body)
	var probeWire map[string]any
	if err := json.Unmarshal(probe, &probeWire); err != nil {
		t.Fatal(err)
	}
	if probeWire["store"] != false {
		t.Fatalf("diagnostic must not persist the ping: %s", probe)
	}
	d, err := a.Discover(context.Background(), []byte("k"))
	if err != nil || len(d.Models) != 2 || d.Models[0].ID != "a" || f.requests[1].URL.Path != "/v1/models" {
		t.Fatalf("discovery=%+v err=%v path=%q", d, err, f.requests[1].URL.Path)
	}
}

func TestResponsesBadRequestSanitizesToolSchemaOnce(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{
		response(400, `{"error":{"message":"additionalProperties not supported"}}`),
		response(200, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`),
	}}
	out, err := NewOpenAIResponses(f, Options{}).Complete(context.Background(), nil, Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Tools: []ToolDefinition{{Name: "t", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","minLength":1}}}`)}}})
	if err != nil || out.Message.Content != "ok" || len(f.requests) != 2 {
		t.Fatalf("out=%+v err=%v requests=%d", out, err, len(f.requests))
	}
	second, _ := io.ReadAll(f.requests[1].Body)
	if strings.Contains(string(second), "additionalProperties") || strings.Contains(string(second), "minLength") {
		t.Fatalf("second attempt must carry sanitized schema: %s", second)
	}
}

func TestResponsesUpstreamStatusIsSafeError(t *testing.T) {
	// Same contract as chat/completions: the bounded upstream reason is
	// surfaced (operators need "model not found"), the secret never is.
	f := &fakeConnector{responses: []*http.Response{response(401, `{"error":{"message":"bad key"}}`)}}
	_, err := NewOpenAIResponses(f, Options{}).Complete(context.Background(), []byte("SECRET-CANARY"), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	var ge *Error
	if !errors.As(err, &ge) || ge.HTTPStatus != 401 || ge.Code != "HTTP_401" || strings.Contains(err.Error(), "SECRET-CANARY") || len(f.requests) != 1 {
		t.Fatalf("err=%v requests=%d", err, len(f.requests))
	}
}
