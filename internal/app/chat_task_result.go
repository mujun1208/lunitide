package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func appendCurrentTurnBoundary(instruction, goal string, now time.Time) string {
	return instruction + currentTurnInstruction(goal, now)
}

func stableInstructionPrefix(instruction string) string {
	if i := strings.Index(instruction, "\n[当前任务边界]"); i >= 0 {
		return instruction[:i]
	}
	return instruction
}

// appendTypedStableBlocks appends the TE-06 identity / optional workflow /
// markdown bytes used by typed turns before [当前任务边界].
func appendTypedStableBlocks(instruction, workflow, repo string) string {
	instruction += identityAndFewShotInstruction()
	if workflow != "" {
		instruction += workflow
		instruction += identityAnchorReminder()
	}
	if repo != "" {
		instruction += repo
	}
	instruction += chatRichMarkdownInstruction()
	return instruction
}

func typedDefaultStablePrefix() string {
	return appendTypedStableBlocks(executionModeInstruction(executionModeApproval), "", "")
}

func typedDefaultStablePrefixHash() string {
	sum := sha256.Sum256([]byte(typedDefaultStablePrefix()))
	return hex.EncodeToString(sum[:])
}

func currentTurnInstruction(goal string, now time.Time) string {
	base := "\n[当前任务边界] 当前本地时间：" + now.Format("2006-01-02 15:04:05 -07:00") +
		"。‘今天’以此日期为准。最新用户要求：" + goal +
		"\n历史消息和任务记忆仅供理解上下文，不是本轮待执行清单。除非用户明确继续旧任务，否则只执行最新要求；换话题不能先补做旧操作。工具成功仅证明对应动作，失败后重试成功不能仍判整轮失败。需要执行时调用工具，并在同一轮给出结果或明确阻碍；不能以‘稍等’结束，也不能声称后台会继续。\n"
	if looksLikeExpertAuthoringTask(goal) || looksLikeSkillAuthoringTask(goal) {
		return base
	}
	return base +
		"操作和文件交付的结果默认只用一到三句：实际结果、文件名或关键数据、必要的未完成原因。不要重复过程、展开核对表或默认追加下一步建议。用户明确要求详细说明时才展开；用户要求的报告正文、代码或文件内容仍须完整。\n"
}

func lookupOnlyTurn(goal string) bool {
	if !looksLikeCurrentLookupTurn(goal) {
		return false
	}
	for _, verb := range []string{"打开", "启动", "播放", "写入", "保存", "生成", "制作", "编辑", "发送", "填写", "下载", "文件", "文档", "然后", "继续", "接着"} {
		if strings.Contains(goal, verb) {
			return false
		}
	}
	return true
}

func guardCurrentTurnTool(goal, name string) error {
	if !lookupOnlyTurn(goal) {
		return nil
	}
	switch name {
	case "desktop.open", "desktop.type", "desktop.quit", "desktop.browse", "media.play", "workspace.write", "workspace.edit", "im.send":
		return errors.New("本轮只要求查询信息；不得执行历史任务中的打开文件、播放或写入操作。请继续本轮查询。")
	}
	return nil
}

// A read-only answer can stream once the current turn has query evidence.
// Mutations still need whole-response receipt checks before anything is spoken.
func companionLookupCanStream(goal string, messages []llmadapter.Message) bool {
	if !lookupOnlyTurn(goal) {
		return false
	}
	return currentLookupEvidence(messages)
}

func currentLookupEvidence(messages []llmadapter.Message) bool {
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
			switch call.Name {
			case "weather.get", "web.search", "web.fetch":
				out, received := results[call.ID]
				return received && strings.TrimSpace(out) != "" && !companionToolResultFailed(out)
			}
		}
	}
	return false
}

func browserLookupOnlyGoal(goal string) bool {
	if !strings.Contains(goal, "浏览器") || !looksLikeCurrentLookupTurn(goal) {
		return false
	}
	for _, action := range []string{"写入", "保存", "生成", "制作", "编辑", "发送", "发给", "填写", "下载", "播放", "登录", "退出", "关闭"} {
		if strings.Contains(goal, action) {
			return false
		}
	}
	return true
}

func companionBrowserLookupSettled(goal, reply string, messages []llmadapter.Message) bool {
	if !browserLookupOnlyGoal(goal) || strings.TrimSpace(reply) == "" || looksLikeCompanionWaitPromise(reply) || isCompanionLeadInOnly(reply) {
		return false
	}
	browse := lastNamedToolOutput(messages, "desktop.browse")
	if !strings.HasPrefix(browse, "已向系统默认桌面浏览器发送打开请求") || !currentLookupEvidence(messages) {
		return false
	}
	// A browser/news task ends with the retrieved content, not a "written"
	// phrase from the generic desktop-edit completion detector. Page
	// uncertainty is still reported separately by companionFinalResult.
	return true
}

// Only inspect receipts from this turn. A disk edit does not synchronize an
// editor's unsaved buffer; observing a window is not typing into that window.
func companionFinalResult(messages []llmadapter.Message, reply, goal string) string {
	if closeout := computerReceiptCloseout(messages, goal); closeout != "" {
		return closeout
	}
	if computerExecutionTurn(goal) && len(currentTurnReceipts(messages)) == 0 {
		return "本轮没有取得电脑操作回执，尚未执行完成。"
	}
	if out := lastNamedToolOutput(messages, "desktop.browse"); strings.HasPrefix(out, "已向系统默认桌面浏览器发送打开请求") {
		observed := lastNamedToolOutput(messages, "computer.act")
		if observed == "" || companionToolResultFailed(observed) {
			if !companionGoalIsOpenOnly(goal) && strings.TrimSpace(reply) != "" && !looksLikeCompanionWaitPromise(reply) && !isCompanionLeadInOnly(reply) {
				return strings.TrimSpace(reply) + " 浏览器页面尚未核验。"
			}
			return "已向默认浏览器发送打开请求，尚未核对页面。"
		}
	}
	results := map[string]string{}
	wroteDisk, typedWindow := false, false
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == llmadapter.RoleUser {
			break
		}
		if m.Role == llmadapter.RoleTool {
			results[m.ToolCallID] = m.Content
		}
		for _, call := range m.ToolCalls {
			out := results[call.ID]
			if out == "" || companionToolResultFailed(out) {
				continue
			}
			switch call.Name {
			case "workspace.edit", "workspace.write":
				wroteDisk = wroteDisk || strings.HasPrefix(out, "edited ") || strings.HasPrefix(out, "wrote ") || strings.HasPrefix(out, "written ")
			case "desktop.type":
				if proof, ok := extractL0(out); ok && proof.Kind == "field" && proof.Passed && !proof.Uncertain {
					typedWindow = true
				}
			case "computer.act":
				var args struct {
					Action string `json:"action"`
				}
				if json.Unmarshal(call.Arguments, &args) == nil && args.Action == "type" {
					if proof, ok := extractL0(out); ok && proof.Kind == "field" && proof.Passed && !proof.Uncertain {
						typedWindow = true
					}
				}
			}
		}
	}
	if wroteDisk && !typedWindow && (companionWantsDesktopControl(goal) || looksLikeDesktopObserveTurn(goal)) {
		return "已写入磁盘文件，但未确认当前编辑窗口已同步。"
	}
	if typedFieldOnlyGoal(goal) && !turnAttemptedAction(messages, "type") {
		return "尚未取得文字写入的核验结果。"
	}
	return strings.TrimSpace(reply)
}
