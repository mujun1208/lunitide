package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

// Tool-loop budget for one chat.start: each Stream() that returns tool
// calls counts as one step. 6 was enough for a single lookup but stopped
// batch work (install many skills, multi-file edits) mid-task.
const (
	maxToolLoopSteps         = 24
	maxContinueNudges        = 3
	maxDesktopContinueNudges = 5
	continueNudgeText        = "继续执行用户的指令直到完成。不要停下来询问、不要等待确认、不要只做勘查后结束本轮。立刻继续调用工具。仅在任务已完成，或缺少无法推断的必要信息/权限时，才给出最终说明。"
)

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
	if disableReasoning || !usedTools || nudges >= maxContinueNudges {
		return false
	}
	return assistantPausedMidTask(text)
}

const incompleteContinueNudgeText = "上一步工具结果未闭环（画面过期、控件引用失效、或播放未确认正在播放）。立刻根据最新结果继续调用工具，不要停下来询问。"

func incompleteContinueNudgeMessage() gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: incompleteContinueNudgeText}
}

func lastToolOutput(messages []gateway.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == gateway.RoleTool {
			return messages[i].Content
		}
	}
	return ""
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
	blob := strings.ToUpper(text + "\n" + lastToolOut)
	if strings.Contains(blob, "STALE_FRAME") || strings.Contains(blob, "COMPUTER_STALE_FRAME") {
		return true
	}
	lower := strings.ToLower(text + "\n" + lastToolOut)
	if looksLikeStaleBrowserRef(lower) {
		return true
	}
	return unverifiedMediaPlay(lastToolName(lastTools), lastToolOut, text)
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
	combined := strings.ToLower(out + "\n" + assistant)
	if strings.Contains(combined, "正在播") || strings.Contains(combined, "now playing") || strings.Contains(combined, "already playing") {
		return false
	}
	t := strings.ToLower(out)
	return strings.Contains(t, "started") || strings.Contains(t, "已启动") || strings.Contains(t, "opened")
}

func continueNudgeMessage() gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: continueNudgeText}
}

const desktopContinueNudgeText = "桌面操作还没做完。根据最新截图的 frameId 继续 see→act→verify，不要停下来闲聊。画面没变也要用当前帧，不要用点之前的图。做完用一句结果收尾；遇到打开/保存对话框就请用户去点。"

func desktopContinueNudgeMessage() gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: desktopContinueNudgeText}
}

func isDesktopControlTool(name string) bool {
	return strings.HasPrefix(name, "cc.") || name == "computer.act" || name == "desktop.type" || name == "desktop.open" || name == "media.play" || name == "browser.act"
}

func shouldContinueDesktopTurn(text string, nudges int) bool {
	if nudges >= maxDesktopContinueNudges {
		return false
	}
	return !desktopTurnSettled(text)
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
		"word", "notepad",
		"打开", "播放", "播一首", "播歌", "听歌", "放一首", "网易云", "汽水", "网页", "浏览器",
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

func desktopTurnSettled(text string) bool {
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
	return false
}
