package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

const maxWindowRetries = 8

// supplierCompactWaves is the last resort after a refusal. The request is
// already folded at 90% of the window, so two tighter passes are enough.
const supplierCompactWaves = 2

// modelRequestKeep is the recent working set kept when the request is over
// 90% of the model window. Shorter threads stay whole.
const modelRequestKeep = 8

const taskContinueAfterFoldText = "较早的对话已收起。供应商拒绝过的是过长的请求，不是这个任务。推理档位不变，工具还在。继续完成当前用户请求。不要复述已经写过的说明，不要对用户说无法执行。计划清单继续执行，需要拍板时仍用 user.ask。"

// providerModelContextWindow reads the configured window for this model.
// Missing configuration keeps the historical 128000 fallback. The bool is
// true only when the provider row itself supplied a positive window.
func providerModelContextWindow(p provider.Provider, modelID string) (int64, bool) {
	for _, m := range p.Models {
		if m.ModelID == modelID && m.ContextWindow > 0 {
			return m.ContextWindow, true
		}
	}
	return 128000, false
}

func (e *Engine) compactSession(ctx context.Context, sessionID string, p provider.Provider, modelID, tokenizerRevision string) {
	if e == nil || e.compactionTrigger == nil || e.compactionExecutor == nil {
		return
	}
	window, _ := providerModelContextWindow(p, modelID)
	if strings.TrimSpace(tokenizerRevision) == "" {
		tokenizerRevision = token.CanonicalTokenizerRevision
	}
	result := e.TriggerPreTurnCompaction(ctx, sessionID, p.ID, modelID, tokenizerRevision, window)
	if result.Err != nil {
		log.Printf("session compaction: session=%s model=%s window=%d: %s", sessionID, modelID, window, result.Reason)
	}
}

func (e *Engine) latestCheckpointSummary(ctx context.Context, sessionID string) string {
	if e == nil || e.summaryReader == nil || sessionID == "" {
		return ""
	}
	if cr, ok := e.summaryReader.(compactionCoverageReader); ok {
		summary, _, err := cr.GetLatestCompactionCheckpoint(ctx, sessionID)
		if err == nil && strings.TrimSpace(summary) != "" {
			return summary
		}
	}
	summary, err := e.summaryReader.GetLatestCompactionSummary(ctx, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

// isContextOverflowError is a request that does not fit. Those retry by
// compacting history. A truncated reply is not a window overflow: the
// partial stays, and the turn continues so the file can still be written.
func isContextOverflowError(err error) bool {
	if errors.Is(err, agentrun.ErrContextWindow) {
		return true
	}
	var upstream *llmadapter.Error
	if !errors.As(err, &upstream) {
		return false
	}
	switch upstream.Code {
	case "CONTEXT_WINDOW_EXCEEDED", "REQUEST_TOO_LARGE":
		return true
	}
	return upstream.HTTPStatus == 413
}

func isWindowOverflowError(err error) bool {
	return isContextOverflowError(err)
}

func isReplyTruncatedError(err error) bool {
	var upstream *llmadapter.Error
	return errors.As(err, &upstream) && upstream.Code == "RESPONSE_TRUNCATED"
}

func isResponseWireLimitError(err error) bool {
	var upstream *llmadapter.Error
	return errors.As(err, &upstream) && upstream.Code == "RESPONSE_BODY_TOO_LARGE"
}

func shrinkMessagesForWindowRetry(req *llmadapter.Request) {
	applyWindowRetryMessages(req, "")
}

func windowRetryKeep(attempt int) int {
	switch attempt {
	case 1:
		return 8
	case 2:
		return 4
	default:
		return 2
	}
}

func applyWindowRetryMessages(req *llmadapter.Request, checkpointSummary string) {
	applyWindowRetryMessagesKeep(req, checkpointSummary, 8)
}

func applyWindowRetryMessagesKeep(req *llmadapter.Request, checkpointSummary string, keep int) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	if keep < 1 {
		keep = 1
	}
	var systems, rest []llmadapter.Message
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem {
			systems = append(systems, m)
			continue
		}
		rest = append(rest, m)
	}
	if len(rest) > keep {
		rest = rest[len(rest)-keep:]
	}
	note := "较早的对话已写入检查点摘要。不要声称记得未出现的细节。继续完成当前用户请求。"
	if summary := strings.TrimSpace(checkpointSummary); summary != "" {
		note = "较早的对话已写入检查点摘要：\n" + summary + "\n不要声称记得未出现的细节。继续完成当前用户请求。"
	}
	req.Messages = append(append([]llmadapter.Message{}, systems...), llmadapter.Message{Role: llmadapter.RoleSystem, Content: note})
	req.Messages = append(req.Messages, rest...)
}

func clipWindowRetryPayloads(req *llmadapter.Request, attempt int) {
	if req == nil || attempt < 2 {
		return
	}
	limit := 4000
	if attempt >= 3 {
		limit = 1500
	}
	for i := range req.Messages {
		if req.Messages[i].Role != llmadapter.RoleTool && req.Messages[i].Role != llmadapter.RoleAssistant {
			continue
		}
		if utf8.RuneCountInString(req.Messages[i].Content) <= limit {
			continue
		}
		req.Messages[i].Content = string([]rune(req.Messages[i].Content)[:limit]) + "\n…（已压缩）"
	}
}

func dropWindowRetryImages(req *llmadapter.Request) {
	if req == nil {
		return
	}
	req.Images = nil
}

func windowOverflowUserMessage() string {
	return "较早的对话已经收起，这一步会接着做。"
}

func windowRetryThinkingNotice() string {
	return "较早的对话已经收起，正在接着做当前这一步。"
}

// fitModelRequest runs before every model call. Old tool arguments are
// cleared first. The thread is folded only when the estimate passes 90% of
// the model window, and the fold keeps a task card plus the latest steps.
// The selected reasoning level and the tool list stay as they are.
func fitModelRequest(req *llmadapter.Request, window int64) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	req.Messages = slimOpenPageMessages(req.Messages)
	stubExecutedToolArgs(req)
	clipOlderPayloads(req)
	if !requestOverBudget(req, window) {
		return
	}
	foldOldModelMessages(req, modelRequestKeep)
	if requestOverBudget(req, window) {
		clipLastUser(req, 4000)
		clipLastTool(req, 1500)
	}
}

func requestOverBudget(req *llmadapter.Request, window int64) bool {
	if window < 1 {
		window = 128000
	}
	limit := window * 9 / 10
	if limit < 1 {
		limit = 1
	}
	return estimateRequestTokens(req) > limit
}

func estimateRequestTokens(req *llmadapter.Request) int64 {
	if req == nil {
		return 1
	}
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
		for _, call := range m.ToolCalls {
			n += len(call.Name) + len(call.Arguments)
		}
	}
	for _, tool := range req.Tools {
		n += len(tool.Name) + len(tool.Description) + len(tool.Schema)
	}
	tokens := int64((n + 3) / 4)
	if tokens < 1 {
		return 1
	}
	return tokens
}

func taskHandoffCard(messages []llmadapter.Message) string {
	var b strings.Builder
	b.WriteString("任务卡\n当前要求：")
	goal := lastUserChatText(messages)
	if utf8.RuneCountInString(goal) > 400 {
		goal = string([]rune(goal)[:400])
	}
	b.WriteString(goal)
	b.WriteByte('\n')
	if path := openPageFileFromMessages(messages); path != "" {
		b.WriteString("正在看的文件：")
		b.WriteString(path)
		b.WriteByte('\n')
	}
	if writing := keptWritingFromMessages(messages); writing != "" {
		b.WriteString("已写内容：\n")
		b.WriteString(writing)
		b.WriteByte('\n')
	}
	if steps := todoCardLines(messages); steps != "" {
		b.WriteString("剩余步骤：\n")
		b.WriteString(steps)
	}
	if paths := landedPathLines(messages); paths != "" {
		b.WriteString("关键路径：")
		b.WriteString(paths)
		b.WriteByte('\n')
	}
	b.WriteString(taskContinueAfterFoldText)
	return b.String()
}

func todoCardLines(messages []llmadapter.Message) string {
	var raw []byte
	for _, m := range messages {
		if m.Role != llmadapter.RoleAssistant {
			continue
		}
		for _, call := range m.ToolCalls {
			if call.Name == "todo.write" && len(call.Arguments) > 0 {
				raw = call.Arguments
			}
		}
	}
	if len(raw) == 0 {
		return ""
	}
	var body struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, item := range body.Todos {
		if item.Status == "completed" || strings.TrimSpace(item.Content) == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(item.Content)
		b.WriteByte('\n')
		n++
		if n == 8 {
			break
		}
	}
	return b.String()
}

func landedPathLines(messages []llmadapter.Message) string {
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] || len(paths) >= 5 {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, m := range messages {
		for _, call := range m.ToolCalls {
			var payload map[string]any
			if json.Unmarshal(call.Arguments, &payload) == nil {
				if p, _ := payload["path"].(string); p != "" {
					add(p)
				}
			}
		}
		if m.Role != llmadapter.RoleTool {
			continue
		}
		line := m.Content
		for _, prefix := range []string{"edited ", "wrote ", "written "} {
			if strings.Contains(line, prefix) {
				rest := line[strings.Index(line, prefix)+len(prefix):]
				rest = strings.TrimSpace(rest)
				if i := strings.IndexAny(rest, " (\n"); i > 0 {
					rest = rest[:i]
				}
				add(rest)
			}
		}
	}
	return strings.Join(paths, "、")
}

func keptWritingFromMessages(messages []llmadapter.Message) string {
	found := ""
	for _, m := range messages {
		for _, mark := range []string{"已写内容如下", "已写内容（不要重写"} {
			i := strings.Index(m.Content, mark)
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(m.Content[i+len(mark):])
			rest = strings.TrimLeft(rest, "：:\n ")
			if rest != "" {
				found = rest
			}
		}
	}
	if found == "" {
		return ""
	}
	return clipSentRunes(found, 800)
}

func clipLastUser(req *llmadapter.Request, n int) {
	i := lastUserMessage(req.Messages)
	if i < 0 {
		return
	}
	// The current question and attached files sit at the end of this turn.
	// A prefix clip keeps the old history and drops those files.
	head := n / 5
	if head < 200 {
		head = 200
	}
	tail := n - head
	if tail < head {
		tail = head
	}
	content := req.Messages[i].Content
	const mark = "\n\n[Untrusted Attachment Data"
	if idx := strings.Index(content, mark); idx >= 0 {
		req.Messages[i].Content = clipEndsRunes(content[:idx], head, tail) + content[idx:]
		return
	}
	req.Messages[i].Content = clipEndsRunes(content, head, tail)
}

func clipLastTool(req *llmadapter.Request, n int) {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role != llmadapter.RoleTool {
			continue
		}
		if skillAgreementMessage(req.Messages[i].Content) {
			return
		}
		if openPageChangePending(req.Messages, nil) {
			req.Messages[i].Content = clipEndsRunes(req.Messages[i].Content, 1500, 1500)
			return
		}
		req.Messages[i].Content = clipSentRunes(req.Messages[i].Content, n)
		return
	}
}

func stubExecutedToolArgs(req *llmadapter.Request) {
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Role != llmadapter.RoleAssistant {
			continue
		}
		for j := range m.ToolCalls {
			call := &m.ToolCalls[j]
			if utf8.RuneCountInString(string(call.Arguments)) <= 800 {
				continue
			}
			path := ""
			var payload map[string]any
			if json.Unmarshal(call.Arguments, &payload) == nil {
				if p, _ := payload["path"].(string); p != "" {
					path = p
				}
			}
			note := map[string]string{"note": "已执行，原文已收起"}
			if path != "" {
				note["path"] = path
			}
			if raw, err := json.Marshal(note); err == nil {
				call.Arguments = raw
			}
		}
	}
}

func clipOlderPayloads(req *llmadapter.Request) {
	lastAssistant, lastTool := -1, -1
	for i := range req.Messages {
		switch req.Messages[i].Role {
		case llmadapter.RoleAssistant:
			lastAssistant = i
		case llmadapter.RoleTool:
			lastTool = i
		}
	}
	for i := range req.Messages {
		m := &req.Messages[i]
		switch m.Role {
		case llmadapter.RoleAssistant:
			limit := 500
			if i == lastAssistant {
				limit = 2000
			}
			m.Content = clipSentRunes(m.Content, limit)
		case llmadapter.RoleTool:
			if skillAgreementMessage(m.Content) {
				continue
			}
			if openPageChangePending(req.Messages, nil) {
				if i == lastTool {
					m.Content = clipEndsRunes(m.Content, 4000, 4000)
				} else {
					m.Content = clipEndsRunes(m.Content, 2000, 2000)
				}
				continue
			}
			limit := 400
			if i == lastTool {
				limit = 3000
			}
			m.Content = clipSentRunes(m.Content, limit)
		case llmadapter.RoleUser:
			if i == lastUserMessage(req.Messages) {
				continue
			}
			const mark = "\n\n[Untrusted Attachment Data"
			if idx := strings.Index(m.Content, mark); idx >= 0 {
				m.Content = clipEndsRunes(m.Content[:idx], 400, 400) + m.Content[idx:]
				continue
			}
			m.Content = clipSentRunes(m.Content, 500)
		}
	}
}

func lastUserMessage(messages []llmadapter.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			return i
		}
	}
	return -1
}

func skillAgreementMessage(content string) bool {
	return strings.Contains(content, "manifestDigest=")
}

func clipSentRunes(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "\n…（已收起）"
}

func clipEndsRunes(s string, head, tail int) string {
	if head < 1 {
		head = 1
	}
	if tail < 1 {
		tail = 1
	}
	runes := []rune(s)
	if len(runes) <= head+tail+20 {
		return s
	}
	return string(runes[:head]) + "\n…\n" + string(runes[len(runes)-tail:])
}

func foldOldModelMessages(req *llmadapter.Request, keep int) {
	if req == nil {
		return
	}
	if keep < 2 {
		keep = 2
	}
	var systems, rest []llmadapter.Message
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem {
			systems = append(systems, m)
			continue
		}
		rest = append(rest, m)
	}
	if len(rest) <= keep {
		return
	}
	card := taskHandoffCard(append(append([]llmadapter.Message{}, systems...), rest...))
	lastUser := -1
	todoAt := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if lastUser < 0 && rest[i].Role == llmadapter.RoleUser {
			lastUser = i
		}
		if todoAt < 0 && messageHasTodo(rest[i]) {
			todoAt = i
		}
	}
	start := len(rest) - keep
	tail := append([]llmadapter.Message{}, rest[start:]...)
	pin := func(i int) {
		if i < 0 || i >= start {
			return
		}
		tail = append([]llmadapter.Message{rest[i]}, tail...)
	}
	pin(todoAt)
	pin(lastUser)
	if !messagesHaveCard(systems) {
		systems = append(systems, llmadapter.Message{Role: llmadapter.RoleSystem, Content: card})
	}
	req.Messages = append(append([]llmadapter.Message{}, systems...), tail...)
}

func messageHasTodo(m llmadapter.Message) bool {
	for _, call := range m.ToolCalls {
		if call.Name == "todo.write" {
			return true
		}
	}
	return false
}

func messagesHaveCard(messages []llmadapter.Message) bool {
	for _, m := range messages {
		if m.Role == llmadapter.RoleSystem && strings.HasPrefix(m.Content, "任务卡\n") {
			return true
		}
	}
	return false
}

func companionColdMaxMessages(hasCheckpoint bool) int {
	if hasCheckpoint {
		return companionMaxMessages
	}
	return companionMaxMessages / 2
}

func discardStepText(builder *strings.Builder, stepStart int) {
	if builder == nil || stepStart < 0 || stepStart > builder.Len() {
		return
	}
	kept := builder.String()[:stepStart]
	builder.Reset()
	builder.WriteString(kept)
}
