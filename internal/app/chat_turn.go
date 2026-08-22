package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/gateway"
)

const (
	turnStatusRunning     = "running"
	turnStatusCompleted   = "completed"
	turnStatusCancelled   = "cancelled"
	turnStatusInterrupted = "interrupted"
	queueInjectNotice     = "\n\n（已并入你刚才补充的说明，将结合当前任务继续执行。）\n\n"
	resumeUserPrompt      = "继续上次未完成的工作。结合任务清单、已完成步骤和我补充过的说明，接着做到完成。"
)

type chatTurnCheckpoint struct {
	Status     string   `json:"status"`
	Goal       string   `json:"goal"`
	StreamID   string   `json:"streamId"`
	Injected   []string `json:"injected,omitempty"`
	LastTools  []string `json:"lastTools,omitempty"`
	ToolFailed bool     `json:"toolFailed,omitempty"`
	UpdatedAt  string   `json:"updatedAt"`
}

func looksLikeResume(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if t == "继续" || strings.HasPrefix(t, "继续上次") {
		return true
	}
	return strings.Contains(t, "未完成的工作")
}

func (e *Engine) turnCheckpointPath(sessionID string) string {
	if e == nil || e.tools == nil || sessionID == "" {
		return ""
	}
	return filepath.Join(e.tools.WorkspaceRoot(), ".turns", sessionID+".json")
}

func (e *Engine) loadTurnCheckpoint(sessionID string) chatTurnCheckpoint {
	path := e.turnCheckpointPath(sessionID)
	if path == "" {
		return chatTurnCheckpoint{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return chatTurnCheckpoint{}
	}
	var cp chatTurnCheckpoint
	if json.Unmarshal(raw, &cp) != nil {
		return chatTurnCheckpoint{}
	}
	return cp
}

func (e *Engine) saveTurnCheckpoint(sessionID string, cp chatTurnCheckpoint) {
	path := e.turnCheckpointPath(sessionID)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	cp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.Marshal(cp)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func (e *Engine) todoSummary(sessionID string) string {
	path := e.turnCheckpointPath(sessionID)
	if path == "" || sessionID == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(e.tools.WorkspaceRoot(), ".todos", sessionID+".json"))
	if err != nil {
		return ""
	}
	var items []struct {
		Content  string `json:"content"`
		Status   string `json:"status"`
		Priority string `json:"priority"`
	}
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
		return ""
	}
	var b strings.Builder
	for i, t := range items {
		mark := " "
		if t.Status == "completed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "%d. [%s] %s (%s)\n", i+1, mark, t.Content, t.Status)
	}
	return b.String()
}

func looksLikeIndependentRequest(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || looksLikeResume(t) {
		return false
	}
	for _, p := range []string{"只要", "改成", "改用", "换成", "不要用", "别用", "补充", "再加上", "还有就是", "用这个", "继续用", "只装"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	return true
}

func closedLoopTurnInjection(userText string) string {
	if looksLikeResume(userText) {
		return ""
	}
	return "\n\n[本轮范围] 只执行用户这一条最新消息。上一轮无论成功还是失败都已闭环，禁止重做，禁止和本轮绑在一起。用户没有说「继续」时，不要去完成聊天记录里更早的任务，也不要打开与本轮无关的文件。"
}

func (e *Engine) unfinishedTurnInjection(sessionID, userText string) string {
	if sessionID == "" || !looksLikeResume(userText) {
		return ""
	}
	cp := e.loadTurnCheckpoint(sessionID)
	if cp.Status != turnStatusRunning && cp.Status != turnStatusInterrupted {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[未完成任务] 上次执行因中断未完成。从断点继续做到完成，不要从头再问一遍。")
	if strings.TrimSpace(cp.Goal) != "" {
		b.WriteString("\n原任务：")
		b.WriteString(strings.TrimSpace(cp.Goal))
	}
	for _, s := range cp.Injected {
		if strings.TrimSpace(s) == "" {
			continue
		}
		b.WriteString("\n已并入的补充：")
		b.WriteString(strings.TrimSpace(s))
	}
	if todos := e.todoSummary(sessionID); todos != "" {
		b.WriteString("\n任务清单：\n")
		b.WriteString(todos)
	}
	return b.String()
}

func (e *Engine) pullQueuedSupplements(ctx context.Context, sessionID string) (string, []string) {
	if e == nil || e.queue == nil || sessionID == "" {
		return "", nil
	}
	pending, err := e.queue.List(ctx, sessionID)
	if err != nil || len(pending) == 0 {
		return "", nil
	}
	for _, m := range pending {
		if looksLikeIndependentRequest(m.Payload) {
			return "", nil
		}
	}
	items, err := e.queue.Consume(ctx, sessionID)
	if err != nil || len(items) == 0 {
		return "", nil
	}
	texts := make([]string, 0, len(items))
	var b strings.Builder
	b.WriteString("用户在任务进行中补充了以下说明，请结合当前正在做的工作一并执行，不要另起炉灶、不要丢弃已完成的步骤：\n")
	for _, m := range items {
		if m.Status == queueinput.StatusWithdrawn {
			continue
		}
		text := strings.TrimSpace(m.Payload)
		if text == "" {
			continue
		}
		texts = append(texts, text)
		fmt.Fprintf(&b, "- %s\n", text)
	}
	if len(texts) == 0 {
		return "", nil
	}
	return b.String(), texts
}

func (e *Engine) applyQueuedSupplements(ctx context.Context, sessionID string, req *gateway.Request, cp *chatTurnCheckpoint, send func(bridge.Event) error, assistantText *strings.Builder) bool {
	if req.DisableReasoning || cp == nil {
		return false
	}
	note, texts := e.pullQueuedSupplements(ctx, sessionID)
	if note == "" {
		return false
	}
	req.Messages = append(req.Messages, queuedSupplementMessage(note))
	cp.Injected = append(cp.Injected, texts...)
	assistantText.WriteString(queueInjectNotice)
	_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: queueInjectNotice}})
	return true
}

func queuedSupplementMessage(note string) gateway.Message {
	return gateway.Message{Role: gateway.RoleUser, Content: note}
}
