package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestIsWindowOverflowError(t *testing.T) {
	if !isWindowOverflowError(&llmadapter.Error{Code: "CONTEXT_WINDOW_EXCEEDED"}) {
		t.Fatal("window code")
	}
	if !isWindowOverflowError(&llmadapter.Error{Code: "REQUEST_TOO_LARGE"}) {
		t.Fatal("too large")
	}
	if !isWindowOverflowError(&llmadapter.Error{Code: "HTTP_413", HTTPStatus: 413}) {
		t.Fatal("http 413")
	}
	if !isWindowOverflowError(&llmadapter.Error{Code: "RESPONSE_TRUNCATED"}) {
		t.Fatal("length finish must retry the current user step")
	}
	if err := chatModelFinishError(llmadapter.FinishReasonWindow); !isWindowOverflowError(err) {
		t.Fatal("window finish must retry")
	}
	if err := chatModelFinishError(llmadapter.FinishReasonLength); !isWindowOverflowError(err) {
		t.Fatal("length finish must retry via checkpoint")
	}
	if isWindowOverflowError(&llmadapter.Error{Code: "RESPONSE_FILTERED"}) {
		t.Fatal("content filter is not a window retry")
	}
}

func TestShrinkMessagesForWindowRetryKeepsSystemAndTail(t *testing.T) {
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "rules"},
		{Role: llmadapter.RoleUser, Content: "old-1"},
		{Role: llmadapter.RoleAssistant, Content: "old-2"},
		{Role: llmadapter.RoleUser, Content: "old-3"},
		{Role: llmadapter.RoleAssistant, Content: "old-4"},
		{Role: llmadapter.RoleUser, Content: "old-5"},
		{Role: llmadapter.RoleAssistant, Content: "old-6"},
		{Role: llmadapter.RoleUser, Content: "old-7"},
		{Role: llmadapter.RoleAssistant, Content: "old-8"},
		{Role: llmadapter.RoleUser, Content: "current"},
	}}
	shrinkMessagesForWindowRetry(req)
	if req.Messages[0].Content != "rules" {
		t.Fatal("lost system")
	}
	if !strings.Contains(req.Messages[1].Content, "检查点") {
		t.Fatal("missing checkpoint note")
	}
	if req.Messages[2].Content == "old-1" {
		t.Fatal("should drop the oldest user turn")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llmadapter.RoleUser || last.Content != "current" {
		t.Fatalf("lost current user: %+v", last)
	}
}

func TestApplyWindowRetryMessagesUsesCheckpointSummary(t *testing.T) {
	req := &llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "rules"},
		{Role: llmadapter.RoleUser, Content: "old-1"},
		{Role: llmadapter.RoleAssistant, Content: "old-2"},
		{Role: llmadapter.RoleUser, Content: "current"},
	}}
	applyWindowRetryMessages(req, "先前约定：继续写周报")
	if req.Messages[0].Content != "rules" {
		t.Fatal("lost system")
	}
	if !strings.Contains(req.Messages[1].Content, "先前约定：继续写周报") {
		t.Fatalf("missing checkpoint summary: %+v", req.Messages)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llmadapter.RoleUser || last.Content != "current" {
		t.Fatalf("lost current user: %+v", last)
	}
}

func TestProviderModelContextWindow(t *testing.T) {
	p := provider.Provider{Models: []provider.Model{{ModelID: "glm-5.3", ContextWindow: 1000000}, {ModelID: "other", ContextWindow: 32000}}}
	if window, explicit := providerModelContextWindow(p, "glm-5.3"); window != 1000000 || !explicit {
		t.Fatalf("configured window = %d explicit=%v", window, explicit)
	}
	if window, explicit := providerModelContextWindow(p, "missing"); window != 128000 || explicit {
		t.Fatalf("fallback window = %d explicit=%v", window, explicit)
	}
}

func TestCompanionColdMaxMessages(t *testing.T) {
	if companionColdMaxMessages(true) != companionMaxMessages {
		t.Fatal("checkpoint should keep the voice window")
	}
	if companionColdMaxMessages(false) != companionMaxMessages/2 {
		t.Fatal("no checkpoint should tighten")
	}
}
