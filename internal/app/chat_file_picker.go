package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// filePickerAskArgs matches web FILE_PICKER_ASK so the wizard can parse the
// parked user.ask approval without a second renderer-only overlay.
var filePickerAskArgs = json.RawMessage(`{"title":"文件对话框","questions":[{"id":"file-dialog","prompt":"屏幕上出现了打开/保存文件对话框。我不能代你选文件，请你在系统对话框里点「保存」「打开」或「取消」。","options":[{"id":"done","label":"我已经点完了"},{"id":"cancel","label":"我点了取消"},{"id":"wait","label":"稍等一下"}]}]}`)

var uacAskArgs = json.RawMessage(`{"title":"系统提权","questions":[{"id":"uac-dialog","prompt":"屏幕上出现了 UAC / 系统提权对话框。我不能代点「是」。请你自己确认或取消。","options":[{"id":"done","label":"我已经处理完了"},{"id":"cancel","label":"我点了取消"},{"id":"wait","label":"稍等一下"}]}]}`)

func looksLikeUACToolResult(summary string) bool {
	text := strings.ToLower(strings.TrimSpace(summary))
	if text == "" {
		return false
	}
	if strings.Contains(text, "needs_user") && (strings.Contains(text, "uac") || strings.Contains(text, "提权")) {
		return true
	}
	return strings.Contains(text, "uac dialog") || strings.Contains(text, "elevation dialog")
}

func (e *Engine) parkUACAsk(ctx context.Context, runID, sessionID string, mode executionMode, send func(bridge.Event) error) error {
	if e == nil || e.tools == nil || sessionID == "" || send == nil {
		return nil
	}
	callID := "ask-" + ulid.Make().String()
	pending, err := e.tools.Prepare(ctx, runID, sessionID, callID, "user.ask", uacAskArgs, toolruntime.Mode(mode), 10*time.Minute)
	if err != nil {
		log.Printf("uac park Prepare failed: %v", err)
		return nil
	}
	return send(bridge.Event{
		Type: bridge.EventApprovalRequired,
		Tool: &bridge.ToolEvent{
			CallID:     pending.CallID,
			Name:       "user.ask",
			ArgsDigest: pending.ArgsDigest,
			Summary:    approvalRequiredSummary("user.ask", uacAskArgs),
		},
	})
}

func browserWallAskArgs(reason string) json.RawMessage {
	title := "需要你决策"
	prompt := "网页需要你本地处理后再继续，不要盲点或调用 computer.act。"
	switch reason {
	case "login":
		title = "需要登录"
		prompt = "页面出现登录墙。请你在浏览器里完成登录，不要让我代填密码或继续盲点。"
	case "pay":
		title = "需要支付确认"
		prompt = "页面出现支付/结账。请你自己完成支付，我不能代点。"
	case "captcha":
		title = "需要验证码"
		prompt = "页面出现验证码。请你自己完成验证，我不能代点。"
	}
	raw, err := json.Marshal(map[string]any{
		"title":  title,
		"reason": reason,
		"questions": []map[string]any{{
			"id":     "browser-wall",
			"prompt": prompt,
			"options": []map[string]string{
				{"id": "done", "label": "我已经处理完了"},
				{"id": "cancel", "label": "先停在这里"},
				{"id": "wait", "label": "稍等一下"},
			},
		}},
	})
	if err != nil {
		return json.RawMessage(`{"title":"需要你决策","reason":"decision","questions":[{"id":"browser-wall","prompt":"网页需要你本地处理后再继续。","options":[{"id":"done","label":"我已经处理完了"},{"id":"cancel","label":"先停在这里"}]}]}`)
	}
	return raw
}

func (e *Engine) parkBrowserWallAsk(ctx context.Context, runID, sessionID string, mode executionMode, reason string, send func(bridge.Event) error) error {
	if e == nil || e.tools == nil || sessionID == "" || send == nil {
		return nil
	}
	if reason == "" {
		reason = "login"
	}
	args := browserWallAskArgs(reason)
	callID := "ask-" + ulid.Make().String()
	pending, err := e.tools.Prepare(ctx, runID, sessionID, callID, "user.ask", args, toolruntime.Mode(mode), 10*time.Minute)
	if err != nil {
		log.Printf("browser-wall park Prepare failed: %v", err)
		return nil
	}
	return send(bridge.Event{
		Type: bridge.EventApprovalRequired,
		Tool: &bridge.ToolEvent{
			CallID:     pending.CallID,
			Name:       "user.ask",
			ArgsDigest: pending.ArgsDigest,
			Summary:    approvalRequiredSummary("user.ask", args),
		},
	})
}

func looksLikeFilePickerToolResult(summary string) bool {
	text := strings.TrimSpace(summary)
	if text == "" {
		return false
	}
	if strings.Contains(text, "needs_user") && (strings.Contains(text, "file_dialog") || strings.Contains(text, "选文件") || strings.Contains(text, "不能代你")) {
		return true
	}
	return strings.Contains(text, "请你点") && (strings.Contains(text, "保存") || strings.Contains(text, "打开"))
}

// parkFilePickerAsk stops the tool loop on an Open/Save dialog by preparing a
// real user.ask. The renderer already knows that tool; full-access companion
// will not auto-approve it. Prepare failure still ends the loop so the
// overlay fallback can ask the user.
func (e *Engine) parkFilePickerAsk(ctx context.Context, runID, sessionID string, mode executionMode, send func(bridge.Event) error) error {
	if e == nil || e.tools == nil || sessionID == "" || send == nil {
		return nil
	}
	callID := "ask-" + ulid.Make().String()
	pending, err := e.tools.Prepare(ctx, runID, sessionID, callID, "user.ask", filePickerAskArgs, toolruntime.Mode(mode), 10*time.Minute)
	if err != nil {
		log.Printf("file-picker park Prepare failed: %v", err)
		return nil
	}
	return send(bridge.Event{
		Type: bridge.EventApprovalRequired,
		Tool: &bridge.ToolEvent{
			CallID:     pending.CallID,
			Name:       "user.ask",
			ArgsDigest: pending.ArgsDigest,
			Summary:    approvalRequiredSummary("user.ask", filePickerAskArgs),
		},
	})
}
