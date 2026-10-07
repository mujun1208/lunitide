package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
)

// Reasoning-effort demotion tests (2026-10-06/07 postmortem): continuation
// waves re-think from scratch because reasoning is not replayed, and GLM-5.3
// cannot disable thinking outright. The run loop therefore demotes effort to
// low when a wave is known to pay the full thinking cost again (length and
// wire-limit continues) or when the upstream produced nothing but thinking
// (stall cut, repeated 0-byte cancels).

func TestDemoteReasoningEffort(t *testing.T) {
	enabled := &modelfit.EffectiveParameters{ThinkingType: "enabled", Effort: "high"}
	req := llmadapter.Request{ReasoningLevel: "max", Effective: enabled}
	demoteReasoningEffort(&req)
	if req.ReasoningLevel != "low" || req.Effective.Effort != "low" || req.Effective.ThinkingType != "enabled" {
		t.Fatalf("level=%q eff=%+v", req.ReasoningLevel, req.Effective)
	}
	// Disabled thinking stays disabled; only the typed level drops.
	disabled := &modelfit.EffectiveParameters{ThinkingType: "disabled", Effort: "high"}
	req = llmadapter.Request{ReasoningLevel: "max", Effective: disabled}
	demoteReasoningEffort(&req)
	if req.ReasoningLevel != "low" || req.Effective.Effort != "high" || req.Effective.ThinkingType != "disabled" {
		t.Fatalf("disabled profile must not be re-enabled: level=%q eff=%+v", req.ReasoningLevel, req.Effective)
	}
	// Companion-style requests never gain a thinking field.
	req = llmadapter.Request{ReasoningLevel: "max", DisableReasoning: true}
	demoteReasoningEffort(&req)
	if req.ReasoningLevel != "max" {
		t.Fatalf("DisableReasoning must be untouched: %+v", req)
	}
	// A missing Effective still demotes through the durable channel.
	req = llmadapter.Request{ReasoningLevel: "max"}
	demoteReasoningEffort(&req)
	if req.ReasoningLevel != "low" || req.Effective != nil {
		t.Fatalf("level=%q eff=%v", req.ReasoningLevel, req.Effective)
	}
}

func TestChatModelRetryLossless(t *testing.T) {
	cancelled := error(&llmadapter.Error{Code: "CANCELLED", Stage: llmadapter.StageStream, Message: "upstream cancel"})
	stall := &llmadapter.Error{Code: "REASONING_STALL", Stage: llmadapter.StageStream, Message: "stall"}
	timeout := &llmadapter.Error{Code: "TIMEOUT", Stage: llmadapter.StageStream, Message: "timeout"}
	if !chatModelRetryLossless(cancelled, true) {
		t.Fatal("a 0-byte cancellation must be retryable")
	}
	if chatModelRetryLossless(cancelled, false) {
		t.Fatal("a cancellation that already streamed text must not be retried")
	}
	if !chatModelRetryLossless(stall, true) || !chatModelRetryLossless(stall, false) {
		t.Fatal("a thinking stall is lossless by construction and always retryable")
	}
	if !chatModelRetryLossless(timeout, true) || !chatModelRetryLossless(timeout, false) {
		t.Fatal("timeouts keep their historical retry shape")
	}
	if chatModelRetryLossless(nil, true) {
		t.Fatal("nil is never retryable")
	}
}

type demoteWaveAdapter struct {
	calls  int
	levels []string
	effort []string
	fail   func(call int, emit func(llmadapter.Delta) error) error
}

func (a *demoteWaveAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *demoteWaveAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *demoteWaveAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.levels = append(a.levels, req.ReasoningLevel)
	if req.Effective != nil {
		a.effort = append(a.effort, req.Effective.Effort)
	} else {
		a.effort = append(a.effort, "")
	}
	if err := a.fail(a.calls, emit); err != nil {
		return llmadapter.Response{}, err
	}
	text := "报告已经全部写完。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func demoteWaveRequest() llmadapter.Request {
	return llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Effective:      &modelfit.EffectiveParameters{ThinkingType: "enabled", Effort: "high"},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个关于12星座爱情匹配的长篇分析报告论文"}},
	}
}

func TestLengthContinueWaveDemotesReasoningEffort(t *testing.T) {
	adapter := &demoteWaveAdapter{}
	adapter.fail = func(call int, emit func(llmadapter.Delta) error) error {
		if call == 1 {
			if err := emit(llmadapter.Delta{Text: "白羊座写到一半"}); err != nil {
				return err
			}
			return &llmadapter.Error{Code: "RESPONSE_TRUNCATED", Stage: llmadapter.StageStream, Message: "length"}
		}
		return nil
	}
	deltas := runContinueStream(t, adapter, demoteWaveRequest())
	if adapter.calls != 2 {
		t.Fatalf("calls=%d: the truncated wave must continue exactly once", adapter.calls)
	}
	if adapter.levels[1] != "low" || adapter.effort[1] != "low" {
		t.Fatalf("continuation wave must run at low effort: levels=%v effort=%v", adapter.levels, adapter.effort)
	}
	joined := strings.Join(deltas, "")
	if !strings.Contains(joined, "白羊座写到一半") || !strings.Contains(joined, "报告已经全部写完") {
		t.Fatalf("partial text must survive the wave: %q", joined)
	}
}

func TestThinkingStallRetryDemotesReasoningEffort(t *testing.T) {
	adapter := &demoteWaveAdapter{}
	adapter.fail = func(call int, emit func(llmadapter.Delta) error) error {
		if call == 1 {
			return &llmadapter.Error{Code: "REASONING_STALL", Stage: llmadapter.StageStream, Message: "stall"}
		}
		return nil
	}
	deltas := runContinueStream(t, adapter, demoteWaveRequest())
	if adapter.calls != 2 {
		t.Fatalf("calls=%d: the stalled attempt must be retried once", adapter.calls)
	}
	if adapter.levels[1] != "low" || adapter.effort[1] != "low" {
		t.Fatalf("stall retry must run at low effort: levels=%v effort=%v", adapter.levels, adapter.effort)
	}
	if !strings.Contains(strings.Join(deltas, ""), "报告已经全部写完") {
		t.Fatalf("retry must finish the task: %q", strings.Join(deltas, ""))
	}
}

func TestZeroByteCancelRetriesThenDemotes(t *testing.T) {
	adapter := &demoteWaveAdapter{}
	adapter.fail = func(call int, emit func(llmadapter.Delta) error) error {
		if call <= 2 {
			// Agent-Plan-shaped upstream cancel: long think, zero bytes.
			return &llmadapter.Error{Code: "CANCELLED", Stage: llmadapter.StageStream, Message: "cancelled"}
		}
		return nil
	}
	deltas := runContinueStream(t, adapter, demoteWaveRequest())
	if adapter.calls != 3 {
		t.Fatalf("calls=%d: two 0-byte cancels must be retried before success", adapter.calls)
	}
	if adapter.levels[0] != "max" || adapter.levels[1] != "max" {
		t.Fatalf("the first cancel must retry at the original effort: %v", adapter.levels)
	}
	if adapter.levels[2] != "low" || adapter.effort[2] != "low" {
		t.Fatalf("a second consecutive 0-byte cancel must demote: levels=%v effort=%v", adapter.levels, adapter.effort)
	}
	if !strings.Contains(strings.Join(deltas, ""), "报告已经全部写完") {
		t.Fatalf("task must complete after demotion: %q", strings.Join(deltas, ""))
	}
}
