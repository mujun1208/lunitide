package app

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

const openPageFileMark = "[正在看的页面文件]\n"
const openPageFileInstruction = "要改这个页面或给它加数据，先 workspace.read 读这个文件，再用 workspace.edit 改这个文件本身。"
const openPageReadInstruction = "这是当前打开的文件。要看页面上的内容，用 workspace.read 读这个文件，不要把页面原文抄进对话。"

// openPageFileFromMessages is the local file the side browser is showing.
// The instruction line must follow the path, so a page that merely displays
// a path does not become writable.
func openPageFileFromMessages(messages []llmadapter.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llmadapter.RoleUser {
			continue
		}
		if path := parseOpenPageFile(messages[i].Content); path != "" {
			return path
		}
	}
	return ""
}

func parseOpenPageFile(content string) string {
	i := strings.Index(content, openPageFileMark)
	if i < 0 {
		return ""
	}
	rest := content[i+len(openPageFileMark):]
	lines := strings.SplitN(rest, "\n", 3)
	if len(lines) < 2 {
		return ""
	}
	path := strings.TrimSpace(lines[0])
	line := strings.TrimSpace(lines[1])
	if !strings.HasPrefix(line, openPageFileInstruction) && !strings.HasPrefix(line, openPageReadInstruction) {
		return ""
	}
	if !validOpenPagePath(path) {
		return ""
	}
	return path
}

func userAskedToChangeOpenPage(content string) bool {
	raw := content
	if i := strings.Index(raw, openPageFileMark); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	for _, k := range []string{"新增", "添加", "加上", "加一条", "加个", "加数据", "加入", "加到", "加进", "修改", "改一下", "写入", "删掉", "删除", "去掉", "模块"} {
		if strings.Contains(raw, k) {
			return true
		}
	}
	return strings.Contains(raw, "加") && (strings.Contains(raw, "数据") || strings.Contains(raw, "商机") || strings.Contains(raw, "客户"))
}

func stripBrowserPageDump(content string) string {
	for _, mark := range []string{"\n\n[浏览器页面]\n", "\n[浏览器页面]\n", "[浏览器页面]\n"} {
		if i := strings.Index(content, mark); i >= 0 {
			return strings.TrimSpace(content[:i])
		}
	}
	return content
}

// slimOpenPageMessages drops the rendered page text whenever the open file
// is named. The model reads that file. The chat does not carry the table.
func slimOpenPageMessages(messages []llmadapter.Message) []llmadapter.Message {
	if len(messages) == 0 {
		return messages
	}
	out := append([]llmadapter.Message(nil), messages...)
	for i := range out {
		if out[i].Role != llmadapter.RoleUser || !strings.Contains(out[i].Content, openPageFileMark) {
			continue
		}
		out[i].Content = stripBrowserPageDump(out[i].Content)
	}
	return out
}

// The canned end-of-turn notices. Constants (not inline literals) so the
// producer and the classifier below can never drift apart again: a visual
// mismatch between two copies of the same sentence once made the classifier
// read the notice as factual speech.
const (
	cannedNoAnswerReceipt   = "这一轮没有回答。\n"
	cannedIncompleteReceipt = "这一轮没有完成。\n"
)

// silentTurnReceipt is the one sentence a finished turn still owes when the
// model returned no words. A landed page edit says so. An edit that never
// landed says so. A blank bubble is not a result.
func silentTurnReceipt(messages []llmadapter.Message, tools []string, toolFailed bool) string {
	if pageChangeAlreadyLanded(messages) {
		return "已经写进正在看的页面。\n"
	}
	if openPageFileFromMessages(messages) != "" && userAskedToChangeOpenPage(lastUserChatText(messages)) {
		return "还没改成正在看的页面。\n"
	}
	if toolFailed {
		if notice := createTurnFailureNotice(tools, ""); notice != "" {
			return notice
		}
		return cannedIncompleteReceipt
	}
	if taskAlreadyLanded(messages, tools) {
		return "已经做完。\n"
	}
	if len(tools) > 0 {
		return cannedNoAnswerReceipt
	}
	return ""
}

// blankTurnFallbackReceipt reports whether the receipt is the canned
// no-answer notice rather than a factual statement about landed or failed
// work. The canned notice is the last resort: the forced end-of-turn
// summary pass may still return real words, so the caller defers it
// instead of letting it preempt that pass.
func blankTurnFallbackReceipt(speech string) bool {
	trimmed := strings.TrimSpace(speech)
	return trimmed == strings.TrimSpace(cannedNoAnswerReceipt) || trimmed == strings.TrimSpace(cannedIncompleteReceipt)
}

func openPageChangePending(messages []llmadapter.Message, lastTools []string) bool {
	if pageChangeAlreadyLanded(messages) {
		return false
	}
	if openPageFileFromMessages(messages) == "" {
		return false
	}
	if !userAskedToChangeOpenPage(lastUserChatText(messages)) {
		return false
	}
	_ = lastTools
	return true
}

const maxOpenPageEditNudges = 8
const openPageEditNudgeText = "正在看的文件还没改成。不要对用户说没成功，不要请用户重试。只改正在看的这个文件。现在调用 workspace.edit 把用户要的改动写进去。改完只用一句话说明改了哪一处。"

func modelAskedTheUserToRetry(text string) bool {
	for _, s := range []string{"请再说具体一点", "这次操作没成功", "无法执行", "请稍后重试"} {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// providerRejectedRequest is an upstream invalid request. A stream
// invalid_request_error arrives with HTTP status 0, not 400.
func providerRejectedRequest(err error) bool {
	var gatewayErr *llmadapter.Error
	if !errors.As(err, &gatewayErr) || thinkingParameterRejected(gatewayErr.Message) {
		return false
	}
	if gatewayErr.HTTPStatus == 400 {
		return true
	}
	switch gatewayErr.Code {
	case "STREAM_BAD_REQUEST", "HTTP_400", "BAD_REQUEST":
		return true
	}
	return false
}

// supplierRejectShouldContinue is a supplier refusal of this request.
// Every task compacts and continues. An empty HTTP 400 before any stream
// text still drops unsupported tools, and that check runs first.
func supplierRejectShouldContinue(err error) bool {
	return providerRejectedRequest(err)
}

func taskAlreadyLanded(messages []llmadapter.Message, tools []string) bool {
	if pageChangeAlreadyLanded(messages) {
		return true
	}
	return usedAnyTool(tools, "docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "workspace.edit", "workspace.write", "canvas.present")
}

func pageChangeAlreadyLanded(messages []llmadapter.Message) bool {
	for _, m := range messages {
		if m.Role != llmadapter.RoleTool || strings.Contains(m.Content, "ok:false") {
			continue
		}
		if strings.Contains(m.Content, "replacement") || strings.Contains(m.Content, "edited ") {
			return true
		}
	}
	return false
}

func openPageEditNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: openPageEditNudgeText}
}

func validOpenPagePath(path string) bool {
	if path == "" || len(path) > 1024 || strings.Contains(path, "..") {
		return false
	}
	if strings.ContainsAny(path, "\x00\n?*\"<>|") {
		return false
	}
	if !filepath.IsAbs(path) && filepath.VolumeName(path) == "" {
		return false
	}
	return true
}
