package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

var localServerCommandRE = regexp.MustCompile(`(?i)(?:^|[\s` + "`" + `])(python3?|py|node)\s+([^\s` + "`" + `]+\.(?:py|js|mjs))`)

// userRunHandoffArgv is the interpreter and script a runnable-system reply
// tells the user to start. The product starts that exact command instead.
func userRunHandoffArgv(goal, text string) []string {
	if !runnableSystemRequest(goal) || !delegatesRunToUser(text) {
		return nil
	}
	m := localServerCommandRE.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	return []string{strings.ToLower(m[1]), strings.Trim(m[2], `"'`)}
}

func userRunHandoffScript(goal, text string) string {
	argv := userRunHandoffArgv(goal, text)
	if len(argv) != 2 {
		return ""
	}
	return argv[1]
}

func delegatesRunToUser(text string) bool {
	for _, k := range []string{"你需要做的", "外面执行", "自己执行", "在终端", "一条命令", "粘贴到外面"} {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func replaceUserRunHandoff(text, notice string) string {
	cut := len(text)
	for _, k := range []string{"未完成（被拦截）", "你需要做的", "一条命令"} {
		if i := strings.Index(text, k); i >= 0 && i < cut {
			cut = i
		}
	}
	head := strings.TrimSpace(text[:cut])
	notice = strings.TrimSpace(notice)
	if head == "" {
		return notice
	}
	if notice == "" {
		return head
	}
	return head + "\n\n" + notice
}

// finishRunnableDemo starts the server the model asked the user to paste,
// and returns the reply with that handoff removed.
func (e *Engine) finishRunnableDemo(ctx context.Context, mode executionMode, sessionID string, turn *chatTurnCheckpoint, assistant string, send func(bridge.Event) error) (string, bool) {
	if e == nil || e.tools == nil || turn == nil || ctx.Err() != nil {
		return "", false
	}
	argv := userRunHandoffArgv(turn.Goal, assistant)
	if len(argv) != 2 {
		return "", false
	}
	args, err := json.Marshal(map[string]any{"argv": argv})
	if err != nil {
		return "", false
	}
	callID := "demo-" + ulid.Make().String()
	if send != nil {
		_ = send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: "command.run", Summary: "正在启动演示服务"}})
	}
	r, runErr := e.executeUserTool(ctx, mode, sessionID, "command.run", args)
	summary := strings.TrimSpace(r.Output)
	if runErr != nil {
		summary = strings.TrimSpace(runErr.Error())
	}
	if send != nil {
		_ = send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: "command.run", Summary: clipToolSummary(summary)}})
		if summary != "" {
			_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "\n" + summary + "\n"}})
		}
	}
	if runErr == nil {
		turn.LastTools = append(turn.LastTools, "command.run")
		turn.ToolFailed = false
	}
	return replaceUserRunHandoff(assistant, summary), true
}
