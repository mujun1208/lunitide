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
