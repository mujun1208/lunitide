package app

import (
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Desktop method ladder (voice and typed share one contract).
// Each rung runs once, forward only:
//  1. Dedicated tools / skills / MCP
//  2. computer.act observe, then one named click
//  3. GUI / SoM
// Success stops immediately and is reported. Failure is spoken only after
// the last available rung. Never go back to a finished rung.

const (
	ladderStepNone     = 0
	ladderStep1        = 1
	ladderStep2Observe = 2
	ladderStep2Click   = 3
	ladderStep3        = 4
)

func desktopLadderNudgeTextFor(next int) string {
	switch next {
	case ladderStep2Observe:
		return "第2步：computer.act observe 一次。不要再调用第1步工具，不要对用户报失败。"
	case ladderStep3:
		return "第3步：走屏幕读号一次。不要再 observe，不要再调用第1步工具，不要对用户报失败。"
	default:
		return "第2步：按控件名字或id点一次。不要再 observe，不要再调用第1步工具，不要对用户报失败。"
	}
}

func desktopLadderNudgeMessage(messages []llmadapter.Message, goal string) llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: desktopLadderNudgeTextFor(desktopLadderNext(messages, goal))}
}

func desktopLadderApplies(goal string) bool {
	goal = strings.TrimSpace(goal)
	if goal == "" || ownedMediaCenterGoal(goal) || lookupOnlyTurn(goal) || typedFieldOnlyGoal(goal) || companionGoalIsOpenOnly(goal) || quitOnlyGoal(goal) {
		return false
	}
	if officeDeliverableSkipsDesktopLadder(goal) || browserLookupOnlyGoal(goal) {
		return false
	}
	return computerExecutionTurn(goal) || companionWantsDesktopControl(goal)
}

// 写周报 / 打开 Word 写报告 is an office deliverable. Forcing the desktop
// 1-2-3 after desktop.open would steal the turn from docx.gen.
func officeDeliverableSkipsDesktopLadder(goal string) bool {
	if !laneLooksLikeOfficeDeliverable(goal) && !includeOfficeGenWorkflow(goal) {
		return false
	}
	if playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal) {
		return false
	}
	if looksLikeTypeAfterLabelTurn(goal) {
		return false
	}
	return !containsAnyFold(goal, strings.ToLower(goal), []string{
		"点击", "点一下", "点开", "帮我点", "点按钮", "截图", "播放", "填写", "身份证",
	})
}

func looksLikeObserveReceipt(out string) bool {
	s := strings.ToLower(strings.TrimSpace(out))
	if s == "" {
		return false
	}
	if strings.Contains(s, "frameid") || strings.Contains(s, "frame_id") {
		return true
	}
	if strings.Contains(s, `"count"`) || strings.Contains(s, `"nodes"`) {
		return true
	}
	return strings.Contains(s, "screenshot") || strings.Contains(s, "observe")
}

func desktopLadderDedicatedTool(name string) bool {
	switch name {
	case "media.play", "desktop.open", "desktop.type", "desktop.browse", "desktop.quit", "skill.invoke", "mcp.call":
		return true
	default:
		return false
	}
}

func desktopLadderBlocked(out string) bool {
	return capabilityDeniedOutput(out) || strings.Contains(out, "电脑控制未启用") || strings.Contains(out, "M10-CC-012") ||
		strings.Contains(out, "前台是月伴") || strings.Contains(out, "禁止像素动作") ||
		looksLikeUACToolResult(out) || looksLikeFilePickerToolResult(out)
}

type desktopLadderReceipt struct {
	ID     string
	Name   string
	Args   json.RawMessage
	Output string
}

func desktopLadderReceipts(messages []llmadapter.Message) []desktopLadderReceipt {
	results := map[string]string{}
	var receipts []desktopLadderReceipt
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
			if output, ok := results[call.ID]; ok {
				receipts = append(receipts, desktopLadderReceipt{call.ID, call.Name, call.Arguments, output})
			}
		}
	}
	return receipts
}

func (r desktopLadderReceipt) isGUI() bool {
	return strings.HasPrefix(r.ID, "gui-")
}

func (r desktopLadderReceipt) isObserve() bool {
	return computerActIsObserve(r.Name, r.Args) || liveDesktopObservation(r.Name, r.Args)
}

func (r desktopLadderReceipt) isNamedClick() bool {
	if r.isGUI() || r.isObserve() {
		return false
	}
	return r.Name == "computer.act" || strings.HasPrefix(r.Name, "cc.")
}

func observeShowsPlayback(out string) bool {
	if observeReturnedEmptyTree(out) {
		return false
	}
	return containsAnyFold(out, strings.ToLower(out), []string{"暂停", "正在播放", "now playing", "pause"})
}

func (r desktopLadderReceipt) succeeded(goal string) bool {
	if desktopLadderBlocked(r.Output) || companionToolResultFailed(r.Output) {
		return false
	}
	if r.Name == "media.play" {
		if mediaKeyDelivered(r.Output) {
			return true
		}
		return !unverifiedMediaPlay("media.play", r.Output, "")
	}
	if r.Name == "desktop.open" || r.Name == "desktop.browse" {
		return companionGoalIsOpenOnly(goal)
	}
	if desktopLadderDedicatedTool(r.Name) {
		if playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal) {
			return false
		}
		return true
	}
	if r.isObserve() && (playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal)) {
		return observeShowsPlayback(r.Output)
	}
	if r.isNamedClick() || r.isGUI() {
		if looksLikeObserveReceipt(r.Output) {
			return false
		}
		if proof, ok := extractL0(r.Output); ok && (!proof.Passed || proof.Uncertain) {
			return false
		}
		return true
	}
	return false
}

func desktopLadderSucceeded(messages []llmadapter.Message, goal string) bool {
	for _, receipt := range desktopLadderReceipts(messages) {
		if receipt.succeeded(goal) {
			return true
		}
	}
	return false
}

func desktopLadderNext(messages []llmadapter.Message, goal string) int {
	if !desktopLadderApplies(goal) {
		return ladderStepNone
	}
	if desktopLadderSucceeded(messages, goal) {
		return ladderStepNone
	}
	var dedicated, observed, namedClick, gui bool
	emptyTree := false
	namedFailed := false
	lastOut := lastToolOutput(messages)
	if desktopLadderBlocked(lastOut) {
		return ladderStepNone
	}
	for _, receipt := range desktopLadderReceipts(messages) {
		if desktopLadderDedicatedTool(receipt.Name) {
			dedicated = true
		}
		if receipt.isObserve() {
			observed = true
			if observeReturnedEmptyTree(receipt.Output) {
				emptyTree = true
			}
		}
		if receipt.isNamedClick() {
			namedClick = true
			if !receipt.succeeded(goal) {
				namedFailed = true
			}
		}
		if receipt.isGUI() {
			gui = true
		}
	}
	if !dedicated && desktopLadderWantsDedicated(goal) {
		return ladderStep1
	}
	if !observed {
		return ladderStep2Observe
	}
	if !emptyTree && !namedClick && !gui {
		return ladderStep2Click
	}
	if !gui && (emptyTree || namedFailed || namedClick) {
		return ladderStep3
	}
	return ladderStepNone
}

func desktopLadderSettled(messages []llmadapter.Message, goal string) bool {
	if !desktopLadderApplies(goal) {
		return true
	}
	return desktopLadderNext(messages, goal) == ladderStepNone
}

func desktopLadderQuiet(messages []llmadapter.Message, goal string) bool {
	return desktopLadderApplies(goal) && !desktopLadderSettled(messages, goal)
}

func desktopLadderLooksLikeFailureTalk(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	for _, needle := range []string{"无法", "未能", "失败", "没有完成", "播不了", "播放失败"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func desktopLadderDedicatedUnresolved(goal, toolOut string, lastTools []string) bool {
	if desktopLadderBlocked(toolOut) {
		return false
	}
	if usedAnyTool(lastTools, "computer.act") {
		return false
	}
	if playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal) {
		if !usedAnyTool(lastTools, "media.play") {
			return false
		}
		if mediaKeyDelivered(toolOut) {
			return false
		}
		return companionToolResultFailed(toolOut) || unverifiedMediaPlay(lastToolName(lastTools), toolOut, "")
	}
	if companionGoalIsOpenOnly(goal) {
		return false
	}
	if usedAnyTool(lastTools, "skill.invoke") || usedAnyTool(lastTools, "mcp.call") {
		return companionToolResultFailed(toolOut)
	}
	if !isDesktopControlTool(lastToolName(lastTools)) {
		return false
	}
	if lastToolName(lastTools) == "computer.act" || strings.HasPrefix(lastToolName(lastTools), "cc.") {
		return false
	}
	return companionToolResultFailed(toolOut)
}

func desktopLadderShouldContinue(goal, toolOut string, lastTools []string, nudges int) bool {
	if !desktopLadderApplies(goal) || nudges >= maxDesktopContinueNudges {
		return false
	}
	if desktopLadderBlocked(toolOut) {
		return false
	}
	if usedAnyTool(lastTools, "computer.act") {
		if observeReturnedEmptyTree(toolOut) {
			return false
		}
		if (playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal)) && observeShowsPlayback(toolOut) {
			return false
		}
		return looksLikeObserveReceipt(toolOut)
	}
	return desktopLadderDedicatedUnresolved(goal, toolOut, lastTools)
}

func desktopLadderNamedMutationAttempted(messages []llmadapter.Message) bool {
	for _, receipt := range desktopLadderReceipts(messages) {
		if receipt.isNamedClick() || receipt.isGUI() {
			return true
		}
	}
	return false
}

func desktopLadderWantsNamedObserve(goal string, messages []llmadapter.Message) bool {
	return desktopLadderNext(messages, goal) == ladderStep2Observe
}

func desktopLadderWantsGUIAfterObserve(goal string, messages []llmadapter.Message, emptyObserves int) bool {
	_ = emptyObserves
	return desktopLadderNext(messages, goal) == ladderStep3
}

func quitOnlyGoal(goal string) bool {
	if containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "打开", "启动", "写入", "输入", "播放", "搜索", "查询", "暂停", "窗口", "标签"}) {
		return false
	}
	if strings.Contains(goal, "退出") || strings.Contains(goal, "关闭软件") || strings.Contains(goal, "关掉软件") || strings.Contains(goal, "退出软件") {
		return true
	}
	if strings.Contains(goal, "关闭") || strings.Contains(goal, "关掉") {
		return quitTargetName(goal) != ""
	}
	return false
}

func quitTargetName(goal string) string {
	t := strings.TrimSpace(goal)
	for _, prefix := range []string{"请你帮我", "请帮我", "帮我", "麻烦", "请", "彻底", "完全", "马上"} {
		t = strings.TrimPrefix(strings.TrimSpace(t), prefix)
	}
	for _, verb := range []string{"关闭软件", "关掉软件", "退出软件", "关闭", "关掉", "退出"} {
		if strings.Contains(t, verb) {
			t = strings.Replace(t, verb, "", 1)
			break
		}
	}
	t = strings.Trim(t, " 。.吧了啊呀呢")
	switch t {
	case "", "软件", "程序", "应用", "窗口", "这个", "它", "他":
		return ""
	}
	if strings.Contains(t, "窗口") || strings.Contains(t, "标签") || strings.Contains(t, "文档") || strings.Contains(t, "网页") || strings.Contains(t, "浏览器") || strings.Contains(t, "浏览") || strings.Contains(t, "页面") {
		return ""
	}
	return t
}

func fallbackDesktopQuitArgs(goal string) json.RawMessage {
	if !quitOnlyGoal(goal) {
		return nil
	}
	name := quitTargetName(goal)
	if name == "" {
		return nil
	}
	out, err := json.Marshal(map[string]any{"name": name, "force": true})
	if err != nil {
		return nil
	}
	return out
}

func desktopLadderWantsDedicated(goal string) bool {
	if playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal) || looksLikeTypeAfterLabelTurn(goal) {
		return true
	}
	return containsAnyFold(goal, strings.ToLower(goal), []string{"打开", "启动", "open "})
}

func desktopLadderAllowsDedicated(goal string, messages []llmadapter.Message) bool {
	if !desktopLadderApplies(goal) {
		return true
	}
	return desktopLadderNext(messages, goal) == ladderStep1
}

func desktopLadderCallStep(call llmadapter.ToolCall) int {
	if desktopLadderDedicatedTool(call.Name) {
		return ladderStep1
	}
	if call.Name != "computer.act" && !strings.HasPrefix(call.Name, "cc.") {
		return ladderStepNone
	}
	if computerActIsObserve(call.Name, call.Arguments) || liveDesktopObservation(call.Name, call.Arguments) {
		return ladderStep2Observe
	}
	return ladderStep2Click
}

func desktopLadderKeepCalls(calls []llmadapter.ToolCall, messages []llmadapter.Message, goal string) []llmadapter.ToolCall {
	if !desktopLadderApplies(goal) || len(calls) == 0 {
		return calls
	}
	next := desktopLadderNext(messages, goal)
	kept := make([]llmadapter.ToolCall, 0, len(calls))
	for _, call := range calls {
		step := desktopLadderCallStep(call)
		if step == ladderStepNone || step == next {
			kept = append(kept, call)
			continue
		}
		if step < next {
			continue
		}
		if next == ladderStep2Observe && step == ladderStep2Click && !desktopLadderWantsDedicated(goal) {
			kept = append(kept, call)
		}
	}
	return kept
}

func desktopLadderSuccessSpeech(goal string) string {
	if playbackOnlyGoal(goal) || companionTurnWantsMusicPlay(goal) {
		return "已经在播了。"
	}
	return "已经做好了。"
}
