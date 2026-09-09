package llmadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCompletionFinishReasonIsBounded(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want FinishReason
	}{
		{"", ""},
		{"stop", FinishReasonStop},
		{"end_turn", FinishReasonStop},
		{"stop_sequence", FinishReasonStop},
		{"length", FinishReasonLength},
		{"max_tokens", FinishReasonLength},
		{"model_context_window_exceeded", FinishReasonLength},
		{"content_filter", FinishReasonContentFilter},
		{"refusal", FinishReasonContentFilter},
		{"tool_calls", FinishReasonToolCalls},
		{"function_call", FinishReasonToolCalls},
		{"tool_use", FinishReasonToolCalls},
		{"pause_turn", FinishReasonOther},
		{strings.Repeat("SECRET-CANARY", 1000), FinishReasonOther},
	} {
		got := normalizeFinishReason(&tc.raw)
		if got != tc.want {
			t.Fatalf("classification=%q want=%q", got, tc.want)
		}
		encoded, err := json.Marshal(Response{FinishReason: got})
		if err != nil || strings.Contains(string(encoded), "SECRET-CANARY") {
			t.Fatalf("raw finish reason escaped normalization: %s %v", encoded, err)
		}
	}
	if normalizeFinishReason(nil) != "" {
		t.Fatal("missing reason invented completion")
	}
	for _, reason := range []FinishReason{FinishReasonStop, FinishReasonLength, FinishReasonContentFilter} {
		out := Response{FinishReason: reason}
		empty, stop := "", "stop"
		out.recordFinishReason(nil)
		out.recordFinishReason(&empty)
		if out.FinishReason != reason {
			t.Fatal("empty frame erased finish reason")
		}
		out.recordFinishReason(&stop)
		if out.FinishReason != reason {
			t.Fatal("conflicting stop erased explicit truncation")
		}
	}
}

func completionFinishFixture(protocol, raw string, stream bool) string {
	field := ""
	if raw != "" {
		key := "finish_reason"
		if protocol == "anthropic" {
			key = "stop_reason"
		}
		field = fmt.Sprintf(",%q:%s", key, raw)
	}
	if protocol == "anthropic" {
		usage := `{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":7,"cache_creation_input_tokens":0}`
		if !stream {
			return `{"content":[{"type":"thinking","thinking":"thought"},{"type":"text","text":"done"}],"usage":` + usage + field + `}`
		}
		return "event: message_start\ndata: " + `{"message":{"stop_reason":null,"usage":` + usage + `}}` + "\n\n" +
			"event: content_block_delta\ndata: " + `{"delta":{"type":"thinking_delta","thinking":"thought"}}` + "\n\n" +
			"event: content_block_delta\ndata: " + `{"delta":{"type":"text_delta","text":"done"}}` + "\n\n" +
			"event: message_delta\ndata: " + `{"delta":{"stop_sequence":null` + field + `},"usage":{"output_tokens":2}}` + "\n\n" +
			"event: message_delta\ndata: " + `{"usage":{"output_tokens":2}}` + "\n\nevent: message_stop\ndata: {}\n\n"
	}
	usage := `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":7}}`
	if !stream {
		return `{"choices":[{"message":{"role":"assistant","content":"done","reasoning_content":"thought"}` + field + `}],"usage":` + usage + `}`
	}
	return "data: " + `{"choices":[{"delta":{"content":"done","reasoning_content":"thought"},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"choices":[{"delta":{}` + field + `}]}` + "\n\n" +
		"data: " + `{"choices":[],"usage":` + usage + `}` + "\n\ndata: [DONE]\n\n"
}

func TestCompletionFinishReasonCompleteAndStream(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, tc := range []struct {
			name, openai, anthropic string
			want                    FinishReason
		}{
			{"stop", `"stop"`, `"end_turn"`, FinishReasonStop},
			{"length", `"length"`, `"max_tokens"`, FinishReasonLength},
			{"filter", `"content_filter"`, `"refusal"`, FinishReasonContentFilter},
			{"tool", `"tool_calls"`, `"tool_use"`, FinishReasonToolCalls},
			{"unknown", `"SECRET-CANARY"`, `"SECRET-CANARY"`, FinishReasonOther},
			{"null", `null`, `null`, ""},
			{"absent", "", "", ""},
		} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", protocol, tc.name, stream), func(t *testing.T) {
					raw := tc.openai
					if protocol == "anthropic" {
						raw = tc.anthropic
					}
					f := &fakeConnector{responses: []*http.Response{response(200, completionFinishFixture(protocol, raw, stream))}}
					var a Adapter = NewOpenAI(f, Options{})
					if protocol == "anthropic" {
						a = NewAnthropic(f, Options{})
					}
					var out Response
					var err error
					if stream {
						out, err = a.Stream(context.Background(), nil, Request{}, nil)
					} else {
						out, err = a.Complete(context.Background(), nil, Request{})
					}
					wantUsage := Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CachedInputTokens: 7, CacheUsageReported: true}
					if err != nil || out.FinishReason != tc.want || out.Message.Content != "done" || out.Reasoning != "thought" || out.Usage != wantUsage || len(f.requests) != 1 {
						t.Fatalf("finish, content, accounting or single call lost: %+v %v", out, err)
					}
					encoded, err := json.Marshal(out)
					if err != nil || strings.Contains(string(encoded), "SECRET-CANARY") {
						t.Fatal("raw provider reason was exposed")
					}
				})
			}
		}
	}
}

func TestCompletionFinishReasonDoesNotReplaceStreamTerminator(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			raw, terminal := `"stop"`, "data: [DONE]\n\n"
			if protocol == "anthropic" {
				raw, terminal = `"end_turn"`, "event: message_stop\ndata: {}\n\n"
			}
			body := strings.TrimSuffix(completionFinishFixture(protocol, raw, true), terminal)
			f := &fakeConnector{responses: []*http.Response{response(200, body)}}
			var a Adapter = NewOpenAI(f, Options{})
			if protocol == "anthropic" {
				a = NewAnthropic(f, Options{})
			}
			out, err := a.Stream(context.Background(), nil, Request{}, nil)
			var ge *Error
			if !errors.As(err, &ge) || ge.Code != "STREAM_INCOMPLETE" || out.FinishReason != FinishReasonStop || len(f.requests) != 1 {
				t.Fatalf("finish reason bypassed terminal safety: %+v %v", out, err)
			}
		})
	}
}

func TestCompletionFinishReasonSurvivesConsumerCancellation(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			body := "data: " + `{"choices":[{"delta":{"content":"partial"},"finish_reason":"length"}]}` + "\n\ndata: [DONE]\n\n"
			if protocol == "anthropic" {
				body = "event: message_delta\ndata: " + `{"delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":2}}` + "\n\nevent: message_stop\ndata: {}\n\n"
			}
			f := &fakeConnector{responses: []*http.Response{response(200, body)}}
			var a Adapter = NewOpenAI(f, Options{})
			if protocol == "anthropic" {
				a = NewAnthropic(f, Options{})
			}
			out, err := a.Stream(context.Background(), nil, Request{}, func(Delta) error { return context.Canceled })
			if !errors.Is(err, context.Canceled) || out.FinishReason != FinishReasonLength || len(f.requests) != 1 {
				t.Fatalf("received finish reason lost on cancellation: %+v %v", out, err)
			}
		})
	}
}
