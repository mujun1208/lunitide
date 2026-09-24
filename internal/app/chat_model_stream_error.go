package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func chatModelStreamError(err error) *bridge.StreamError {
	out := chatStreamError(err)
	code := string(networkpolicy.ErrorCode(err))
	var upstream *llmadapter.Error
	if errors.As(err, &upstream) {
		code = upstream.Code
	}
	set := func(code, message string, retryable bool) {
		out = &bridge.StreamError{Code: code, Message: message, Retryable: retryable}
	}
	switch code {
	case "RESPONSE_TRUNCATED":
		set("UPSTREAM_RESPONSE_TRUNCATED", "已保留收到的内容，任务未完成，请再试一次。", false)
	case "RESPONSE_FILTERED":
		set("UPSTREAM_RESPONSE_FILTERED", "供应商内容过滤中止了模型输出，已保留部分内容；任务未完成，本次响应中的工具未执行", false)
	case "MALFORMED_RESPONSE":
		set("UPSTREAM_MALFORMED_RESPONSE", "模型返回格式不完整，已保留收到的内容，请重试", true)
	case "REQUEST_TOO_LARGE":
		set("REQUEST_TOO_LARGE", "较早的对话已经收起。请再试一次。", false)
	case "CONTEXT_WINDOW_EXCEEDED":
		set("CONTEXT_WINDOW_EXCEEDED", windowOverflowUserMessage(), false)
	case "OUTCOME_UNKNOWN":
		set("UPSTREAM_OUTCOME_UNKNOWN", "模型请求结果尚无法确认，请先检查任务状态和已生成文件，避免重复执行", false)
	case "MODEL_CHANNEL_UNAVAILABLE":
		set("MODEL_CHANNEL_UNAVAILABLE", "当前模型没有可用的供应商通道，请检查模型通道配置或联系供应商", false)
	case "STREAM_INCOMPLETE":
		set("UPSTREAM_STREAM_INCOMPLETE", "模型响应在结束标记前中断，已保留收到的内容，任务未完成，请重试", true)
	case "UPSTREAM_STREAM_FAILED":
		set("UPSTREAM_STREAM_FAILED", "供应商在响应过程中报告错误，已保留收到的内容，任务未完成，请重试", true)
	case "STREAM_BAD_REQUEST":
		set("UPSTREAM_BAD_REQUEST", "供应商拒绝了请求，请检查模型、附件和上下文", false)
	case "STREAM_AUTHENTICATION_FAILED":
		set("PROVIDER_AUTHENTICATION_FAILED", "供应商身份验证失败，请检查凭据", false)
	case "STREAM_ACCESS_DENIED":
		set("PROVIDER_ACCESS_DENIED", "供应商拒绝访问，请检查模型权限", false)
	case "STREAM_NOT_FOUND":
		set("UPSTREAM_NOT_FOUND", "模型服务或模型不存在，请检查服务地址和模型名称", false)
	case "STREAM_RATE_LIMITED":
		set("PROVIDER_RATE_LIMITED", "供应商请求过于频繁，请稍后重试", true)
	case "STREAM_UNAVAILABLE", "STREAM_OVERLOADED":
		set("UPSTREAM_UNAVAILABLE", "供应商服务暂时不可用，请稍后重试", true)
	case "RESPONSE_TOO_LARGE":
		set("UPSTREAM_RESPONSE_TOO_LARGE", "模型响应超过接收大小限制，已保留收到的内容，请缩短输出后重试", false)
	case "RESPONSE_BODY_TOO_LARGE":
		set("UPSTREAM_RESPONSE_BODY_TOO_LARGE", "模型响应流的累计传输数据超过接收限制，已保留收到的内容，任务未完成，请再试一次", false)
	case "RESPONSE_LINE_TOO_LARGE":
		set("UPSTREAM_RESPONSE_LINE_TOO_LARGE", "模型响应流的单行数据超过接收限制，已保留收到的内容，任务未完成，请检查响应流格式和单行大小限制", false)
	case "RESPONSE_EVENT_TOO_LARGE":
		set("UPSTREAM_RESPONSE_EVENT_TOO_LARGE", "模型响应流的单个事件超过接收限制，已保留收到的内容，任务未完成，请检查响应流格式和事件大小限制", false)
	case "DNS_ERROR":
		set("UPSTREAM_DNS_FAILED", "模型服务地址解析失败，请检查网络和服务地址", true)
	case "TLS_ERROR":
		set("UPSTREAM_TLS_FAILED", "模型服务安全连接失败，请检查网络和证书", false)
	case "CONNECTION_REFUSED", "CONNECTION_FAILED":
		set("UPSTREAM_CONNECTION_FAILED", "模型服务连接失败或中断，请检查网络后重试", true)
	case "SSRF_BLOCKED", "REDIRECT_BLOCKED", "HTTPS_REQUIRED":
		set("UPSTREAM_CONNECTION_BLOCKED", "模型服务连接被网络安全策略阻止，请检查服务地址", false)
	case "CANCELLED":
		set("UPSTREAM_CANCELLED", "模型请求已取消，任务未完成", true)
	case "TIMEOUT":
		set("UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		set("UPSTREAM_TIMEOUT", "模型请求超时，请稍后重试", true)
	}
	if upstream != nil && upstream.HTTPStatus == 404 {
		set("UPSTREAM_NOT_FOUND", "模型服务或模型不存在，请检查服务地址和模型名称", false)
	}
	if upstream != nil && upstream.Stage == llmadapter.StageStream {
		out.Message += "（阶段：响应流）"
	}
	return out
}

// chatModelCallRetryable retries a timed-out model call inside the same turn.
// A refused connection or a half-finished stream already has a terminal
// outcome, and retrying it repeats the partial text.
func chatModelCallRetryable(err error) bool {
	if err == nil {
		return false
	}
	se := chatModelStreamError(err)
	return se != nil && se.Code == "UPSTREAM_TIMEOUT"
}

func chatModelFinishError(reason llmadapter.FinishReason) error {
	code := ""
	switch reason {
	case llmadapter.FinishReasonLength:
		code = "RESPONSE_TRUNCATED"
	case llmadapter.FinishReasonWindow:
		code = "CONTEXT_WINDOW_EXCEEDED"
	case llmadapter.FinishReasonContentFilter:
		code = "RESPONSE_FILTERED"
	default:
		return nil
	}
	return &llmadapter.Error{Code: code, Stage: llmadapter.StageStream, Message: "model response did not complete normally"}
}

func chatModelCompletionFailed(err error) bool {
	var upstream *llmadapter.Error
	return errors.As(err, &upstream) && (upstream.Code == "RESPONSE_TRUNCATED" || upstream.Code == "RESPONSE_FILTERED")
}

// Only local classifications enter logs. Never format err or vendor Message:
// either may contain request bodies, endpoint queries or credentials.
func chatModelFailureDiagnostic(err error) string {
	stage, status := "unknown", 0
	var upstream *llmadapter.Error
	if errors.As(err, &upstream) {
		switch upstream.Stage {
		case llmadapter.StageConnect, llmadapter.StageHTTP, llmadapter.StageDecode, llmadapter.StageStream:
			stage = string(upstream.Stage)
		}
		if upstream.HTTPStatus >= 100 && upstream.HTTPStatus <= 599 {
			status = upstream.HTTPStatus
		}
	}
	return fmt.Sprintf("code=%s stage=%s http_status=%d", chatModelStreamError(err).Code, stage, status)
}

func timeoutAfterDeliverable(err error, messages []llmadapter.Message) (string, bool) {
	if !chatModelCallRetryable(err) {
		return "", false
	}
	// Files from an earlier step are not the end of a plan that still has
	// open checklist items. Keep the model call retry instead of closing.
	if openPlanSteps(messages) > 0 {
		return "", false
	}
	speech := deliveredFileSpeech(messages)
	if speech == "" {
		return "", false
	}
	return speech, true
}

func deliveredFileSpeech(messages []llmadapter.Message) string {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	var names []string
	seen := map[string]bool{}
	for _, m := range messages[start:] {
		if m.Role != llmadapter.RoleTool {
			continue
		}
		line := strings.TrimSpace(m.Content)
		if i := strings.IndexAny(line, "\r\n"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		var name string
		switch {
		case strings.HasPrefix(line, "wrote "):
			name = strings.TrimPrefix(line, "wrote ")
		case strings.HasPrefix(line, "written "):
			name = strings.TrimPrefix(line, "written ")
		case strings.HasPrefix(line, "edited "):
			name = strings.TrimPrefix(line, "edited ")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if i := strings.IndexAny(name, " ("); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if j := strings.LastIndexAny(name, `/\`); j >= 0 {
			name = name[j+1:]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	return "已经写好：" + strings.Join(names, "、") + "。后面的总结超时了，文件还在，可以直接打开。"
}

func chatModelOutcomeNotice(cancelling bool, err error) string {
	if cancelling {
		return turnInterruptNotice
	}
	if err != nil {
		return turnErrorNotice + chatModelStreamError(err).Message
	}
	return ""
}
