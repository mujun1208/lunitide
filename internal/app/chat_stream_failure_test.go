package app

import (
	"context"
	"errors"
	"strings"
	"testing"

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
		{"gateway request budget", &llmadapter.Error{Code: "REQUEST_TOO_LARGE", Stage: llmadapter.StageDecode, Message: canary}, "REQUEST_TOO_LARGE", "请求内容过大，请减少附件或上下文后重试", false},
		{"http 400", &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: canary}, "UPSTREAM_BAD_REQUEST", "供应商拒绝了请求，请检查模型、附件和上下文", false},
		{"http 413", &llmadapter.Error{Code: "HTTP_413", Stage: llmadapter.StageHTTP, HTTPStatus: 413, Message: canary}, "REQUEST_TOO_LARGE", "请求内容过大，请减少附件或上下文后重试", false},
		{"http 401", &llmadapter.Error{Code: "HTTP_401", Stage: llmadapter.StageHTTP, HTTPStatus: 401, Message: canary}, "PROVIDER_AUTHENTICATION_FAILED", "供应商身份验证失败，请检查凭据", false},
		{"http 403", &llmadapter.Error{Code: "HTTP_403", Stage: llmadapter.StageHTTP, HTTPStatus: 403, Message: canary}, "PROVIDER_ACCESS_DENIED", "供应商拒绝访问，请检查模型权限", false},
		{"http 429", &llmadapter.Error{Code: "HTTP_429", Stage: llmadapter.StageHTTP, HTTPStatus: 429, Message: canary}, "PROVIDER_RATE_LIMITED", "供应商请求过于频繁，请稍后重试", true},
		{"http 500", &llmadapter.Error{Code: "HTTP_500", Stage: llmadapter.StageHTTP, HTTPStatus: 500, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true},
		{"http 599", &llmadapter.Error{Code: "HTTP_599", Stage: llmadapter.StageHTTP, HTTPStatus: 599, Message: canary}, "UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true},
		{"deadline", context.DeadlineExceeded, "UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true},
		{"uncertain timeout", &llmadapter.Error{Code: "OUTCOME_UNKNOWN", Stage: llmadapter.StageConnect, Message: canary}, "UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true},
		{"malformed response", &llmadapter.Error{Code: "MALFORMED_RESPONSE", Stage: llmadapter.StageDecode, Message: canary}, "UPSTREAM_MALFORMED_RESPONSE", "模型返回格式不完整，已保留收到的内容，请重试", true},
		{"unknown", errors.New(canary), "UPSTREAM_FAILED", "模型请求失败", true},
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
