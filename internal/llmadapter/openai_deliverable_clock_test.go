package llmadapter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Deliverable-clock tests (2026-10-06/07 postmortem). The three existing
// watchdog clocks (byte, event, content) all reset on reasoning deltas and
// keepalives, which is how production continuation waves streamed nothing
// but thinking for 10+ minutes without a cut. The deliverable clock is the
// fourth layer: it only retires on the first text or tool delta, so it
// fires exclusively on streams that never produced a deliverable byte.
// A cut is therefore always lossless for the same-turn retry.

const deliverableTestIdle = 500 * time.Millisecond

// streamReasoningForever writes one deliverable-free beat after another;
// every frame is a valid SSE event carrying reasoning only, so the byte,
// event and content clocks all stay fed and only the deliverable clock
// can cut the stream.
func streamReasoningForever(reasoningFrame string, beat time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(beat):
				fmt.Fprint(w, reasoningFrame)
				flusher.Flush()
			}
		}
	}
}

func TestDeliverableClockReasoningOnlyStreamIsStall(t *testing.T) {
	srv, pool := h2WatchdogServer(t, streamReasoningForever(
		`data: {"id":"1","choices":[{"delta":{"reasoning_content":"想"}}]}`+"\n\n",
		30*time.Millisecond))
	// L1/L2 at 4s and the content clock at 4s never fire on reasoning
	// beats; only the 500ms deliverable clock can, so a cut proves it.
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewOpenAI(c, Options{StreamContentIdle: 4 * time.Second, DeliverableIdle: deliverableTestIdle})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, watchdogRequest(), func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a reasoning-only stream must be cut by the deliverable clock")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "REASONING_STALL" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: thinking-only streams must surface as REASONING_STALL so the run loop demotes effort and retries", code, stage)
	}
	if elapsed < deliverableTestIdle || elapsed > 8*time.Second {
		t.Fatalf("deliverable clock fired at %s, want ~%s", elapsed, deliverableTestIdle)
	}
	if resp.Message.Content != "" {
		t.Fatalf("a stall cut must happen with zero deliverable bytes: %q", resp.Message.Content)
	}
	if !strings.Contains(resp.Reasoning, "想") {
		t.Fatalf("received reasoning must survive the cut: %q", resp.Reasoning)
	}
}

// Once the first text delta lands the deliverable clock retires for the
// rest of the stream: a long quiet reasoning stretch mid-stream must not
// be cut, because the retry would throw away already-delivered text.
func TestDeliverableClockRetiresAfterFirstText(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"content":"开头"}}]}`+"\n\n")
		flusher.Flush()
		// 900ms of reasoning beats, triple the 300ms deliverable window.
		deadline := time.Now().Add(900 * time.Millisecond)
		for time.Now().Before(deadline) {
			fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning_content":"想"}}]}`+"\n\n")
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"content":"结尾"}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewOpenAI(c, Options{StreamContentIdle: 4 * time.Second, DeliverableIdle: 300 * time.Millisecond})
	resp, err := a.Stream(context.Background(), nil, watchdogRequest(), func(Delta) error { return nil })
	if err != nil {
		t.Fatalf("a stream that already delivered text must not be cut mid-reasoning: %v", err)
	}
	if resp.Message.Content != "开头结尾" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
}

// Tool fragments are deliverables too: a stream that opened a tool call
// and then thinks for longer than the window must complete, because the
// retry would re-issue a call the model already committed to.
func TestDeliverableClockToolFragmentRetiresClock(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"write","arguments":"{\"path\":"}}]}}]}`+"\n\n")
		flusher.Flush()
		deadline := time.Now().Add(900 * time.Millisecond)
		for time.Now().Before(deadline) {
			fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"reasoning_content":"想"}}]}`+"\n\n")
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.txt\"}"}}]}}]}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, `data: {"id":"1","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`+"\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewOpenAI(c, Options{StreamContentIdle: 4 * time.Second, DeliverableIdle: 300 * time.Millisecond})
	resp, err := a.Stream(context.Background(), nil, watchdogRequest(), func(Delta) error { return nil })
	if err != nil {
		t.Fatalf("a stream that already opened a tool call must not be cut mid-reasoning: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "write" {
		t.Fatalf("tool calls=%+v", resp.Message.ToolCalls)
	}
}

// The Anthropic protocol line: thinking deltas feed every other clock, so
// only the deliverable clock cuts a thinking-only stream.
func TestDeliverableClockAnthropicThinkingOnlyIsStall(t *testing.T) {
	srv, pool := h2WatchdogServer(t, streamReasoningForever(
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"想\"}}\n\n",
		30*time.Millisecond))
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewAnthropic(c, Options{StreamContentIdle: 4 * time.Second, DeliverableIdle: deliverableTestIdle})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, Request{Model: "claude", MaxTokens: 64, Messages: []Message{{Role: RoleUser, Content: "写一段"}}}, func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a thinking-only anthropic stream must be cut")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "REASONING_STALL" || stage != StageStream {
		t.Fatalf("code=%q stage=%q", code, stage)
	}
	if elapsed < deliverableTestIdle || elapsed > 8*time.Second {
		t.Fatalf("deliverable clock fired at %s, want ~%s", elapsed, deliverableTestIdle)
	}
	if resp.Message.Content != "" {
		t.Fatalf("stall cut must carry zero text: %q", resp.Message.Content)
	}
}

// The Responses protocol line (the wire Ark Agent Plan speaks): reasoning
// summary deltas feed every other clock, so only the deliverable clock
// cuts a thinking-only stream.
func TestDeliverableClockResponsesReasoningOnlyIsStall(t *testing.T) {
	srv, pool := h2WatchdogServer(t, streamReasoningForever(
		"event: response.reasoning_text.delta\ndata: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"想\"}\n\n",
		30*time.Millisecond))
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewOpenAIResponses(c, Options{StreamContentIdle: 4 * time.Second, DeliverableIdle: deliverableTestIdle})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, watchdogRequest(), func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a reasoning-only responses stream must be cut")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "REASONING_STALL" || stage != StageStream {
		t.Fatalf("code=%q stage=%q", code, stage)
	}
	if elapsed < deliverableTestIdle || elapsed > 8*time.Second {
		t.Fatalf("deliverable clock fired at %s, want ~%s", elapsed, deliverableTestIdle)
	}
	if resp.Message.Content != "" {
		t.Fatalf("stall cut must carry zero text: %q", resp.Message.Content)
	}
}
