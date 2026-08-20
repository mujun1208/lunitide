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

// assistantPausedMidTask reports whether the model stopped after reconnaissance
// and is waiting for the user to say "继续" instead of finishing the job.
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
		"接下来", "下一步", "安装前", "先弄清", "还没开始", "初步判断",
		"请确认", "是否继续", "稍等", "让我先", "需要先", "先看一下",
		"弄清结构", "安装方式", "要不要我", "是否需要", "请问你",
		"before installing", "next i will", "let me first", "shall i",
		"i'll start by", "to proceed", "waiting for", "haven't started",
		"preliminary",
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
