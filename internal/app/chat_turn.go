package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/oklog/ulid/v2"
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
	Status          string                    `json:"status"`
	Goal            string                    `json:"goal"`
	StreamID        string                    `json:"streamId"`
	Injected        []string                  `json:"injected,omitempty"`
	QueueDeliveries []string                  `json:"queueDeliveries,omitempty"`
	LastTools       []string                  `json:"lastTools,omitempty"`
	ToolFailed      bool                      `json:"toolFailed,omitempty"`
	PptActive       bool                      `json:"pptActive,omitempty"`
	PptStage        string                    `json:"pptStage,omitempty"`
	PptTools        []string                  `json:"pptTools,omitempty"`
	PptNudges       int                       `json:"pptNudges,omitempty"`
	PptGenerated    bool                      `json:"pptGenerated,omitempty"`
	DocxActive      bool                      `json:"docxActive,omitempty"`
	DocxKind        string                    `json:"docxKind,omitempty"`
	DocxStage       string                    `json:"docxStage,omitempty"`
	DocxTools       []string                  `json:"docxTools,omitempty"`
	DocxNudges      int                       `json:"docxNudges,omitempty"`
	DocxGenerated   bool                      `json:"docxGenerated,omitempty"`
	DocxChars       int                       `json:"docxChars,omitempty"`
	PersistDraft    string                    `json:"persistDraft,omitempty"`
	PersistFailed   bool                      `json:"persistFailed,omitempty"`
	PersistUsage    messageapp.AssistantUsage `json:"persistUsage,omitempty"`
	UpdatedAt       string                    `json:"updatedAt"`
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
	ownerCtx, ownerCancel := turnJournalContext()
	defer ownerCancel()
	if exists, err := e.turnCheckpointSessionExists(ownerCtx, sessionID); err != nil || !exists {
		return chatTurnCheckpoint{}
	}
	if e != nil && e.turnJournal != nil {
		ctx, cancel := turnJournalContext()
		defer cancel()
		raw, err := e.turnJournal.LatestChatTurn(ctx, sessionID)
		if err != nil {
			log.Printf("chat turn journal read failed: %v", err)
			return chatTurnCheckpoint{}
		}
		if len(raw) > 0 {
			var cp chatTurnCheckpoint
			if json.Unmarshal(raw, &cp) == nil {
				return cp
			}
			return chatTurnCheckpoint{}
		}
	}
	return e.loadLegacyTurnCheckpoint(sessionID)
}

func (e *Engine) loadLegacyTurnCheckpoint(sessionID string) chatTurnCheckpoint {
	ownerCtx, ownerCancel := turnJournalContext()
	defer ownerCancel()
	if exists, err := e.turnCheckpointSessionExists(ownerCtx, sessionID); err != nil || !exists {
		return chatTurnCheckpoint{}
	}
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

func (e *Engine) saveTurnCheckpoint(sessionID string, cp chatTurnCheckpoint) error {
	if sessionID == "" {
		return nil
	}
	ctx, cancel := turnJournalContext()
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"sessionId": sessionID})
	release, scopeErr := e.authorizeDataRequest(ctx, "chat.checkpoint", payload)
	if scopeErr != nil {
		return scopeErr
	}
	defer release()
	cp.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if cp.StreamID == "" {
		cp.StreamID = ulid.Make().String()
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	if e.turnJournal != nil {
		return e.turnJournal.PutChatTurn(ctx, sessionID, cp.StreamID, raw, cp.PersistDraft != "")
	}
	path := e.turnCheckpointPath(sessionID)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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

func looksLikeStatusFollowUp(text string) bool {
	t := strings.TrimSpace(text)
	t = strings.TrimRight(t, "？?。.!！~… ")
	if t == "" {
		return false
	}
	if len([]rune(t)) > 40 {
		return false
	}
	lower := strings.ToLower(t)
	for _, p := range []string{
		"做好了没有", "做好了吗", "做完了没有", "做完了吗", "做完了没", "做好了没",
		"好了没有", "好了吗", "好了没", "完成了吗", "完成了没有", "弄好了吗",
		"还要多久", "要多久", "还要等吗", "等多久",
		"还在做吗", "在做吗", "还在跑吗", "还在吗",
		"什么进度", "到哪了", "到哪一步", "怎么样了", "如何了",
		"done yet", "is it done", "how long", "progress",
	} {
		if lower == p || strings.Contains(lower, p) {
			return true
		}
	}
	if t == "进度" || lower == "status" || lower == "eta" {
		return true
	}
	return false
}

func looksLikeSteer(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, p := range []string{"改方案", "改方向", "换个方向", "调整一下", "换个结构", "不要这样"} {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

func looksLikeIndependentRequest(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || looksLikeResume(t) || looksLikeStatusFollowUp(t) || looksLikeSteer(t) {
		return false
	}
	for _, p := range []string{
		"只要", "改成", "改用", "换成", "不要用", "别用", "补充", "再加上", "还有就是",
		"用这个", "继续用", "只装", "封面", "先做出", "先写", "加上", "别忘了", "记得",
	} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	return true
}

// looksLikeTaskChange detects a pivot away from the in-flight goal (negation or a
// clearly independent new ask). Supplements and progress checks must stay false.
func looksLikeTaskChange(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || looksLikeResume(t) || looksLikeStatusFollowUp(t) {
		return false
	}
	for _, p := range []string{
		"别做", "不要做", "不要", "别写", "别生成", "别用", "停止这个", "不做", "算了",
		"换话题", "换个话题", "别管", "先别", "别帮我做", "不用做",
	} {
		if strings.Contains(t, p) {
			return true
		}
	}
	return looksLikeIndependentRequest(text)
}

func followUpIntent(text string) string {
	switch {
	case looksLikeStatusFollowUp(text):
		return "progress"
	case looksLikeTaskChange(text):
		return "task_change"
	default:
		return "supplement"
	}
}

func closedLoopTurnInjection(userText string) string {
	if looksLikeResume(userText) || looksLikeStatusFollowUp(userText) || looksLikeSteer(userText) {
		return ""
	}
	return "\n\n[本轮范围] 只执行用户这一条最新消息。上一轮无论成功还是失败都已闭环，禁止重做，禁止和本轮绑在一起。用户没有说「继续」时，不要去完成聊天记录里更早的任务，也不要打开与本轮无关的文件。"
}

func (e *Engine) noteLiveTurnDraft(sessionID string, turn *chatTurnCheckpoint, text string, last *time.Time) error {
	text = strings.TrimSpace(text)
	if e == nil || turn == nil || sessionID == "" || text == "" {
		return nil
	}
	// Bound full-checkpoint writes by time or newly accumulated bytes; using
	// total text length made every delta after 80 characters hit SQLite.
	if last != nil && !last.IsZero() && time.Since(*last) < 750*time.Millisecond && len(text)-len(turn.PersistDraft) < 4096 {
		return nil
	}
	turn.PersistDraft = text
	if err := e.saveTurnCheckpoint(sessionID, *turn); err != nil {
		return err
	}
	if last != nil {
		*last = time.Now()
	}
	return nil
}

func handleChatTurnGet(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.SessionID) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.turn.get 参数无效", false)
	}
	if exists, err := e.turnCheckpointSessionExists(ctx, p.SessionID); err != nil {
		return messageFailure(request, err)
	} else if !exists {
		return request.Fail("SESSION_NOT_FOUND", "会话不存在", false)
	}
	cp := e.loadTurnCheckpoint(p.SessionID)
	draft := clipRunes(strings.TrimSpace(cp.PersistDraft), 8192)
	persistFailed := cp.PersistFailed && draft != ""
	unfinished := draft != "" && (cp.Status == turnStatusRunning || cp.Status == turnStatusInterrupted)
	if !persistFailed && !unfinished {
		draft = ""
	}
	status := strings.TrimSpace(cp.Status)
	switch status {
	case turnStatusRunning, turnStatusCompleted, turnStatusCancelled, turnStatusInterrupted:
	default:
		status = ""
	}
	return request.Ok(map[string]any{
		"status":        status,
		"persistFailed": persistFailed,
		"persistDraft":  draft,
	})
}

func (e *Engine) unfinishedTurnInjection(sessionID, userText string) string {
	if sessionID == "" {
		return ""
	}
	if !looksLikeResume(userText) && !looksLikeStatusFollowUp(userText) {
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

func (e *Engine) pullQueuedSupplements(ctx context.Context, sessionID string, cp *chatTurnCheckpoint) (string, []string, error) {
	if e == nil || e.queue == nil || sessionID == "" {
		return "", nil, nil
	}
	pending, err := e.queue.List(ctx, sessionID)
	if err != nil {
		return "", nil, err
	}
	for _, m := range pending {
		if looksLikeTaskChange(m.Payload) {
			return "", nil, nil
		}
	}
	var items []queueinput.Message
	var delivery queueapp.Delivery
	if store := e.queue.Deliveries(); store != nil {
		if cp == nil || cp.StreamID == "" || e.turnJournal == nil {
			return "", nil, queueapp.ErrDeliveryUnavailable
		}
		delivery, err = store.ClaimQueueDelivery(ctx, sessionID, cp.StreamID)
		if errors.Is(err, queueapp.ErrDeliveryBusy) {
			return "", nil, nil
		}
		if err == nil {
			for _, item := range delivery.Items {
				if looksLikeTaskChange(item.Payload) {
					_, handoffErr := store.RecoverQueueDelivery(ctx, sessionID, delivery.ID, "handoff")
					return "", nil, handoffErr
				}
			}
		}
		if err == nil {
			delivery, err = e.prepareQueueDelivery(ctx, delivery)
		}
		items = delivery.Items
	} else {
		items, err = e.queue.Consume(ctx, sessionID)
	}
	if err != nil || len(items) == 0 {
		return "", nil, err
	}
	texts := make([]string, 0, len(items))
	var b strings.Builder
	statusOnly := true
	for _, m := range items {
		if !looksLikeStatusFollowUp(m.Payload) && !looksLikeSteer(m.Payload) && !looksLikeResume(m.Payload) {
			statusOnly = false
			break
		}
	}
	if statusOnly {
		b.WriteString("用户在询问当前任务进度或微调方向。不要中断、不要重开任务。先用一两句话说明此刻进度（已完成步骤/正在做的步骤），若对方改了方案就按新方向调整，然后继续把原任务做完：\n")
	} else {
		b.WriteString("用户在任务进行中补充了以下说明，请结合当前正在做的工作一并执行，不要另起炉灶、不要丢弃已完成的步骤：\n")
	}
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
		return "", nil, nil
	}
	if cp != nil {
		already := false
		for _, id := range cp.QueueDeliveries {
			if id == delivery.ID && id != "" {
				already = true
			}
		}
		if !already {
			cp.Injected = append(cp.Injected, texts...)
			if delivery.ID != "" {
				cp.QueueDeliveries = append(cp.QueueDeliveries, delivery.ID)
			}
		}
		if err := e.saveTurnCheckpoint(sessionID, *cp); err != nil {
			return "", nil, err
		}
	}
	if delivery.ID != "" {
		if err := e.queue.Deliveries().StartQueueDelivery(ctx, sessionID, delivery.ID, cp.StreamID); err != nil {
			return "", nil, err
		}
	}
	return b.String(), texts, nil
}

func (e *Engine) applyQueuedSupplements(ctx context.Context, sessionID string, req *llmadapter.Request, cp *chatTurnCheckpoint, send func(bridge.Event) error, assistantText *strings.Builder) (bool, error) {
	if req.DisableReasoning || cp == nil {
		return false, nil
	}
	note, _, err := e.pullQueuedSupplements(ctx, sessionID, cp)
	if err != nil {
		return false, err
	}
	if note == "" {
		return false, nil
	}
	req.Messages = append(req.Messages, queuedSupplementMessage(note))
	assistantText.WriteString(queueInjectNotice)
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: "已收到你的补充，继续当前任务，不另起炉灶。\n"}})
	_ = send(bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: queueInjectNotice}})
	return true, nil
}

func queuedSupplementMessage(note string) llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleUser, Content: note}
}
