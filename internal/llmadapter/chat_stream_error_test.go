package llmadapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestChatStreamRejectsErrorFramesAndPrematureEOF(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, failure := range []struct {
			name, tail, code string
			status           int
		}{
			{"EOF", "", "STREAM_INCOMPLETE", 0},
			{"rate limit", "data: {\"error\":{\"type\":\"rate_limit_error\",\"message\":\"SECRET-CANARY\"}}\n\n", "STREAM_RATE_LIMITED", 0},
			{"overloaded", "event: error\ndata: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"SECRET-CANARY\"}}\n\n", "STREAM_OVERLOADED", 0},
			{"unknown", "event: error\ndata: {\"error\":{\"type\":\"SECRET-CANARY\",\"message\":\"SECRET-CANARY\"}}\n\n", "UPSTREAM_STREAM_FAILED", 0},
			{"malformed error", "event: error\ndata: SECRET-CANARY\n\n", "UPSTREAM_STREAM_FAILED", 0},
		} {
			t.Run(protocol+"/"+failure.name, func(t *testing.T) {
				prefix := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"
				if protocol == "anthropic" {
					prefix = "event: message_start\ndata: {\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}\n\nevent: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"
				}
				body := prefix + failure.tail
				if failure.tail != "" {
					if protocol == "anthropic" {
						body += "event: message_stop\ndata: {}\n\n"
					} else {
						body += "data: [DONE]\n\n"
					}
				}
				f := &fakeConnector{responses: []*http.Response{response(200, body)}}
				var a Adapter = NewOpenAI(f, Options{})
				if protocol == "anthropic" {
					a = NewAnthropic(f, Options{})
				}
				var text string
				out, err := a.Stream(context.Background(), []byte("SECRET-CANARY"), Request{MaxAttempts: 3}, func(d Delta) error { text += d.Text; return nil })
				var ge *Error
				if !errors.As(err, &ge) || ge.Code != failure.code || ge.HTTPStatus != failure.status || ge.Stage != StageStream {
					t.Fatalf("error=%+v want=%s/%d/stream", ge, failure.code, failure.status)
				}
				if strings.Contains(err.Error(), "SECRET-CANARY") || text != "partial" || out.Message.Content != text || out.Usage.TotalTokens != 5 || len(f.requests) != 1 {
					t.Fatalf("partial response, usage, secrecy or no-retry contract lost: out=%+v err=%v calls=%d", out, err, len(f.requests))
				}
			})
		}
	}
}

func TestOpenAIIncompleteStreamDoesNotEmitExecutableToolCall(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"workspace_write\",\"arguments\":\"{}\"}}]}}]}\n\n"
	for _, complete := range []bool{false, true} {
		stream := body
		if complete {
			stream += "data: [DONE]\n\n"
		}
		f := &fakeConnector{responses: []*http.Response{response(200, stream)}}
		calls := 0
		out, err := NewOpenAI(f, Options{}).Stream(context.Background(), nil, Request{}, func(d Delta) error {
			if d.ToolCall != nil {
				calls++
			}
			return nil
		})
		if complete {
			if err != nil || calls != 1 || len(out.Message.ToolCalls) != 1 {
				t.Fatalf("completed tool call lost: %+v %v", out, err)
			}
		} else if err == nil || calls != 0 || len(out.Message.ToolCalls) != 0 {
			t.Fatalf("incomplete stream exposed tool: %+v %v", out, err)
		}
	}
}

type brokenStreamConnector struct {
	fakeConnector
	readErr error
}

func (f *brokenStreamConnector) ReadSSE(io.Reader) ([]byte, bool, error) {
	return nil, false, f.readErr
}

func TestChatStreamReadErrorsRetainStageAndClass(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, tc := range []struct {
			err  error
			code string
		}{
			{io.ErrUnexpectedEOF, "STREAM_INCOMPLETE"},
			{context.DeadlineExceeded, "TIMEOUT"},
			{context.Canceled, "CANCELLED"},
			{&networkpolicy.Error{Code: networkpolicy.CodeResponseTooLarge, Err: errors.New("SECRET-CANARY")}, "RESPONSE_TOO_LARGE"},
			{&networkpolicy.Error{Code: networkpolicy.CodeResponseTooLarge, Op: "read response", Err: errors.New("SECRET-CANARY")}, "RESPONSE_BODY_TOO_LARGE"},
			{&networkpolicy.Error{Code: networkpolicy.CodeResponseTooLarge, Op: "read SSE line", Err: errors.New("SECRET-CANARY")}, "RESPONSE_LINE_TOO_LARGE"},
			{&networkpolicy.Error{Code: networkpolicy.CodeResponseTooLarge, Op: "read SSE event", Err: errors.New("SECRET-CANARY")}, "RESPONSE_EVENT_TOO_LARGE"},
			{&networkpolicy.Error{Code: networkpolicy.CodeResponseTooLarge, Op: "SECRET-CANARY", Err: errors.New("SECRET-CANARY")}, "RESPONSE_TOO_LARGE"},
			{&networkpolicy.Error{Code: networkpolicy.CodeConnectionRefused, Err: errors.New("SECRET-CANARY")}, "CONNECTION_REFUSED"},
		} {
			t.Run(protocol+"/"+tc.code, func(t *testing.T) {
				f := &brokenStreamConnector{fakeConnector: fakeConnector{responses: []*http.Response{response(200, "")}}, readErr: tc.err}
				var a Adapter = NewOpenAI(f, Options{})
				if protocol == "anthropic" {
					a = NewAnthropic(f, Options{})
				}
				_, err := a.Stream(context.Background(), nil, Request{}, nil)
				var ge *Error
				if !errors.As(err, &ge) || ge.Code != tc.code || ge.Stage != StageStream || strings.Contains(err.Error(), "SECRET-CANARY") {
					t.Fatalf("error=%+v", ge)
				}
			})
		}
	}
}
