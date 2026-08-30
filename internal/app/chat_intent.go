package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
)

var typeAfterWriteRe = regexp.MustCompile(`(?:.*?(?:在|对))?([^，,。！？]+?)(?:后面|之后)(?:写上|写入|写|填|输入)(.+)`)

func normalizeTypeAfterLabel(after string) string {
	after = strings.TrimSpace(after)
	for _, prefix := range []string{"文档的", "文件里的", "表格中的", "这一栏的", "那一格的"} {
		if strings.HasPrefix(after, prefix) {
			after = strings.TrimPrefix(after, prefix)
		}
	}
	return strings.TrimSpace(after)
}

func parseDesktopTypeArgsFromGoal(goal string) (after, text string, ok bool) {
	m := typeAfterWriteRe.FindStringSubmatch(strings.TrimSpace(goal))
	if len(m) < 3 {
		return "", "", false
	}
	after = normalizeTypeAfterLabel(m[1])
	text = strings.TrimSpace(m[2])
	if after == "" || text == "" {
		return "", "", false
	}
	return after, text, true
}

func fallbackDesktopTypeArgs(goal string) json.RawMessage {
	after, text, ok := parseDesktopTypeArgsFromGoal(goal)
	if !ok {
		return nil
	}
	window := ""
	if strings.Contains(goal, "协议") || strings.Contains(goal, "劳动") || strings.Contains(goal, "合同") {
		window = "协议"
	}
	raw, _ := json.Marshal(map[string]any{
		"text": text, "after": after, "window": window,
	})
	return raw
}

// officeGenInternalHint is the model-only reminder for writing Office
// files onto the Desktop. It must never appear in assistant deltas,
// 无法执行 banners, or mid-markdown fences.
const officeGenInternalHint = "写到桌面请用对应 *.gen 工具并设 desktop=true，不要用 command.run。"

// followUpIntent classifies queued input during an in-flight turn for the engine.
// Returns progress, supplement, or task_change (see followUpIntent in chat_turn.go).
func classifyFollowUpIntent(text string) string {
	return followUpIntent(text)
}

func looksLikePlayOrOpenTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	play := strings.Contains(t, "播") || strings.Contains(t, "放一首") || strings.Contains(t, "随便") ||
		strings.Contains(lower, "play") || strings.Contains(t, "随机播放")
	music := strings.Contains(t, "汽水") || strings.Contains(t, "网易云") || strings.Contains(t, "音乐") ||
		strings.Contains(t, "听歌") || strings.Contains(lower, "music")
	openDoc := (strings.Contains(t, "打开") || strings.Contains(t, "把开")) &&
		(strings.Contains(t, "桌面") || strings.Contains(t, "协议") || strings.Contains(t, "文档") || strings.Contains(t, "文件"))
	return (play && music) || openDoc
}

func looksLikeTypeAfterLabelTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if _, _, ok := parseDesktopTypeArgsFromGoal(t); ok {
		return true
	}
	return (strings.Contains(t, "后面") || strings.Contains(t, "之后") || strings.Contains(t, "填写") || strings.Contains(t, "写入") || strings.Contains(t, "输入")) &&
		(strings.Contains(t, "号码") || strings.Contains(t, "证件") || strings.Contains(t, "身份证") || strings.Contains(t, "住址") || strings.Contains(t, "电话") || strings.Contains(t, "文档"))
}

func looksLikeWeatherTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	return strings.Contains(t, "天气") || strings.Contains(t, "气温") || strings.Contains(t, "温度") ||
		strings.Contains(lower, "weather") || strings.Contains(t, "查天气")
}

func looksLikeComputerControlTurn(text string) bool {
	return looksLikePlayOrOpenTurn(text) || looksLikeTypeAfterLabelTurn(text) || looksLikeWeatherTurn(text)
}

func looksLikeExcelTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	for _, k := range []string{"excel", "xlsx", "表格", "财报", "工作簿"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return looksLikeHardwareBom(text)
}

func looksLikeHardwareBom(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	if strings.Contains(t, "bom") || strings.Contains(t, "物料清单") {
		return true
	}
	return strings.Contains(t, "硬件") && (strings.Contains(t, "配置") || strings.Contains(t, "选型") || strings.Contains(t, "清单"))
}

func looksLikeHtmlGenTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	return strings.Contains(t, "html") || strings.Contains(t, "小游戏") || strings.Contains(t, "点球")
}

func wantsOfficeGen(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	if looksLikePptTask(text) || looksLikeReportTask(text) || looksLikeNovelTask(text) ||
		looksLikeExcelTask(text) || looksLikeHtmlGenTask(text) {
		return true
	}
	for _, k := range []string{
		"ppt", "pptx", "幻灯", "演示", "docx", "word", "报告", "小说",
	} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func wantsOfficeFileOnDesktop(text string) bool {
	if !wantsOfficeGen(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(t, "桌面") || strings.Contains(t, "desktop") ||
		strings.Contains(t, "放到桌面") || strings.Contains(t, "输出到桌面")
}

func looksLikeArchitectMermaidTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || wantsOfficeGen(t) {
		return false
	}
	return strings.Contains(t, "架构") || strings.Contains(t, "系统架构") ||
		strings.Contains(strings.ToLower(t), "architect") || strings.Contains(t, "架构师")
}

func includeOfficeGenWorkflow(turnText string) bool {
	if strings.TrimSpace(turnText) == "" {
		return false
	}
	if looksLikeComputerControlTurn(turnText) && !wantsOfficeGen(turnText) {
		return false
	}
	if looksLikeArchitectMermaidTurn(turnText) {
		return false
	}
	return wantsOfficeGen(turnText)
}

func hasOfficeGenTool(tools []string) bool {
	for _, name := range tools {
		switch name {
		case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen":
			return true
		}
	}
	return false
}

func usedCommandRun(tools []string) bool {
	for _, name := range tools {
		if name == "command.run" {
			return true
		}
	}
	return false
}

func closeOpenMarkdownFences(text string) string {
	if text == "" {
		return text
	}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			n++
		}
	}
	if n%2 == 0 {
		return text
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "```\n"
}

func clipOfficeTitle(goal string, fallback string) string {
	t := strings.TrimSpace(goal)
	t = strings.TrimPrefix(t, "帮我")
	t = strings.TrimPrefix(t, "请")
	t = strings.TrimSpace(t)
	if t == "" {
		return fallback
	}
	if utf8.RuneCountInString(t) > 40 {
		return string([]rune(t)[:40])
	}
	return t
}

func stripOfficeGenLecture(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, officeGenInternalHint, "")
	s = strings.ReplaceAll(s, "写到桌面请用对应 *.gen 工具并设 desktop=true，不要用 command.run。", "")
	return s
}

func userVisibleToolSummary(summary string) string {
	if strings.Contains(summary, officeGenInternalHint) ||
		(strings.Contains(summary, "*.gen") && strings.Contains(summary, "desktop=true")) ||
		strings.Contains(summary, "写到桌面请用对应") {
		return "正在生成到桌面…"
	}
	return summary
}

func hasComputerControlTool(tools []string) bool {
	for _, name := range tools {
		switch name {
		case "desktop.open", "desktop.type", "media.play":
			return true
		}
		if strings.HasPrefix(name, "cc.") || name == "computer.act" {
			return true
		}
	}
	return false
}

func sanitizeUserVisibleText(text string) string {
	if text == "" {
		return text
	}
	if strings.Contains(text, officeGenInternalHint) ||
		strings.Contains(text, "写到桌面请用") ||
		strings.Contains(text, "不要用 command.run") {
		return strings.TrimSpace(stripOfficeGenLecture(text))
	}
	return text
}

func sanitizeUserVisibleNotice(notice string) string {
	notice = sanitizeUserVisibleText(notice)
	if notice == "" {
		return notice
	}
	if strings.Contains(notice, "*.gen") || strings.Contains(notice, "desktop=true") {
		if strings.HasPrefix(notice, turnErrorNotice) {
			return turnErrorNotice + "生成失败，请重试。"
		}
		return "生成失败，请重试。"
	}
	if notice == turnErrorNotice {
		return turnErrorNotice + "生成失败，请重试。"
	}
	return notice
}

func sanitizeOutgoingEvent(ev *bridge.Event) {
	if ev == nil {
		return
	}
	switch ev.Type {
	case bridge.EventDelta:
		if ev.Delta != nil {
			ev.Delta.Text = sanitizeUserVisibleText(ev.Delta.Text)
		}
	case bridge.EventToolStarted, bridge.EventToolCompleted, bridge.EventToolOutput:
		if ev.Tool != nil && ev.Tool.Summary != "" {
			ev.Tool.Summary = userVisibleToolSummary(ev.Tool.Summary)
		}
	}
}

func isCompanionLeadInOnly(text string) bool {
	t := strings.TrimSpace(text)
	t = strings.TrimRight(t, "。.!！ ")
	if t == "" {
		return true
	}
	for _, p := range []string{
		"等一下", "稍等", "稍等我一下", "嗯，我在呢，稍等我一下",
		"好，我帮你查一下", "好，我来执行", "好，我来打开", "好，我来播放",
		"好，我来输入", "好，我马上处理", "好，我来操作电脑", "嗯，", "嗯",
	} {
		if t == strings.TrimRight(p, "。.!！ ") || t == p {
			return true
		}
	}
	return utf8.RuneCountInString(t) <= 6 && (strings.Contains(t, "等") || strings.Contains(t, "好"))
}
