package app

import (
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Input modality changes presentation and consent, not desktop execution rules.
func computerExecutionTurn(goal string) bool {
	if detectTaskRoute(goal) == RouteR2 {
		return true
	}
	if lookupOnlyTurn(goal) || !companionWantsDesktopControl(goal) {
		return false
	}
	return containsAnyFold(goal, strings.ToLower(goal), []string{
		"打开", "启动", "点击", "点一下", "输入", "填写", "打字", "写入",
		"关闭", "退出", "播放", "暂停", "下一首", "上一首", "截图",
		"click", "type ", "open ", "close ", "quit ",
	})
}

func desktopExecutionInstruction() string {
	return "\n[电脑执行约定：语音与文字共用]\n" +
		"先按目标选专用工具：桌面浏览器/搜索页用 desktop.browse，文件/应用用 desktop.open，播放/切歌用 media.play，命名字段输入用 desktop.type，彻底退出用 desktop.quit。网页内操作用 browser.act，通用桌面操作才用 computer.act；不为同一个目标换工具重复操作。\n" +
		"打开浏览器搜索并报告内容时，搜索入口只打开一次，后续检索用查询工具；不要每换一个检索词就再启动搜索页。用户明确要求打开具体结果页或多个页面时按要求执行。\n" +
		"专用工具已经成功就使用原回执；不再启动、切歌、打字或发送一次。失败先读取具体错误并重新观察，只在目标或参数得到纠正后重试；相同失败不得循环。截图或像素变化仅证明观察/画面变化，不证明已输入、已保存、已发送或已播放。\n" +
		"操作必须遵循当前会话权限，不绕过确认，不覆盖未保存内容。电脑控制需要最新 frameId；优先实际控件名称/ID，坐标只能来自当前截图。\n" +
		"最终按本轮实际证据简短报告已做的事和未完成部分。打开不等于播放，写磁盘不等于窗口更新，关闭窗口不等于退出进程。失败后成功应采用最新成功证据；只有观察或命令回执时不能说任务完成。草稿并不代表用户已经看到结果。\n"
}

type turnToolReceipt struct {
	Name   string
	Args   json.RawMessage
	Output string
}

func currentTurnReceipts(messages []llmadapter.Message) []turnToolReceipt {
	results := map[string]string{}
	var receipts []turnToolReceipt
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
				receipts = append(receipts, turnToolReceipt{call.Name, call.Arguments, output})
			}
		}
	}
	return receipts
}

// Automatic rescue must not re-run an action already attempted via another
// tool. A model can still deliberately correct a failed call within the budget.
func turnAttemptedAction(messages []llmadapter.Message, action string) bool {
	for _, receipt := range currentTurnReceipts(messages) {
		var args struct {
			Action string `json:"action"`
			Op     string `json:"op"`
		}
		_ = json.Unmarshal(receipt.Args, &args)
		switch action {
		case "lookup":
			if receipt.Name == "weather.get" || receipt.Name == "web.search" || receipt.Name == "web.fetch" {
				return true
			}
		case "open":
			if receipt.Name == "desktop.open" || receipt.Name == "desktop.browse" ||
				(receipt.Name == "browser.act" && args.Op == "navigate") {
				return true
			}
		case "type":
			if receipt.Name == "desktop.type" || receipt.Name == "cc.keyboard_type" || receipt.Name == "cc.paste" || receipt.Name == "cc.set_value" ||
				(receipt.Name == "computer.act" && (args.Action == "type" || args.Action == "paste" || args.Action == "set_value")) {
				return true
			}
		case "observe":
			if liveDesktopObservation(receipt.Name, receipt.Args) {
				return true
			}
		}
	}
	return false
}

func desktopMutation(name string, args json.RawMessage) bool {
	return isDesktopControlTool(name) && !liveDesktopObservation(name, args)
}

func desktopAttemptKey(name string, args json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil {
		return argsDigestOrFallback(name, args)
	}
	// Refreshing an observation is allowed, but a new frame alone does not
	// justify indefinitely repeating the same failed mutation.
	delete(fields, "frameId")
	delete(fields, "frame_id")
	canonical, _ := json.Marshal(fields)
	return argsDigestOrFallback(name, canonical)
}

// Refining a background search is not a request to keep launching search tabs.
// Explicit URLs, multi-page requests and unsuccessful launches remain eligible.
func reuseBrowserSearchEntry(goal string, args json.RawMessage, messages []llmadapter.Message) string {
	if !browserLookupOnlyGoal(goal) || !currentLookupEvidence(messages) || containsAnyFold(goal, strings.ToLower(goal), []string{"多个", "几个", "分别", "依次", "两个", "两条", "另一个", "新标签", "新窗口", "结果页"}) {
		return ""
	}
	var next struct {
		Query string `json:"query"`
		URL   string `json:"url"`
	}
	if json.Unmarshal(args, &next) != nil || strings.TrimSpace(next.Query) == "" || strings.TrimSpace(next.URL) != "" {
		return ""
	}
	for _, receipt := range currentTurnReceipts(messages) {
		if receipt.Name != "desktop.browse" {
			continue
		}
		if !strings.HasPrefix(receipt.Output, "已向系统默认桌面浏览器发送打开请求") || companionToolResultFailed(receipt.Output) {
			return ""
		}
		return receipt.Output + "\n本轮搜索入口已经打开，此次没有重复启动页面。更换检索词请使用 web.search；报告已经查询到的内容。"
	}
	return ""
}

func clipExecutionToolSummary(name, output string) string {
	if !isDesktopControlTool(name) {
		return clipToolSummary(output)
	}
	proof, ok := extractL0(output)
	clipped := clipToolSummary(output)
	if !ok {
		return clipped
	}
	if _, present := extractL0(clipped); present {
		return clipped
	}
	proof.Detail = truncateUTF8Bytes(proof.Detail, 256)
	raw, _ := json.Marshal(map[string]any{"l0": proof})
	return truncateUTF8Bytes(clipped, toolSummaryMaxBytes-len(raw)-1) + "\n" + string(raw)
}

func playbackOnlyGoal(goal string) bool {
	if !companionTurnWantsMusicPlay(goal) && !companionPlayFollowUp(goal) {
		return false
	}
	return !containsAnyFold(goal, strings.ToLower(goal), []string{"写入", "输入", "填写", "保存", "发送", "发给", "退出", "关闭", "新闻", "天气", "文件", "截图", "下载"})
}

func typedFieldOnlyGoal(goal string) bool {
	return containsAnyFold(goal, strings.ToLower(goal), []string{"输入", "填写", "打字", "写入"}) &&
		!containsAnyFold(goal, strings.ToLower(goal), []string{"发送", "发给", "提交", "保存", "然后", "之后", "退出", "关闭", "播放", "新闻", "天气"})
}

func computerReceiptCloseout(messages []llmadapter.Message, goal string) string {
	if playbackOnlyGoal(goal) && lastNamedToolOutput(messages, "media.play") != "" {
		out := lastNamedToolOutput(messages, "media.play")
		if proof, ok := extractL0(out); ok && proof.Passed && !proof.Uncertain && strings.Contains(goal, "随机") && strings.Contains(out, "shuffle=false") {
			return "已开始播放，但未确认随机模式。"
		}
		return mediaTurnResultSpeech(messages)
	}
	if typedFieldOnlyGoal(goal) {
		for _, receipt := range currentTurnReceipts(messages) {
			var args struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(receipt.Args, &args)
			if receipt.Name != "desktop.type" && receipt.Name != "cc.keyboard_type" && receipt.Name != "cc.paste" && receipt.Name != "cc.set_value" &&
				!(receipt.Name == "computer.act" && (args.Action == "type" || args.Action == "paste" || args.Action == "set_value")) {
				continue
			}
			proof, verified := extractL0(receipt.Output)
			if verified && proof.Passed && !proof.Uncertain && proof.Kind == "field" {
				return "已在目标输入框写入并核对。"
			}
			return "已尝试输入，但未确认目标输入框的内容。"
		}
	}
	if strings.Contains(goal, "退出") && !containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "打开", "启动", "写入", "输入", "播放", "搜索", "查询"}) {
		seen := map[string]bool{}
		var facts []string
		for _, receipt := range currentTurnReceipts(messages) {
			if receipt.Name != "desktop.quit" {
				continue
			}
			var args struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(receipt.Args, &args)
			if seen[args.Name] {
				continue
			}
			seen[args.Name] = true
			proof, ok := extractL0(receipt.Output)
			if ok && proof.Kind == "process" && proof.Passed && !proof.Uncertain {
				line, _, _ := strings.Cut(receipt.Output, "\n")
				facts = append(facts, strings.TrimSpace(line))
			} else {
				facts = append(facts, "未确认"+args.Name+"已彻底退出")
			}
		}
		if len(facts) > 0 {
			return strings.Join(facts, "；") + "。"
		}
	}
	if companionGoalIsOpenOnly(goal) {
		out := lastNamedToolOutput(messages, "desktop.open")
		if out != "" {
			proof, ok := extractL0(out)
			if ok && proof.Passed && !proof.Uncertain && proof.Kind == "foreground" && !companionToolResultFailed(out) {
				return "已打开目标文件或应用。"
			}
			if companionToolResultFailed(out) {
				return companionToolResultSpeech("desktop.open", out)
			}
			return "已发送打开操作，但未确认目标窗口。"
		}
	}
	return ""
}
