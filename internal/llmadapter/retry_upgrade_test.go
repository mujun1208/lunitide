package llmadapter

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// failFirstConnector fails the first Do calls with a connection-refused
// networkpolicy error, then delegates to the embedded fakeConnector.
type failFirstConnector struct {
	fakeConnector
	failures int
}

func (f *failFirstConnector) Do(r *http.Request) (*http.Response, error) {
	if f.failures > 0 {
		f.failures--
		f.mu.Lock()
		f.requests = append(f.requests, r.Clone(r.Context()))
		f.mu.Unlock()
		return nil, &networkpolicy.Error{Code: networkpolicy.CodeConnectionRefused, Op: "dial", Err: errors.New("connection refused")}
	}
	return f.fakeConnector.Do(r)
}

const openAIHelloStream = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n"

// TestRetryDelayEqualJitter pins the equal-jitter backoff: the exponential
// delay lands in [50%, 100%) of its computed value with real variance, while
// a server-provided Retry-After header stays exact.
func TestRetryDelayEqualJitter(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 60; i++ {
		d := retryDelay(nil, 2, 100*time.Millisecond)
		if d < 100*time.Millisecond || d >= 200*time.Millisecond {
			t.Fatalf("jittered delay out of [100ms,200ms): %v", d)
		}
		seen[d] = true
	}
	if len(seen) < 10 {
		t.Fatalf("backoff shows no jitter variance: %d distinct values", len(seen))
	}
	r := response(429, "")
	r.Header.Set("Retry-After", "2")
	for i := 0; i < 10; i++ {
		if d := retryDelay(r, 3, time.Second); d != 2*time.Second {
			t.Fatalf("Retry-After must stay unjittered: %v", d)
		}
	}
}

// TestStreamRetriesConnectFailure: a streaming request whose connection is
// refused retries and succeeds on the next attempt — the caller sees one
// clean stream.
func TestStreamRetriesConnectFailure(t *testing.T) {
	for name, build := range map[string]func(Connector) Adapter{
		"openai":     func(c Connector) Adapter { return NewOpenAI(c, Options{RetryBase: time.Millisecond}) },
		"anthropic": func(c Connector) Adapter { return NewAnthropic(c, Options{RetryBase: time.Millisecond}) },
	} {
		t.Run(name, func(t *testing.T) {
			f := &failFirstConnector{failures: 1, fakeConnector: fakeConnector{responses: []*http.Response{response(200, openAIHelloStream)}}}
			if name == "anthropic" {
				f.responses = []*http.Response{response(200, "event: message_start\ndata: {\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}\n\nevent: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: message_stop\ndata: {}\n\n")}
			}
			a := build(f)
			var text string
			out, err := a.Stream(context.Background(), nil, Request{MaxAttempts: 2}, func(d Delta) error { text += d.Text; return nil })
			if err != nil || text != "hello" || out.Message.Content != "hello" {
				t.Fatalf("stream retry lost: out=%+v err=%v text=%q", out, err, text)
			}
			if len(f.requests) != 2 {
				t.Fatalf("want 2 attempts, got %d", len(f.requests))
			}
		})
	}
}

// TestStreamRetriesRetryableStatusWithoutIdempotencyKey: a 429 received
// before any stream byte is zero-delivery by construction, so a streaming
// retry needs no idempotency key (non-streaming keeps the key contract).
func TestStreamRetriesRetryableStatusWithoutIdempotencyKey(t *testing.T) {
	f := &fakeConnector{responses: []*http.Response{response(429, ""), response(200, openAIHelloStream)}}
	a := NewOpenAI(f, Options{RetryBase: time.Millisecond})
	var text string
	out, err := a.Stream(context.Background(), nil, Request{MaxAttempts: 2}, func(d Delta) error { text += d.Text; return nil })
	if err != nil || text != "hello" || out.Message.Content != "hello" {
		t.Fatalf("stream status retry lost: out=%+v err=%v text=%q", out, err, text)
	}
	if len(f.requests) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(f.requests))
	}
}

// TestStreamRetriesZeroDeliverableFault: a stream that ends incomplete while
// having delivered nothing (usage/keepalive frames only) is retried in the
// same turn; the retry's output is the only thing the caller ever sees.
func TestStreamRetriesZeroDeliverableFault(t *testing.T) {
	incomplete := "data: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, incomplete), response(200, openAIHelloStream)}}
	a := NewOpenAI(f, Options{RetryBase: time.Millisecond})
	var text string
	out, err := a.Stream(context.Background(), nil, Request{MaxAttempts: 2}, func(d Delta) error { text += d.Text; return nil })
	if err != nil || text != "hello" || out.Message.Content != "hello" {
		t.Fatalf("zero-deliverable retry lost: out=%+v err=%v text=%q", out, err, text)
	}
	if len(f.requests) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(f.requests))
	}
}

// TestRetryableStreamFaultTable pins which stream fault classes qualify for
// a zero-deliverable same-turn retry.
func TestRetryableStreamFaultTable(t *testing.T) {
	for _, tc := range []struct {
		code string
		want bool
	}{
		{"REASONING_STALL", true},
		{"TIMEOUT", true},
		{"STREAM_INCOMPLETE", true},
		{"STREAM_RATE_LIMITED", true},
		{"STREAM_UNAVAILABLE", true},
		{"STREAM_OVERLOADED", true},
		{"STREAM_BAD_REQUEST", false},
		{"STREAM_AUTHENTICATION_FAILED", false},
		{"CANCELLED", false},
		{"MALFORMED_RESPONSE", false},
		{"RESPONSE_BODY_TOO_LARGE", false},
	} {
		if got := retryableStreamFault(safeError(tc.code, StageStream, 0, "test")); got != tc.want {
			t.Fatalf("%s: retryable=%v want %v", tc.code, got, tc.want)
		}
	}
	if retryableStreamFault(errors.New("plain")) {
		t.Fatal("plain errors must not be retryable stream faults")
	}
}

// TestAttemptsNoLongerForcesSingleStreamAttempt guards the un-degradation:
// a streaming request with MaxAttempts honors the budget instead of being
// clamped to one attempt.
func TestAttemptsNoLongerForcesSingleStreamAttempt(t *testing.T) {
	if got := attempts(Options{MaxAttempts: 3}, Request{MaxAttempts: 2}, true); got != 2 {
		t.Fatalf("stream attempts=%d want 2", got)
	}
	if got := attempts(Options{MaxAttempts: 3}, Request{}, true); got != 3 {
		t.Fatalf("stream attempts=%d want 3", got)
	}
	if got := attempts(Options{}, Request{}, true); got != 1 {
		t.Fatalf("default attempts=%d want 1", got)
	}
}
