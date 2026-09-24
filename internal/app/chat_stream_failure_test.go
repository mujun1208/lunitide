package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestChatStreamErrorMapsProviderFailureClasses(t *testing.T) {
	const canary = "SECRET-CANARY upstream response"
	tests := []struct {
		name      string
		err       error
		code      string
		message   string
		retryable bool
	}{
		{"gateway request budget", &llmadapter.Error{Code: "REQUEST_TOO_LARGE", Stage: llmadapter.StageDecode, Message: canary}, "REQUEST_TOO_LARGE", "较早的对话已经收起。请再试一次。", false},
		{"http 400", &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: canary}, "UPSTREAM_BAD_REQUEST", "供应商拒绝了请求，请检查模型、附件和上下文", false},
		{"http 413", &llmadapter.Error{Code: "HTTP_413", Stage: llmadapter.StageHTTP, HTTPStatus: 413, Message: canary}, "REQUEST_TOO_LARGE", "较早的对话已经收起。请再试一次。", false},
		{"http 401", &llmadapter.Error{Code: "HTTP_401", Stage: llmadapter.StageHTTP, HTTPStatus: 401, Message: canary}, "PROVIDER_AUTHENTICATION_FAILED", "供应商身份验证失败，请检查凭据", false},
		{"http 403", &llmadapter.Error{Code: "HTTP_403", Stage: llmadapter.StageHTTP, HTTPStatus: 403, Message: canary}, "PROVIDER_ACCESS_DENIED", "供应商拒绝访问，请检查模型权限", false},
		{"http 429", &llmadapter.Error{Code: "HTTP_429", Stage: llmadapter.StageHTTP, HTTPStatus: 429, Message: canary}, "PROVIDER_RATE_LIMITED", "供应商请求过于频繁，请稍后重试", true},
		{"http 500", &llmadapter.Error{Code: "HTTP_500", Stage: llmadapter.StageHTTP, HTTPStatus: 500, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true},
		{"http 599", &llmadapter.Error{Code: "HTTP_599", Stage: llmadapter.StageHTTP, HTTPStatus: 599, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true},
		{"deadline", context.DeadlineExceeded, "UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true},
		{"uncertain timeout", &llmadapter.Error{Code: "OUTCOME_UNKNOWN", Stage: llmadapter.StageConnect, Message: canary}, "UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true},
		{"stream incomplete", &llmadapter.Error{Code: "STREAM_INCOMPLETE", Stage: llmadapter.StageDecode, Message: canary}, "UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true},
		{"connection refused", &llmadapter.Error{Code: "CONNECTION_REFUSED", Stage: llmadapter.StageConnect, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商连接失败，请稍后重试", true},
		{"stream unavailable", &llmadapter.Error{Code: "STREAM_UNAVAILABLE", Stage: llmadapter.StageHTTP, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true},
		{"malformed response", &llmadapter.Error{Code: "MALFORMED_RESPONSE", Stage: llmadapter.StageDecode, Message: canary}, "UPSTREAM_MALFORMED_RESPONSE", "模型返回格式不完整，已保留收到的内容，请重试", true},
		{"unknown", errors.New(canary), "UPSTREAM_FAILED", "模型请求失败", true},
		{"execution budget", agentrun.ErrExecutionBudget, "BUDGET_EXHAUSTED", "这一轮先记下已完成的内容，请再试一次。", false},
		{"context window", agentrun.ErrContextWindow, "CONTEXT_WINDOW_EXCEEDED", "这一轮的内容太长，较早的对话已经收起。请再试一次。", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := chatStreamError(test.err)
			if got.Code != test.code || got.Message != test.message || got.Retryable != test.retryable {
				t.Fatalf("chatStreamError() = %#v", got)
			}
			if strings.Contains(got.Message, canary) {
				t.Fatalf("stream error leaked upstream text: %#v", got)
			}
		})
	}
}

func TestModelTimeoutStaysInsideTheTurn(t *testing.T) {
	if !chatModelCallRetryable(context.DeadlineExceeded) {
		t.Fatal("a model timeout must be retried inside the turn")
	}
	if !chatModelCallRetryable(&llmadapter.Error{Code: "TIMEOUT", Stage: llmadapter.StageStream}) {
		t.Fatal("response-stream timeout must be retried")
	}
	if chatModelCallRetryable(&llmadapter.Error{Code: "STREAM_AUTHENTICATION_FAILED", Stage: llmadapter.StageHTTP, HTTPStatus: 401}) {
		t.Fatal("an authentication failure must not be retried")
	}
}

func TestTimeoutAfterWrittenFilesSettlesTheTurn(t *testing.T) {
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "构建一个可运行的 CRM 系统 POC"},
		{Role: llmadapter.RoleTool, ToolCallID: "w1", Content: "wrote index.html"},
		{Role: llmadapter.RoleTool, ToolCallID: "w2", Content: "wrote README.md"},
	}
	speech, settle := timeoutAfterDeliverable(context.DeadlineExceeded, messages)
	if !settle || !strings.Contains(speech, "index.html") || !strings.Contains(speech, "README.md") || strings.Contains(speech, "无法执行") {
		t.Fatalf("speech=%q settle=%v", speech, settle)
	}
	if _, settle := timeoutAfterDeliverable(context.DeadlineExceeded, nil); settle {
		t.Fatal("a timeout before any file must keep retrying")
	}
	open := append([]llmadapter.Message{}, messages...)
	open = append(open, llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
		Name:      "todo.write",
		Arguments: []byte(`{"todos":[{"content":"写页面","status":"completed"},{"content":"跑测试","status":"pending"},{"content":"改说明","status":"in_progress"},{"content":"收尾","status":"pending"}]}`),
	}}})
	if _, settle := timeoutAfterDeliverable(context.DeadlineExceeded, open); settle {
		t.Fatal("an open checklist must keep the turn going after files exist")
	}
	done := []llmadapter.Message{
		messages[0], messages[1], messages[2],
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			Name:      "todo.write",
			Arguments: []byte(`{"todos":[{"content":"写页面","status":"completed"},{"content":"跑测试","status":"completed"}]}`),
		}}},
	}
	if speech, settle := timeoutAfterDeliverable(context.DeadlineExceeded, done); !settle || !strings.Contains(speech, "index.html") {
		t.Fatalf("a finished checklist may settle, speech=%q settle=%v", speech, settle)
	}
}

func TestPlanPauseRoomIsOneTurnNotThreePerStep(t *testing.T) {
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "分四步做完"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			Name:      "todo.write",
			Arguments: []byte(`{"todos":[{"content":"a","status":"completed"},{"content":"b"},{"content":"c","status":"in_progress"},{"content":"d","status":"pending"}]}`),
		}}},
	}
	open, total := planChecklist(messages)
	if open != 3 || total != 4 {
		t.Fatalf("open=%d total=%d", open, total)
	}
	// A 4-step checklist may pause four times, even after some steps are
	// already completed. It must not become 3 pauses × 4 steps, and finishing
	// a step must not shrink the room so the rest of the plan stops early.
	for nudge := 0; nudge < 4; nudge++ {
		if !planPauseRoom(nudge, 1, 4) {
			t.Fatalf("nudge %d should still be inside the same turn", nudge)
		}
	}
	if planPauseRoom(4, 1, 4) || planPauseRoom(0, 0, 4) || planPauseRoom(12, 4, 4) {
		t.Fatal("the pause room must close at the checklist length")
	}
	finished := []llmadapter.Message{messages[0], {Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
		Name: "todo.write", Arguments: []byte(`{"todos":[{"content":"a","status":"completed"}]}`),
	}}}}
	if openPlanSteps(finished) != 0 {
		t.Fatal("a completed checklist is not an open plan")
	}
}
