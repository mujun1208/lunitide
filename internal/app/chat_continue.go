package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Tool-loop budget for one chat.start: each Stream() that returns tool
// calls counts as one step. 6 was enough for a single lookup but stopped
// batch work (install many skills, multi-file edits) mid-task.
const (
	maxToolLoopSteps         = 24
	maxContinueNudges        = 3
	maxDesktopContinueNudges = 5
	continueNudgeText        = "继续执行用户的指令直到完成。不要停下来询问、不要等待确认、不要只做勘查后结束本轮。立刻继续调用工具。仅在任务已完成，或缺少无法推断的必要信息/权限时，才给出最终说明。"

	// maxToolLoopStepsHard caps how far one productive turn may extend its
	// step budget (E4). A long legitimate batch (install many skills,
	// multi-file edits, long build→fix loops) used to be silently truncated
	// at maxToolLoopSteps mid-task; now a turn that keeps making real tool
	// calls near the ceiling earns more room, up to this hard cap. A turn
	// that stalls, loops, or stops calling tools never reaches the extension
	// and stays at the base limit.
	maxToolLoopStepsHard = 72
	toolLoopExtendChunk  = 8
	maxToolBudgetWaves   = 3
)

// extendToolLoopLimit grows the per-turn step ceiling when the model is still
// productively calling tools within two steps of the current limit (E4). It
// never exceeds the hard cap and never shrinks an already-larger limit. This
// is the "adaptive step limit + checkpoint resume instead of hard truncation"
// contract: the turn checkpoint is saved every step, so an extension resumes
// exactly where the batch left off.
func extendToolLoopLimit(current, step int) int {
	if step < current-2 {
		return current
	}
	next := current + toolLoopExtendChunk
	if next > maxToolLoopStepsHard {
		next = maxToolLoopStepsHard
	}
	if next < current {
		return current
	}
	return next
}

// turnMayEarnMoreSteps reports whether a turn that is still productively
// calling tools is allowed to grow its step ceiling (E4) rather than being cut
// off mid-batch.
//
// A spoken turn earns the same room as a typed one once it is driving the
// desktop. The "keep a voice reply short" rule is about how much the assistant
// says, not how many steps the job takes, so capping a spoken turn lower than
// a typed one only meant the same task stopped halfway when asked out loud.
// Inventory lookups stay pinned: those turns are supposed to be two steps.
func turnMayEarnMoreSteps(companion, usedDesktopTools bool, goal string, lane LaneContract) bool {
	if inventoryLookupBlocksPublicWeb(goal) {
		return false
	}
	// A skill trial or other capability job is one task. A one-step lane
	// used to stop after the catalog lookup and report the budget as spent.
	if capabilityWorkTask(goal) {
		return !companion || usedDesktopTools
	}
	if companion && !usedDesktopTools {
		return false
	}
	return laneMayExtendToolLoop(lane)
}

// turnAdmitsUnfinishedToolBudget is the model stopping because it believes
// this round's tool allowance is gone while the task is still open.
func turnAdmitsUnfinishedToolBudget(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, done := range []string{"任务已完成", "已全部完成", "all done", "task complete"} {
		if strings.Contains(strings.ToLower(t), strings.ToLower(done)) {
			return false
		}
	}
	for _, marker := range []string{
		"工具额度", "额度耗尽", "步数已达上限", "步数达到上限", "调用步数", "步数限制", "工具耗尽", "只完成了",
		"没有实际执行", "再发一句",
		"未取得可靠", "没做完", "尚未完成", "还没做完", "待验证",
		"尚未落盘", "未落盘", "没有落盘", "无法落盘", "没能落盘",
		"尚未写入", "还没写入", "未能写入", "没有写入文件",
	} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

const toolBudgetContinueText = "上一轮工具步数用完了，任务还没完成。这是自动衔接的下一轮：继续调用工具把剩下的事做完。生成的文件写入当前对话文件夹，不要写到桌面，除非用户明确要求放到桌面。不要再说额度耗尽或尚未落盘后停下来。"

const maxLengthContinueWaves = 2

const lengthContinueText = "上一轮输出被长度截断，不完整的工具调用已经丢弃，文件还没落盘。这是自动衔接的下一轮：用一次完整的工具调用把剩余内容写入当前对话文件夹。文件大就拆成几次写入。不要重复长说明，不要停在截断处。"

func lengthContinueMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: lengthContinueText}
}

func toolBudgetContinueMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: toolBudgetContinueText}
}

// assistantPausedMidTask reports whether the model clearly stopped to ASK the
// user (confirm / shall I / waiting for) instead of finishing the job.
// Progress phrases such as 「接下来」「先看一下」「下一步」 must not extra-loop
// a one-shot computer task that is already executing.
func assistantPausedMidTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, m := range []string{
		"已全部安装", "全部安装完成", "安装完成", "已全部完成", "任务已完成",
		"以上就是", "all done", "that's all", "finished installing",
		"successfully installed", "task complete",
	} {
		if strings.Contains(t, strings.ToLower(m)) {
			return false
		}
	}
	for _, m := range []string{
		"请确认", "是否继续", "要不要我", "是否需要", "请问你",
		"shall i", "waiting for",
	} {
		if strings.Contains(t, m) {
			return true
		}
	}
	return false
}

func shouldContinueTurn(text string, usedTools bool, nudges int, disableReasoning bool) bool {
	// disableReasoning only affects model thinking output. Continuation is a
	// task-completion decision and stays independent (PRD C09 / FR-502).
	_ = disableReasoning
	if !usedTools || nudges >= maxContinueNudges {
		return false
	}
	return assistantPausedMidTask(text)
}

const incompleteContinueNudgeText = "上一步工具结果未闭环（画面过期、控件引用失效、材料已读但交付未生成、或播放未确认正在播放）。立刻根据最新结果继续调用本轮该用的工具，不要改去做看屏点按钮，不要停下来询问。"

func incompleteContinueNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: incompleteContinueNudgeText}
}

// forceSummaryNudgeText drives the end-of-turn forced summary (UX-05 #3):
// after a multi-tool / multi-round loop hits its step budget with tool calls
// still pending and no final text, one more model pass runs WITHOUT tools so
// the user always gets a spoken/readable wrap-up instead of a silent finish.
const forceSummaryNudgeText = "根据已经执行的工具结果，用一两句中文做最终总结。不能再调用任何工具。说出已经完成了什么、还有哪些没做完。不要提到步数、额度或上限，不要让用户再发一句。没做成的事直接说明原因。"

// shouldExtendPastPreparatoryStep keeps a turn alive after a catalog or file
// read. Stopping there is what made the model tell the user the step limit
// had been reached.
func shouldExtendPastPreparatoryStep(lastTools []string, waves, limit, step int) bool {
	if waves >= 3 || step+1 < limit || limit >= maxToolLoopStepsHard || len(lastTools) == 0 {
		return false
	}
	for _, name := range lastTools {
		switch name {
		case "skill.invoke", "skill.try", "skill.list", "skill.catalog.list", "skill.install",
			"workspace.read", "workspace.list", "workspace.search", "kb.search", "kb.cite", "office.inspect",
			"todo.write":
		default:
			return false
		}
	}
	return true
}

func mediaCenterPlayStillPending(goal string, lastTools []string) bool {
	return ownedMediaCenterGoal(goal) && !usedAnyTool(lastTools, "media.play")
}

func toolCallNames(calls []llmadapter.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	return names
}

func forceSummaryNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: forceSummaryNudgeText}
}

// budgetSummaryNudgeText closes a turn whose generation allowance ran out
// mid-job. The tools already ran, so the user is owed a report on the work,
// not a bare limit message.
const budgetSummaryNudgeText = "本轮生成预算已用完，不能再调用任何工具，也不要再输出思考过程。请只用自然语言，基于以上已经执行完的工具结果，简短地告诉用户：已经完成了什么、文件或结果在哪里、还剩哪些没做完。不要重复工具原始输出，不要说「稍等」。"

// budgetPartialTurnNotice marks the reply above as the report of a turn that
// stopped early. The turn ends normally because the work is real, but the
// notice keeps the partial state visible instead of passing it off as done.
const budgetPartialTurnNotice = "\n（本轮生成预算已用完，上面是已完成部分的小结。发送“继续”可以接着做完剩下的。）\n"

func budgetSummaryNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: budgetSummaryNudgeText}
}

func lastToolOutput(messages []llmadapter.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleTool {
			return messages[i].Content
		}
	}
	return ""
}

func lastNamedToolOutput(messages []llmadapter.Message, name string) string {
	results := map[string]string{}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == llmadapter.RoleUser {
			break
		}
		if m.Role == llmadapter.RoleTool {
			results[m.ToolCallID] = m.Content
		}
		for j := len(m.ToolCalls) - 1; j >= 0; j-- {
			call := m.ToolCalls[j]
			if call.Name == name {
				return results[call.ID]
			}
		}
	}
	return ""
}

func skillAuthoringSettled(lastTools []string, toolOut string) bool {
	switch lastToolName(lastTools) {
	case "skill.manage", "skill.create":
	default:
		return false
	}
	if companionToolResultFailed(toolOut) {
		return false
	}
	return !strings.Contains(strings.ToLower(toolOut), "ok:false")
}

func lastToolName(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	return tools[len(tools)-1]
}

func officeInspectedWithoutDeliverable(lastTools []string, userGoal string) bool {
	if !usedAnyTool(lastTools, "office.inspect") {
		return false
	}
	if usedAnyTool(lastTools, "office.generate", "pptx.gen", "docx.gen", "excel.gen", "pdf.gen") {
		return false
	}
	return officeExplicitCreationRE.MatchString(userGoal) || explicitOfficeOutputTool(userGoal) != "" ||
		looksLikeReportTask(userGoal) || wantsOfficeGen(userGoal)
}

// announcedWorkStillPending is the model describing the writes it is about
// to do after only reading. That sentence used to end the turn, so the user
// had to type 继续 again and got the same sentence back.
func announcedWorkStillPending(text string, lastTools []string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	// A real question waits. 「是否继续」 is a handoff, not a question, and
	// still falls through so the same task keeps going.
	if (strings.Contains(t, "请问") || strings.Contains(t, "你希望")) && !strings.Contains(t, "是否继续") {
		return false
	}
	for _, done := range []string{"已经写入", "已写入", "已经写好", "写好了", "已完成", "已经完成", "任务已完成"} {
		if strings.Contains(t, done) {
			return false
		}
	}
	pending := false
	for _, needle := range []string{"写入", "落笔", "并行执行", "并行两", "重新拉起", "先更新"} {
		if strings.Contains(t, needle) {
			pending = true
			break
		}
	}
	if !pending {
		return false
	}
	if len(lastTools) == 0 {
		return true
	}
	for _, name := range lastTools {
		switch name {
		case "workspace.read", "workspace.list", "workspace.search", "skill.list", "skill.catalog.list",
			"office.inspect", "kb.search", "kb.cite", "memory.search", "memory.get", "web.search", "web.fetch",
			"todo.write":
		default:
			return false
		}
	}
	return true
}

func shouldContinueIncompleteWork(text, lastToolOut string, lastTools []string, usedTools bool, nudges int) bool {
	if !usedTools || nudges >= maxContinueNudges {
		return false
	}
	if lastToolName(lastTools) == "media.play" && companionToolResultFailed(lastToolOut) {
		// Failed play is a method switch, not another media.play retry.
		return false
	}
	if mediaKeyDelivered(lastToolOut) {
		return false
	}
	blob := strings.ToUpper(text + "\n" + lastToolOut)
	if strings.Contains(blob, "STALE_FRAME") || strings.Contains(blob, "COMPUTER_STALE_FRAME") {
		return true
	}
	lower := strings.ToLower(text + "\n" + lastToolOut)
	if looksLikeStaleBrowserRef(lower) {
		return true
	}
	if unverifiedMediaPlay(lastToolName(lastTools), lastToolOut, text) {
		return nudges < 1
	}
	return false
}

func looksLikeStaleBrowserRef(lower string) bool {
	if !strings.Contains(lower, "stale") && !strings.Contains(lower, "失效") {
		return false
	}
	return strings.Contains(lower, "ref") || strings.Contains(lower, "snapshot") || strings.Contains(lower, "控件")
}

func unverifiedMediaPlay(lastTool, out, assistant string) bool {
	if lastTool != "media.play" {
		return false
	}
	if mediaControlReceiptSpeech(out) != "" {
		return false
	}
	if l0, ok := extractL0(out); ok && (!l0.Passed || l0.Uncertain) {
		return true
	}
	combined := strings.ToLower(out)
	if strings.Contains(combined, "verified playing") || strings.Contains(combined, "verified already playing") {
		return false
	}
	if strings.Contains(combined, "started playing") {
		if l0, ok := extractL0(out); ok && l0.Passed && !l0.Uncertain {
			return false
		}
	}
	return true
}

func companionStuckLeadInSpeech(goal, spoken string) string {
	blob := goal + "\n" + spoken
	if companionTurnWantsMusicPlay(blob) || companionRetryActionTurn(spoken) || companionPlayFollowUp(spoken) || companionPlayFollowUp(goal) {
		return "尚未开始播放。请确认要使用的音乐播放器。"
	}
	if companionWantsDesktopControl(blob) || strings.Contains(blob, "点开") || strings.Contains(blob, "第一条") {
		return "这一轮没有真正操作电脑。请再说一次要点哪里。"
	}
	return "无法执行：这一轮没有完成查询。"
}

func mediaTurnResultSpeech(messages []llmadapter.Message) string {
	out := lastNamedToolOutput(messages, "media.play")
	if out == "" {
		return "尚未开始播放。请确认要使用的音乐播放器。"
	}
	if companionToolResultFailed(out) {
		return companionToolResultSpeech("media.play", out)
	}
	return companionToolResultSpeech("media.play", out)
}

// mediaKeyDelivered is a transport command that already reached the player.
// The turn stops there. A later screen click would toggle or skip again.
func mediaKeyDelivered(out string) bool {
	if mediaControlReceiptSpeech(out) != "" {
		return true
	}
	if !strings.Contains(out, "MEDIA_UNVERIFIED") {
		return false
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	// The key was sent. That stops a second click, which would toggle pause.
	// It is not evidence that audio started.
	return strings.HasPrefix(line, "started playing in ") || strings.Contains(line, "playback not confirmed")
}

func mediaKeyDeliveredSpeech(out string) string {
	if speech := mediaControlReceiptSpeech(out); speech != "" {
		return speech
	}
	if strings.Contains(out, "MEDIA_UNVERIFIED") || strings.Contains(out, "playback not confirmed") {
		return "已发送播放操作，但还没有确认音乐开始播放。"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	const prefix = "started playing in "
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	app := strings.TrimPrefix(line, prefix)
	if i := strings.LastIndex(app, " ("); i > 0 {
		app = app[:i]
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return "已经发送播放了。"
	}
	return "已经让" + app + "播放了。"
}

// A transport acknowledgement proves the command was sent, not the player's
// resulting state. In particular, next/pause are not failed attempts to play.
func mediaControlReceiptSpeech(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if proof, ok := extractL0(out); ok && proof.Kind == "media-session" && proof.Passed && !proof.Uncertain {
		for prefix, speech := range map[string]string{
			"verified next in ":  "已切换到下一首并开始播放。",
			"verified prev in ":  "已切换到上一首并开始播放。",
			"verified pause in ": "已暂停播放。",
			"verified stop in ":  "已停止播放。",
		} {
			if strings.HasPrefix(line, prefix) {
				return speech
			}
		}
	}
	switch strings.TrimSpace(line) {
	case "sent next track":
		return "已发送切换下一首的操作。"
	case "sent previous track":
		return "已发送切换上一首的操作。"
	case "sent pause/play toggle":
		return "已发送暂停或继续播放的操作。"
	case "sent stop":
		return "已发送停止播放的操作。"
	}
	if speech := mediaSessionTransportSpeech(line); speech != "" {
		return speech
	}
	return ""
}

// An unconfirmed media-session receipt still means the key reached the player.
// Pausing and then clicking the screen toggles playback again and leaks frame JSON.
func mediaSessionTransportSpeech(line string) string {
	if !strings.Contains(line, "media session action=") {
		return ""
	}
	switch {
	case strings.Contains(line, "action=pause"):
		return "已暂停播放。"
	case strings.Contains(line, "action=next"):
		return "已切换到下一首。"
	case strings.Contains(line, "action=prev"):
		return "已切换到上一首。"
	case strings.Contains(line, "action=stop"):
		return "已停止播放。"
	case strings.Contains(line, "action=toggle"):
		return "已切换播放或暂停。"
	default:
		return "播放操作已经送到播放器。"
	}
}

func continueNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: continueNudgeText}
}

const desktopContinueNudgeText = "桌面操作还没做完。根据最新截图的 frameId 继续 see→act→verify，不要停下来闲聊。画面没变也要用当前帧，不要用点之前的图。做完用一句结果收尾；遇到打开/保存对话框就请用户去点。"

func desktopContinueNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: desktopContinueNudgeText}
}

func isDesktopControlTool(name string) bool {
	return strings.HasPrefix(name, "cc.") || name == "computer.act" || name == "desktop.type" || name == "desktop.open" || name == "desktop.browse" || name == "desktop.quit" || name == "media.play" || name == "browser.act"
}

func shouldContinueDesktopTurn(text string, nudges int) bool {
	return shouldContinueDesktopTurnGoal(text, "", nudges)
}

func shouldContinueDesktopTurnGoal(text, userGoal string, nudges int) bool {
	if nudges >= maxDesktopContinueNudges {
		return false
	}
	return !desktopTurnSettled(text, userGoal)
}

func companionGoalIsOpenOnly(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || !companionWantsDesktopControl(t) {
		return false
	}
	open := strings.Contains(t, "打开") || strings.Contains(t, "启动") || strings.Contains(t, "把开")
	if !open {
		return false
	}
	if websiteFirstResultGoal(t) {
		return false
	}
	// "打开网页里的第一个链接" / "点开第一个结果" is NOT "open only" — it
	// requires clicking inside an already-open page.
	if strings.Contains(t, "链接") || strings.Contains(t, "结果") {
		if strings.Contains(t, "第一") || strings.Contains(t, "第二") || strings.Contains(t, "第三") {
			return false
		}
	}
	for _, follow := range []string{
		"填写", "填一下", "填入", "填上", "填",
		"输入",
		"写入", "写上", "写一", "写进", "写好", "帮我写", "然后写", "并写",
		"播放", "播一",
		"点击", "点开", "点一下", "点保存", "点确定", "点登", "登录",
		"搜索", "搜一", "查",
		"发消息", "发送", "提交", "保存", "生成", "制作", "编辑",
		"下载", "上传", "关闭", "退出", "朗读",
		"然后", "之后", "接着",
	} {
		if strings.Contains(t, follow) {
			return false
		}
	}
	return true
}

func companionGoalIsOpenPage(goal string) bool {
	if !companionGoalIsOpenOnly(goal) {
		return false
	}
	t := strings.TrimSpace(goal)
	return strings.Contains(t, "网页") || strings.Contains(t, "浏览器") || strings.Contains(t, "网站")
}

func companionDesktopFilenameFragment(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if strings.Contains(t, "打开") || strings.Contains(t, "启动") || strings.Contains(t, "把开") || strings.Contains(t, "运行") {
		return false
	}
	if strings.HasSuffix(t, "增补文档") || strings.HasSuffix(t, "手写文档") || strings.HasSuffix(t, "协议文档") {
		return true
	}
	return strings.Contains(t, "桌面上的") && (strings.HasSuffix(t, "文档") || strings.HasSuffix(t, "文件"))
}

func desktopOpenSucceeded(toolOut string, lastTools []string) bool {
	if !usedAnyTool(lastTools, "desktop.open") {
		return false
	}
	out := strings.TrimSpace(toolOut)
	if strings.Contains(out, "opened ") {
		// Both fully-confirmed and soft-success ("unverified") count.
		return !strings.Contains(out, "无法执行")
	}
	// Open ran earlier this turn; a later tool's output is not the receipt.
	return lastToolName(lastTools) != "desktop.open"
}

// pickTurnContinueKind decides why the tool loop should take another
// model step. Desktop mid-task beats companion lead-in: a screenshot plus
// 「好，我来操作电脑」 is not done (that used to ask the model to narrate
// “完成了” after only looking).
func pickTurnContinueKind(stepText, assistantAll, toolOut string, lastTools []string, usedTools, usedDesktop, companion, disableReasoning bool, nudges int, userGoal string, toolsAttached bool) string {
	computerTask := companion || computerExecutionTurn(userGoal)
	if strings.TrimSpace(stepText) == "" {
		stepText = assistantAll
	}
	if capabilityDeniedOutput(toolOut) {
		return ""
	}
	if usedTools && skillAuthoringSettled(lastTools, toolOut) {
		return ""
	}
	if companion && companionNeedsSpokenInput(stepText) {
		return ""
	}
	if shouldContinueTurn(stepText, usedTools, nudges, disableReasoning) {
		return "ask"
	}
	if computerTask && companionGoalIsOpenOnly(userGoal) && desktopOpenSucceeded(toolOut, lastTools) && !strings.Contains(stepText+assistantAll+toolOut, "无法执行") {
		return ""
	}
	if computerTask && desktopLadderShouldContinue(userGoal, toolOut, lastTools, nudges) {
		return "ladder"
	}
	if shouldContinueIncompleteWork(stepText, toolOut, lastTools, usedTools, nudges) {
		return "incomplete"
	}
	if usedTools && officeInspectedWithoutDeliverable(lastTools, userGoal) && nudges < maxContinueNudges {
		return "incomplete"
	}
	if computerTask && playbackOnlyGoal(userGoal) && usedAnyTool(lastTools, "media.play") {
		return ""
	}
	if computerTask && companionCloseResultSettled(stepText, userGoal, toolOut) {
		return ""
	}
	if computerTask && usedTools && !looksLikeCompanionWaitPromise(stepText) && !isCompanionLeadInOnly(stepText) &&
		(companionToolResultFailed(toolOut) || strings.Contains(stepText, "未能") || strings.Contains(stepText, "无法") || strings.Contains(stepText, "没有查到")) {
		return ""
	}
	if computerTask && usedDesktop && shouldContinueDesktopTurnGoal(stepText, userGoal, nudges) {
		return "desktop"
	}
	if companion && usedTools && isCompanionLeadInOnly(stepText) && nudges < maxContinueNudges {
		return "leadin"
	}
	if toolsAttached && !usedTools && nudges < maxContinueNudges &&
		(looksLikeCompanionWaitPromise(stepText) || looksLikeUnexecutedActPromise(stepText) || (companion && isCompanionLeadInOnly(stepText))) {
		return "wait"
	}
	if toolsAttached && nudges < maxContinueNudges && announcedWorkStillPending(stepText, lastTools) {
		return "act"
	}
	// A buffered final reply is not in assistantAll yet. The current step was
	// checked above; an earlier spoken lead-in must not restart completed work.
	return ""
}

func companionCloseResultSettled(text, goal, toolOut string) bool {
	// Closing an app is a terminal action only when that is the actual goal
	// and its tool reported success. A caption alone cannot prove success.
	g := strings.ToLower(strings.TrimSpace(goal))
	if !strings.Contains(g, "关闭") && !strings.Contains(g, "关掉") && !strings.HasPrefix(g, "close ") {
		return false
	}
	for _, continuation := range []string{"然后", "再打开", "并", "之后", "播放", "写入", "发送", "所有", "全部", " and "} {
		if strings.Contains(g, continuation) {
			return false
		}
	}
	if companionToolResultFailed(toolOut) {
		return false
	}
	out := strings.ToLower(toolOut)
	if strings.Contains(out, "unverified") || strings.Contains(out, "screen unchanged") {
		return false
	}
	verified := strings.Contains(out, "closed") || strings.Contains(out, "已关闭") || strings.Contains(out, "关闭成功") ||
		((strings.HasPrefix(out, "window close ") || strings.HasPrefix(out, "quit ")) && strings.Contains(out, "screen updated"))
	return verified && (strings.Contains(text, "关闭") || strings.Contains(text, "关掉") || strings.Contains(strings.ToLower(text), "closed"))
}

func companionWantsDesktopControl(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if looksLikeTypeAfterLabelTurn(t) {
		return true
	}
	lower := strings.ToLower(t)
	for _, needle := range []string{
		"截图", "屏幕", "对话框", "点击", "鼠标", "电脑", "填写", "输入", "证件",
		"填表", "再点", "帮我点", "打字", "点一下", "点按钮", "记事本",
		"回车", "按一下", "按回车", "快捷键", "粘贴", "全选", "热键", "ctrl+",
		"点确定", "点保存", "点取消",
		"notepad",
		"打开", "点开", "点进", "第一条", "播放", "播一首", "播歌", "听歌", "放一首", "网易云", "汽水", "网页", "浏览器",
	} {
		if strings.Contains(t, needle) || strings.Contains(lower, needle) {
			return true
		}
	}
	if strings.Contains(t, "桌面") && (strings.Contains(t, "点") || strings.Contains(t, "填") || strings.Contains(t, "写") || strings.Contains(t, "操作")) &&
		!laneLooksLikeOfficeDeliverable(t) {
		return true
	}
	return wantsAgentHostAct(t)
}

func companionDesktopToolLoop(e *Engine, sessionID, goal string) bool {
	if companionWantsDesktopControl(goal) {
		return true
	}
	if e != nil && sessionID != "" {
		ctx := e.loadCompanionContext(sessionID)
		if ctx.DesktopActive {
			return true
		}
	}
	return false
}

func desktopTurnSettled(text string, userGoal string) bool {
	t := strings.TrimSpace(text)
	if t == "" || isCompanionLeadInOnly(t) {
		return false
	}
	lower := strings.ToLower(t)
	for _, m := range []string{
		"请你点", "请你在", "需要你点", "needs_user", "无法执行",
		"已经写", "已经填",
		"写上了", "填好了", "填完了",
		"我点不了", "保存对话框",
	} {
		if strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	if companionGoalIsOpenOnly(userGoal) && (strings.Contains(t, "已经打开") || strings.Contains(t, "打开了")) {
		return true
	}
	return false
}

func dropCompanionFailedTail(messages []llmadapter.Message) []llmadapter.Message {
	if len(messages) == 0 {
		return messages
	}
	idx := len(messages) - 1
	if messages[idx].Role == llmadapter.RoleUser && idx > 0 {
		idx--
	}
	for idx >= 0 && messages[idx].Role == llmadapter.RoleSystem {
		idx--
	}
	if idx < 0 || messages[idx].Role != llmadapter.RoleAssistant {
		return messages
	}
	body := strings.TrimSpace(messages[idx].Content)
	if body == "" || strings.Contains(body, "无法执行") {
		out := append([]llmadapter.Message(nil), messages[:idx]...)
		return append(out, messages[idx+1:]...)
	}
	return messages
}
