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
	maxToolLoopStepsHard = 48
	toolLoopExtendChunk  = 8
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

const incompleteContinueNudgeText = "上一步工具结果未闭环（画面过期、控件引用失效、或播放未确认正在播放）。立刻根据最新结果继续调用工具，不要停下来询问。"

func incompleteContinueNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: incompleteContinueNudgeText}
}

// forceSummaryNudgeText drives the end-of-turn forced summary (UX-05 #3):
// after a multi-tool / multi-round loop hits its step budget with tool calls
// still pending and no final text, one more model pass runs WITHOUT tools so
// the user always gets a spoken/readable wrap-up instead of a silent finish.
const forceSummaryNudgeText = "本轮工具调用步数已达上限，工具已执行完毕，不能再调用任何工具。请只用自然语言，基于以上工具执行结果，给用户一段简洁的最终总结：说清楚已经完成了什么、得到的关键结果、以及还有哪些没做完或需要用户接下来做的事。不要再请求调用工具，不要只说「稍等」。"

func forceSummaryNudgeMessage() llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: forceSummaryNudgeText}
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

func shouldContinueIncompleteWork(text, lastToolOut string, lastTools []string, usedTools bool, nudges int) bool {
	if !usedTools || nudges >= maxContinueNudges {
		return false
	}
	if lastToolName(lastTools) == "media.play" && companionToolResultFailed(lastToolOut) {
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
	return ""
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
	for _, follow := range []string{"填写", "填一下", "填", "输入", "写入", "写", "播放", "播一", "点", "搜索", "搜一", "查", "发消息", "发送", "提交", "保存", "生成", "制作", "编辑", "下载", "上传", "关闭", "退出", "朗读", "然后", "之后", "接着"} {
		if strings.Contains(t, follow) {
			return false
		}
	}
	return true
}

func desktopOpenSucceeded(toolOut string, lastTools []string) bool {
	if lastToolName(lastTools) != "desktop.open" {
		return false
	}
	out := strings.TrimSpace(toolOut)
	return strings.HasPrefix(out, "opened ") && !strings.Contains(out, "无法执行")
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
	if shouldContinueIncompleteWork(stepText, toolOut, lastTools, usedTools, nudges) {
		return "incomplete"
	}
	if computerTask && playbackOnlyGoal(userGoal) && usedAnyTool(lastTools, "media.play") {
		return ""
	}
	if computerTask && companionGoalIsOpenOnly(userGoal) && desktopOpenSucceeded(toolOut, lastTools) && !strings.Contains(stepText+assistantAll+toolOut, "无法执行") {
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
		(looksLikeCompanionWaitPromise(stepText) || (companion && isCompanionLeadInOnly(stepText))) {
		return "wait"
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
		"word", "notepad",
		"打开", "点开", "点进", "第一条", "播放", "播一首", "播歌", "听歌", "放一首", "网易云", "汽水", "网页", "浏览器",
	} {
		if strings.Contains(t, needle) || strings.Contains(lower, needle) {
			return true
		}
	}
	if strings.Contains(t, "桌面") && (strings.Contains(t, "点") || strings.Contains(t, "填") || strings.Contains(t, "写") || strings.Contains(t, "操作")) {
		return true
	}
	return false
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
