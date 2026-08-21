package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

// Tool-loop budget for one chat.start: each Stream() that returns tool
// calls counts as one step. 6 was enough for a single lookup but stopped
// batch work (install many skills, multi-file edits) mid-task.
const (
	maxToolLoopSteps  = 24
	maxContinueNudges = 3
	continueNudgeText = "继续执行用户的指令直到完成。不要停下来询问、不要等待确认、不要只做勘查后结束本轮。立刻继续调用工具。仅在任务已完成，或缺少无法推断的必要信息/权限时，才给出最终说明。"
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

func continueNudgeMessage() gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: continueNudgeText}
}
