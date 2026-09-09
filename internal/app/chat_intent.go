package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

var typeAfterWriteRe = regexp.MustCompile(`(?:.*?(?:在|对))?([^，,。！？]+?)(?:后面|之后)(?:写上|写入|写|填|输入)(.+)`)

var lookupOptOutRe = regexp.MustCompile(`(?i)(?:不(?:要|必|用|需要|需|得|能)?|别|无需|禁止)\s*(?:联网|上网|搜索|检索)|(?:do\s+not|don't|never|without|no)\s+(?:web\s+|online\s+|internet\s+)?(?:search|brows|lookup|look\s+up|fetch)|\boffline\b`)
var lookupOfflineRe = regexp.MustCompile(`(?i)(?:不(?:要|必|用|需要|需|得|能)?|别|无需|禁止)\s*(?:联网|上网)|\boffline\b`)
var lookupActionRe = regexp.MustCompile(`(?i)(?:^|[，,。；;\n]|然后|接着|并且|再|并|\band\s+|\bthen\s+)(?:\s*(?:请你|请|帮我|先|再|please)\s*)*(?:联网|上网|搜索|检索|查询|查找|查|search\b|browse\b|look\s+up\b|fetch\b)`)

func lookupOptedOut(text string) bool {
	if !lookupOptOutRe.MatchString(text) {
		return false
	}
	if lookupOfflineRe.MatchString(text) {
		return true
	}
	// A scoped rejection may be followed by a different, explicit lookup.
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("，,。；;\n", r)
	}) {
		if !lookupOptOutRe.MatchString(clause) && lookupActionRe.MatchString(clause) {
			return false
		}
	}
	return true
}

func referenceOnlyOfficeTurn(text string) bool {
	if !wantsOfficeGen(text) {
		return false
	}
	lower := strings.ToLower(text)
	if lookupActionRe.MatchString(lower) {
		return false
	}
	for _, reference := range []string{"已提供", "已生成", "已完成", "已有", "现有", "上述", "以上", "附件", "转为", "转换", "整理成", "provided", "existing", "attached", "convert"} {
		if strings.Contains(lower, reference) {
			return true
		}
	}
	return false
}

func normalizeTypeAfterLabel(after string) string {
	after = strings.TrimSpace(after)
	for _, prefix := range []string{"文档的", "文件里的", "表格中的", "这一栏的", "那一格的"} {
		after = strings.TrimPrefix(after, prefix)
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
	raw, _ := json.Marshal(map[string]any{
		"text": text, "after": after, "window": window,
	})
	return raw
}

// officeGenInternalHint is the model-only reminder for writing Office
// files onto the Desktop. It must never appear in assistant deltas,
// 无法执行 banners, or mid-markdown fences.
const officeGenInternalHint = "写到桌面请用对应 *.gen 工具并设 desktop=true，不要用 command.run。"

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

func looksLikeCurrentLookupTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || lookupOptedOut(t) || referenceOnlyOfficeTurn(t) {
		return false
	}
	lower := strings.ToLower(t)
	for _, needle := range []string{
		"天气", "气温", "温度", "火车", "高铁", "动车", "车次", "火车票", "航班", "机票",
		"股价", "汇率", "新闻", "热搜", "票价", "时刻表", "几点发车", "几点到",
		"行情", "沪深", "上证", "深证", "创业板", "股票", "大盘", "涨跌", "金价", "油价",
		"weather", "train", "flight", "stock price", "exchange rate", "latest news",
	} {
		if strings.Contains(t, needle) || strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func fallbackWebSearchArgs(goal string) json.RawMessage {
	goal = strings.TrimSpace(goal)
	if goal == "" || len(goal) > 512 || lookupOptedOut(goal) || referenceOnlyOfficeTurn(goal) {
		return nil
	}
	raw, _ := json.Marshal(map[string]any{"query": goal, "max": 5})
	return raw
}

func mediaGenerationKind(text string) string {
	for _, clause := range strings.FieldsFunc(strings.ToLower(chatRoutingText(text)), func(r rune) bool { return strings.ContainsRune("，,。；;\n", r) }) {
		blocked := false
		for _, blocker := range []string{"不要", "不用", "无需", "不需要", "先不", "别", "不能", "不支持", "配置", "供应商", "测试连接", "怎么", "如何", "do not", "don't", "without", "how to", "configure"} {
			blocked = blocked || strings.Contains(clause, blocker)
		}
		if blocked {
			continue
		}
		for _, pair := range []struct{ noun, tool string }{{"视频", "video.generate"}, {"图片", "image.generate"}} {
			if strings.Contains(clause, pair.noun) && containsAnyFold(clause, clause, []string{"生成", "做个", "制作", "画"}) {
				return pair.tool
			}
		}
		if match := englishMediaGenerationRE.FindStringSubmatch(clause); len(match) > 1 {
			if match[1] == "video" {
				return "video.generate"
			}
			return "image.generate"
		}
		if strings.Contains(clause, "生视频") && !strings.Contains(clause, "模型") {
			return "video.generate"
		}
		if (strings.Contains(clause, "生图") && !strings.Contains(clause, "模型")) || strings.Contains(clause, "画一张") || strings.Contains(clause, "画图") {
			return "image.generate"
		}
	}
	return ""
}

var englishMediaGenerationRE = regexp.MustCompile(`\b(?:generate|create|make|draw)\s+(?:(?:a|an|one|short)\s+){0,2}(video|image|picture)\b`)

func fallbackMediaGenerationArgs(goal string) json.RawMessage {
	goal = strings.TrimSpace(goal)
	if goal == "" || utf8.RuneCountInString(goal) > 4000 {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"prompt": goal})
	return raw
}

func desktopOpenTargetFromGoal(goal string) (string, bool) {
	// Only an imperative clause may trigger a host-side action. Descriptions
	// such as "a document that can be opened" and negations are not commands.
	for _, clause := range strings.FieldsFunc(goal, func(r rune) bool {
		return strings.ContainsRune("，,。；;\n", r)
	}) {
		if target, ok := desktopOpenTargetFromClause(clause); ok {
			return target, true
		}
	}
	return "", false
}

func desktopOpenTargetFromClause(clause string) (string, bool) {
	t := strings.TrimSpace(clause)
	if t == "" {
		return "", false
	}
	t = strings.ReplaceAll(t, "把开了", "打开")
	t = strings.ReplaceAll(t, "把开", "打开")
	for _, prefix := range []string{"然后", "接着", "请你帮我", "麻烦你帮我", "帮我", "请", "先", "再"} {
		t = strings.TrimPrefix(strings.TrimSpace(t), prefix)
	}
	matched := false
	for _, verb := range []string{"打开", "启动", "运行"} {
		if strings.HasPrefix(t, verb) {
			t = strings.TrimSpace(strings.TrimPrefix(t, verb))
			matched = true
			break
		}
	}
	if !matched {
		return "", false
	}
	for _, prefix := range []string{"桌面上的", "桌面的", "桌面上", "桌面"} {
		t = strings.TrimPrefix(strings.TrimSpace(t), prefix)
	}
	for _, marker := range []string{"的最后", "最后一", "末尾", "后面", "之后", "然后", "接着", "并播放", "并且", "并在", "再在", "输入", "填写", "填入", "写入", "点击"} {
		if i := strings.Index(t, marker); i > 0 {
			t = t[:i]
		}
	}
	t = strings.Trim(strings.TrimSpace(t), "，,。！？?!的")
	if t == "" || t == "文件" || t == "文档" {
		return "", false
	}
	return t, true
}

func fallbackDesktopOpenArgs(goal string) json.RawMessage {
	target, ok := desktopOpenTargetFromGoal(goal)
	if !ok {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"name": target})
	return raw
}

func looksLikeDesktopObserveTurn(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if wantsOfficeGen(t) {
		if _, explicitOpen := desktopOpenTargetFromGoal(t); !explicitOpen {
			return false
		}
	}
	contextual := strings.Contains(t, "桌面") || strings.Contains(t, "文档") || strings.Contains(t, "文件") ||
		strings.Contains(t, "表格") || strings.Contains(t, "窗口") || strings.Contains(t, "屏幕")
	action := strings.Contains(t, "输入") || strings.Contains(t, "填写") || strings.Contains(t, "填入") ||
		strings.Contains(t, "写入") || strings.Contains(t, "写上") || strings.Contains(t, "点击") ||
		strings.Contains(t, "按下") || strings.Contains(t, "保存") || strings.Contains(t, "发送")
	return contextual && action
}

func toolDefinitionsHave(defs []llmadapter.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func usedAnyTool(names []string, wanted ...string) bool {
	for _, name := range names {
		for _, candidate := range wanted {
			if name == candidate {
				return true
			}
		}
	}
	return false
}

func looksLikeComputerControlTurn(text string) bool {
	return looksLikePlayOrOpenTurn(text) || looksLikeTypeAfterLabelTurn(text) || looksLikeWeatherTurn(text)
}

func looksLikeExcelTask(text string) bool {
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	for _, k := range []string{"excel", "xlsx", "表格", "计算表", "模拟表", "财报", "工作簿"} {
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
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	return strings.Contains(t, "html") || strings.Contains(t, "小游戏") || strings.Contains(t, "点球") ||
		strings.Contains(t, "checklist") || strings.Contains(t, "待办页") || strings.Contains(t, "计时器") ||
		(strings.Contains(t, "清单") && !strings.Contains(t, "硬件"))
}

func wantsOfficeGen(text string) bool {
	if spokenResultReportOnly(text) {
		return false
	}
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(text) || looksLikeResume(text) {
		return false
	}
	if looksLikePptTask(text) || looksLikeReportTask(text) || looksLikeNovelTask(text) ||
		looksLikeExcelTask(text) || looksLikeHtmlGenTask(text) {
		return true
	}
	if explicitOfficeOutputTool(text) != "" {
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

var negatedOfficeDesktopRE = regexp.MustCompile(`(?:不要|不用|无需|不必|不需要|别|不|勿)(?:再|直接|自动)?(?:保存|存放|输出|生成|写入|复制|移动|放|存|写)?(?:在|到|至)?(?:我的|电脑)?桌面|(?:do not|don't|never|without)(?:\s+\w+){0,5}\s+desktop`)

func wantsOfficeFileOnDesktop(text string) bool {
	if !wantsOfficeGen(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	// A destination mentioned only to exclude it is not a write request.
	t = negatedOfficeDesktopRE.ReplaceAllString(t, "")
	return strings.Contains(t, "桌面") || strings.Contains(t, "desktop")
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
	// Structured tool data can legitimately quote these phrases from a source
	// document. Never replace an entire JSON result because its content matches
	// a prose-only status hint; callers depend on stable IDs, digests and cursors.
	if json.Valid([]byte(summary)) {
		return summary
	}
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
		case "desktop.open", "desktop.type", "desktop.quit", "desktop.browse", "media.play":
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

func looksLikeCompanionWaitPromise(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || strings.Contains(t, "无法执行") {
		return false
	}
	if strings.Contains(t, "已经打开") || strings.Contains(t, "已经写入") {
		return false
	}
	for _, n := range []string{"稍等", "等一下", "帮你查", "我去查", "我来做", "我来执行"} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return strings.Contains(t, "手头没有") && strings.Contains(t, "查")
}

func isCompanionLeadInOnly(text string) bool {
	t := strings.TrimSpace(text)
	t = strings.TrimRight(t, "。.!！ ")
	if t == "" {
		return true
	}
	for _, p := range []string{
		"等一下", "稍等", "稍等我一下", "嗯，我在呢，稍等我一下", "我在呢，稍等我一下",
		"好，我帮你查一下", "好，我来执行", "好，我来打开", "好，我来播放",
		"好，我来输入", "好，我马上处理", "好，我来操作电脑", "嗯，", "嗯",
	} {
		if t == strings.TrimRight(p, "。.!！ ") || t == p {
			return true
		}
	}
	return utf8.RuneCountInString(t) <= 6 && (strings.Contains(t, "等") || strings.Contains(t, "好"))
}
