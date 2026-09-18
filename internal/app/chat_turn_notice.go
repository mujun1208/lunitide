package app

import (
	"strings"
	"unicode/utf8"
)

func createTurnClosingNotice(tools []string, assistantText string) string {
	empty := strings.TrimSpace(assistantText) == ""
	if empty {
		for _, name := range tools {
			if name == "skill.create" {
				return "技能已创建并写入技能中心。请到技能中心安装并发布。\n"
			}
			if name == "expert.create" {
				return "专家已创建。请到专家中心确认挂载技能（技能挂在专家身上），需要时再挂到项目步骤。\n"
			}
			if name == "plugin.create" {
				return "能力包已创建。请到能力包页查看安装状态。\n"
			}
		}
	}
	if complexTaskCanSaveAsSkill(tools) && !strings.Contains(assistantText, "技能草稿") {
		return "这次任务已经落成文件。若要复用做法，可以到技能中心保存为技能草稿。\n"
	}
	if !hasActingComputerTool(tools) {
		return ""
	}
	if assistantTextContainsDone(assistantText) {
		return ""
	}
	return ""
}

func complexTaskCanSaveAsSkill(tools []string) bool {
	office, acted := false, false
	for _, name := range tools {
		switch name {
		case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen", "workspace.write":
			office = true
		case "skill.invoke", "web.search", "todo.write":
			acted = true
		}
	}
	return office && acted
}

func createTurnFailureNotice(tools []string, assistantText string) string {
	if assistantTextContainsDone(assistantText) {
		return ""
	}
	trimmed := strings.TrimSpace(assistantText)
	if trimmed != "" && (strings.Contains(trimmed, "失败") || strings.Contains(trimmed, "没能") || strings.Contains(trimmed, "无法")) {
		return ""
	}
	openedDesktop := false
	triedMedia := false
	usedVision := false
	for _, name := range tools {
		switch name {
		case "desktop.open":
			openedDesktop = true
		case "media.play":
			triedMedia = true
		default:
			if strings.HasPrefix(name, "cc.") || name == "computer.act" {
				usedVision = true
			}
		}
	}
	if openedDesktop && !triedMedia && !usedVision {
		return ""
	}
	if openedDesktop && !triedMedia {
		return "软件已经打开了，但还没开始播放。你可以再说「随便放一首」或歌名让我继续。\n"
	}
	if triedMedia {
		return "这次没能开始播放。你可以再说「随便放一首」或具体歌名让我再试。\n"
	}
	for _, name := range tools {
		switch name {
		case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen":
			return "生成失败：文件没有写成功。\n"
		}
	}
	if usedVision {
		return "这次电脑操作未能完成。\n"
	}
	if hasActingComputerTool(tools) {
		return "这次操作没成功，请再说具体一点让我重试。\n"
	}
	return ""
}

func hasActingComputerTool(tools []string) bool {
	for _, name := range tools {
		switch name {
		case "workspace.write", "workspace.edit", "command.run", "web.fetch", "web.search", "browser.act", "browser.open",
			"docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "html.gen", "desktop.open", "desktop.type", "media.play", "im.send", "image.generate", "video.generate", "audio.generate", "data.process", "image.batch", "pdf.copy":
			return true
		}
		if strings.HasPrefix(name, "cc.") || name == "computer.act" {
			return true
		}
	}
	return false
}

// assistantTurnPersistText stores streamed reasoning only when persistThinking
// is true (failed turn, no model reply). Successful or partial replies keep
// the assistant body only so history does not dump the thinking chain.
func clipCancelledCompanionPersist(text string) string {
	return clipCancelledCompanionPersistToSpoken(text, "")
}

func clipCancelledCompanionPersistToSpoken(text, spoken string) string {
	if prefix := strings.TrimSpace(spoken); prefix != "" {
		full := strings.TrimSpace(text)
		if full == "" {
			return prefix
		}
		if strings.HasPrefix(full, prefix) || strings.HasPrefix(prefix, full) {
			if len([]rune(prefix)) <= len([]rune(full)) {
				return prefix
			}
			return full
		}
		return prefix
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	last := -1
	runes := []rune(text)
	for i, r := range runes {
		switch r {
		case '。', '？', '！', '.', '?', '!', '，', ',':
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.TrimSpace(string(runes[:last+1]))
}

func thinkingDuplicatesBody(thinking, body string) bool {
	t := strings.Join(strings.Fields(strings.TrimSpace(thinking)), " ")
	b := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if t == "" || b == "" {
		return false
	}
	if t == b {
		return true
	}
	if strings.Contains(t, b) && utf8.RuneCountInString(b) >= 8 {
		return true
	}
	return strings.Contains(b, t) && utf8.RuneCountInString(t) >= 8
}

func assistantTurnPersistText(assistant, thinking string, persistThinking bool) string {
	assistant = strings.TrimSpace(assistant)
	thinking = strings.TrimSpace(thinking)
	if thinkingDuplicatesBody(thinking, assistant) {
		thinking = ""
	}
	if !persistThinking || thinking == "" {
		return assistant
	}
	block := "【思考过程】\n" + thinking
	if assistant == "" {
		return block
	}
	return block + "\n\n" + assistant
}

// shouldPersistAssistantTurn keeps partial work when a turn ends in failure,
// but preserves the early-cancel contract (no durable append before
// finalization is claimed).
func shouldPersistAssistantTurn(err error, finalizationClaimed, cancelling bool) bool {
	if err == nil {
		return finalizationClaimed
	}
	if cancelling {
		return finalizationClaimed
	}
	return true
}

func assistantTextContainsDone(text string) bool {
	if strings.Contains(text, "已完成") || strings.Contains(text, "做完") {
		return true
	}
	if !strings.Contains(text, "完成") {
		return false
	}
	stripped := strings.ReplaceAll(text, "未完成", "")
	stripped = strings.ReplaceAll(stripped, "无法完成", "")
	return strings.Contains(stripped, "完成")
}

const (
	turnInterruptNotice   = "终止打断了"
	turnErrorNotice       = "无法执行。"
	duplicateToolResult   = "already done this turn"
	expertSectionMaxRunes = 2500
)

// UX-03 expert persona injection budget guard.
const (
	// expertInjectionCeilingRatio caps total expert persona injection at this
	// fraction of the model context window.
	expertInjectionCeilingRatio = 0.30
	// expertPersonaMinPerExpertTokens is the floor kept per expert (name +
	// core capability line) even under tight budgets.
	expertPersonaMinPerExpertTokens = 200
	// defaultExpertBudgetContextWindow is the fallback window when the model
	// does not advertise one.
	defaultExpertBudgetContextWindow = 128000
)

func skipExpertCouncil(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	htmlOnDesktop := (strings.Contains(strings.ToLower(t), "html") || strings.Contains(t, "网页") || strings.Contains(t, "小游戏")) &&
		(strings.Contains(t, "桌面") || strings.Contains(strings.ToLower(t), "desktop"))
	openWeb := (strings.Contains(t, "打开") || strings.Contains(t, "访问") || strings.Contains(strings.ToLower(t), "open")) &&
		(strings.Contains(t, "网站") || strings.Contains(t, "网页") || strings.Contains(t, "页面") || strings.Contains(strings.ToLower(t), "http"))
	playMusic := (strings.Contains(t, "播") || strings.Contains(strings.ToLower(t), "play")) &&
		(strings.Contains(t, "歌") || strings.Contains(t, "音乐") || strings.Contains(strings.ToLower(t), "music"))
	return wantsAgentHostAct(t) || htmlOnDesktop || openWeb || playMusic
}

// wantsAgentHostAct is the L4 gate. Office deliverables that merely land on
// the desktop (写周报保存到桌面) stay L2; deleting/renaming/mkdir still
// override even when the filename mentions 周报.
func wantsAgentHostAct(text string) bool {
	if !wantsLocalHostAct(text) {
		return false
	}
	if laneLooksLikeOfficeDeliverable(text) && !localHostFolderOrDeleteAct(text) {
		return false
	}
	return true
}

func localHostFolderOrDeleteAct(text string) bool {
	t := strings.TrimSpace(text)
	lower := strings.ToLower(t)
	return containsAnyFold(t, lower, []string{
		"文件夹", "目录", "folder", "directory", "mkdir",
		"删除", "删掉", "删了", "remove", "delete",
		"重命名", "改名", "rename",
		"清空",
	})
}

// wantsLocalHostAct is the L4 gate for local disk/process side effects.
// L1 would strip command.run and kill continue nudges, so mkdir/delete/rename
// must not fall through the default prose lane.
func wantsLocalHostAct(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	lower := strings.ToLower(t)
	host := localHostLocation(t, lower)
	dest := localHostDestination(t, lower)
	if laneLooksLikeProse(t) && !host && !dest {
		return false
	}
	if localFSMutate(t, lower) && (host || dest || localFSTarget(t, lower)) {
		return true
	}
	return dest && containsAnyFold(t, lower, []string{
		"下载", "解压", "复制", "拷贝", "移动", "download", "unzip", "copy", "move", "extract",
	})
}

func localHostLocation(t, lower string) bool {
	return containsAnyFold(t, lower, []string{
		"桌面", "desktop", "本机", "这台电脑", "电脑上",
		"回收站", "recycle bin", "recyclebin",
		"下载文件夹", "downloads", "我的文档",
		`:\\`, `:\`, "/users/", "/home/", "~/",
		"d盘", "c盘", "e盘", "d 盘", "c 盘",
	})
}

func localHostDestination(t, lower string) bool {
	return containsAnyFold(t, lower, []string{
		"到桌面", "至桌面", "to the desktop", "to my desktop", "onto the desktop",
		"on my desktop", "on the desktop",
		"到回收站",
	})
}

func localFSTarget(t, lower string) bool {
	if strings.Contains(t, "文件夹") || strings.Contains(t, "目录") || strings.Contains(t, "文件") {
		return true
	}
	return containsAnyFold(t, lower, []string{
		"folder", "directory", ".txt", ".md", ".zip",
		" text file", "a file", "the file", "this file",
	})
}

func localFSMutate(t, lower string) bool {
	return containsAnyFold(t, lower, []string{
		"创建", "新建", "建一个", "建个", "mkdir",
		"create", "make a folder", "make a directory", "make a file",
		"new folder", "new directory", "new file",
		"删除", "删掉", "删了", "remove", "delete",
		"重命名", "改名", "rename",
		"移动", "移到", "move the", "move this",
		"复制", "拷贝", "copy this", "copy the", "copy to",
		"解压", "unzip", "extract",
		"下载到", "download to", "保存到", "save to", "save on",
		"写到", "清空",
	})
}

func turnOutcomeNotice(cancelling bool, err error, goal string, tools []string) string {
	if cancelling {
		return turnInterruptNotice
	}
	if err != nil {
		return turnErrorNotice + turnFailureCause(err, goal, tools)
	}
	return ""
}

func turnFailureCause(err error, goal string, tools []string) string {
	se := chatStreamError(err)
	switch se.Code {
	case "UPSTREAM_TIMEOUT":
		return "请求超时，请稍后重试。"
	case "ASSISTANT_RESPONSE_TOO_LARGE", "REQUEST_TOO_LARGE":
		return "回复或工具参数过大，请减少内容后重试。"
	}
	if officeGenToolForGoal(goal) != "" || hasOfficeGenTool(tools) {
		return officeGenFailNotice(err)
	}
	if looksLikeComputerControlTurn(goal) || hasComputerControlTool(tools) {
		return "这次操作没成功，请再说具体一点让我重试。"
	}
	return "模型结果不完整，请重试。"
}

func thinkingParameterRejected(reason string) bool {
	lower := strings.ToLower(reason)
	if !strings.Contains(lower, "thinking") {
		return false
	}
	return strings.Contains(lower, "disabled") || strings.Contains(lower, "not supported") || strings.Contains(lower, "enable_thinking")
}

func imageUnsupportedReason(reason string) bool {
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "does not support image") || strings.Contains(lower, "image is not supported") {
		return true
	}
	if strings.Contains(lower, "vision") && strings.Contains(lower, "not support") {
		return true
	}
	return strings.Contains(lower, "url") && strings.Contains(lower, "base64") && strings.Contains(lower, "image")
}

func appendAssistantNotice(existing, notice string) (next, delta string) {
	notice = sanitizeUserVisibleNotice(notice)
	if notice == "" || strings.Contains(existing, notice) {
		return existing, ""
	}
	if strings.TrimSpace(existing) == "" {
		return notice, notice
	}
	delta = "\n" + notice
	return existing + delta, delta
}

func duplicateToolSkipSummary(digest string, completed map[string]string) (string, bool) {
	if digest == "" {
		return "", false
	}
	if _, ok := completed[digest]; ok {
		return duplicateToolResult, true
	}
	return "", false
}
