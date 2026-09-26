package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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
	instruction += typedAssistInstruction()
	return instruction
}

// typedAssistInstruction is typed chat only. Voice uses the companion
// persona, which asks one spoken question and never opens this card.
func typedAssistInstruction() string {
	return "\n[打字协助] 这是打字对话。复杂或多步、会改文件、或交付有多种做法时：先 todo.write 写出完整步骤（通常 3–7 步，同时只有一步 in_progress），做完一步就重写整份清单。一句话能做完的不要硬拆。只有用户必须拍板且选项会改变交付时才 user.ask：每题恰好一项 recommended=true（你会选的），detail 写一句结果；其余选项完整且互斥。用户提交前不要做受该选择影响的步骤。没有分叉就自己决定，并在正文里用一两句说明选择和原因。不要弹确认卡，不要问上下文已有答案的问题。"
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

func inventoryLookupGoal(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"火车", "高铁", "动车", "车次", "火车票", "车票", "余票",
		"航班", "机票", "飞机票",
		"train", "flight", "airfare",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func inventoryOpenPageAllowed(goal string) bool {
	t := strings.ToLower(strings.TrimSpace(goal))
	if t == "" {
		return false
	}
	for _, needle := range []string{
		"打开12306", "打开 12306", "上12306", "打开网页", "用浏览器",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func inventoryLookupBlocksPublicWeb(goal string) bool {
	return inventoryLookupGoal(goal)
}

func browserLaunchFailedOutput(out string) bool {
	return strings.Contains(out, "BROWSER_LAUNCH_FAILED") || strings.Contains(out, "initializeServer")
}

func ownedMediaCenterGoal(goal string) bool {
	return containsAnyFold(goal, strings.ToLower(goal), []string{"媒体中心", "自带媒体", "自带的媒体中心", "自带播放"})
}

// carryMediaCenterGoal keeps an in-app playback request when the next sentence
// only says to pick something and play it. The desktop screen checker cannot
// see the media center, so that follow-up must stay on media.play.
func carryMediaCenterGoal(messages []llmadapter.Message) string {
	goal := lastUserChatText(messages)
	if ownedMediaCenterGoal(goal) || !mediaCenterPlayFollowUp(goal) {
		return goal
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llmadapter.RoleUser {
			continue
		}
		text := chatRoutingText(messages[i].Content)
		if text == "" || text == goal {
			continue
		}
		if ownedMediaCenterGoal(text) {
			return text
		}
	}
	return goal
}

func mediaCenterPlayFollowUp(goal string) bool {
	if containsAnyFold(goal, strings.ToLower(goal), []string{"暂停", "下一", "上一", "停止", "关掉", "关闭"}) {
		return false
	}
	return containsAnyFold(goal, strings.ToLower(goal), []string{"播放", "放一下", "放个", "找个", "随便"})
}

func mediaCenterSkipsDesktopVerifier(goal string, messages []llmadapter.Message) bool {
	if ownedMediaCenterGoal(goal) {
		return true
	}
	return strings.Contains(lastNamedToolOutput(messages, "media.play"), "MEDIA_CENTER")
}

func closeCurrentBrowserGoal(goal string) bool {
	if !strings.Contains(goal, "关闭") && !strings.Contains(goal, "关掉") {
		return false
	}
	return strings.Contains(goal, "浏览器") || strings.Contains(goal, "浏览区") || strings.Contains(goal, "网页")
}

func closeOpenDocumentGoal(goal string) bool {
	if quitOnlyGoal(goal) {
		return false
	}
	if !strings.Contains(goal, "关闭") && !strings.Contains(goal, "关掉") {
		return false
	}
	return strings.Contains(goal, "文档") || strings.Contains(goal, "这个文件")
}

func directUserWindowClose(goal, name string) bool {
	if !closeCurrentBrowserGoal(goal) && !closeOpenDocumentGoal(goal) {
		return false
	}
	switch name {
	case "browser.act", "mcp.search", "mcp.call", "computer.act", "desktop.quit":
		return true
	}
	return false
}

// guardSystemBrowserClick refuses browser.act after the page is already in
// the user's system browser. That tool launches a second Chrome on about:blank.
func guardSystemBrowserClick(systemBrowserOpen bool, goal, name string) error {
	if !systemBrowserOpen || name != "browser.act" {
		return nil
	}
	if systemBrowserFirstResultGoal(goal) || websiteFirstResultGoal(goal) {
		return errors.New("搜索页已在系统浏览器里。请用 computer.act 点击第一条结果的标题，不要另开受控浏览器。")
	}
	return nil
}

func guardCurrentTurnTool(goal, name string) error {
	if closeCurrentBrowserGoal(goal) && (name == "browser.act" || name == "mcp.search" || name == "mcp.call") {
		return errors.New("关闭当前浏览器请用 computer.act 关掉已经打开的窗口，不要启动 browser.act 或 MCP。")
	}
	if quitOnlyGoal(goal) && (name == "computer.act" || strings.HasPrefix(name, "cc.") || name == "media.play") {
		return errors.New("关闭这个软件请用 desktop.quit，不要点屏幕，也不要暂停。")
	}
	if websiteFirstResultGoal(goal) && name == "computer.act" {
		return errors.New("本轮要打开网页里的第一条结果，请用 browser.act，不要用电脑像素点击。")
	}
	if companionGoalIsOpenOnly(goal) || companionDesktopFilenameFragment(goal) {
		switch name {
		case "workspace.list", "workspace.search", "workspace.read", "workspace.write", "command.run",
			"computer.act", "desktop.type", "browser.act", "media.play":
			return errors.New("本轮只要打开桌面文件、应用或页面，不得浏览工作区或跑命令。")
		case "desktop.browse":
			if companionGoalIsOpenPage(goal) {
				return nil
			}
			return errors.New("本轮只要打开桌面文件或应用，不得浏览工作区或跑命令。请只用 desktop.open。")
		}
	}
	if ownedMediaCenterGoal(goal) || moviePlayGoal(goal) || playerCloseGoal(goal) {
		switch name {
		case "web.search", "web.fetch", "browser.act", "computer.act", "desktop.open", "desktop.browse", "desktop.type":
			return errors.New("在自带媒体中心播放时只调用 media.play，target=center。不要检索网页，也不要操作其它软件。")
		}
	}
	if inventoryLookupGoal(goal) {
		switch name {
		case "web.search", "web.fetch", "browser.act":
			return errors.New("本轮是实时余票/航班查询：没有已接入的专用接口时不得抓网页或打开 12306。请说明尚未接入并结束本轮。")
		}
	}
	if !lookupOnlyTurn(goal) {
		return nil
	}
	switch name {
	case "desktop.open", "desktop.type", "desktop.quit", "desktop.browse", "media.play", "workspace.write", "workspace.edit", "im.send":
		if inventoryOpenPageAllowed(goal) && (name == "desktop.open" || name == "desktop.browse") {
			return nil
		}
		return errors.New("本轮只要求查询信息；不得执行历史任务中的打开文件、播放或写入操作。请继续本轮查询。")
	}
	return nil
}

func currentTurnBrowserLaunchFailed(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role == llmadapter.RoleTool && browserLaunchFailedOutput(m.Content) {
			return true
		}
	}
	return false
}

func currentTurnCompanionWindowBlocked(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role != llmadapter.RoleTool {
			continue
		}
		if strings.Contains(m.Content, "前台是月伴") || strings.Contains(m.Content, "禁止像素动作") || strings.Contains(m.Content, "前台仍是月伴") {
			return true
		}
	}
	return false
}

func currentTurnHasBrowserMCPNotReady(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role == llmadapter.RoleTool && strings.Contains(m.Content, "BROWSER_MCP_NOT_READY") {
			return true
		}
	}
	return false
}

// currentTurnBrowserActFailed checks whether any browser.act call in the
// current turn returned an error, indicating that the Playwright-based
// browser interaction is not available.
func currentTurnBrowserActFailed(messages []llmadapter.Message) bool {
	if currentTurnHasBrowserMCPNotReady(messages) {
		return true
	}
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	// Look for assistant tool calls to browser.act followed by a
	// tool-result containing ok:false.
	for i := start; i < len(messages); i++ {
		m := messages[i]
		if m.Role == llmadapter.RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.Name == "browser.act" {
					// Find the corresponding tool result.
					for j := i + 1; j < len(messages); j++ {
						if messages[j].Role == llmadapter.RoleTool && messages[j].ToolCallID == tc.ID && strings.Contains(messages[j].Content, "ok:false") {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// openOnlyFallbackTools are the rungs the desktop ladder climbs to when the
// dedicated open tool could not finish the job. The open-only guard keeps them
// out of an untouched turn — opening a file never needs a screen click — but
// once the dedicated attempt has failed they are the only way left to honour
// the request, so blocking them turns one failure into a wall of them.
func openOnlyFallbackTools(name string) bool {
	return name == "computer.act" || strings.HasPrefix(name, "cc.")
}

// currentTurnDedicatedOpenFailed reports whether this turn already asked a
// dedicated open tool to do the job and got a failure back.
func currentTurnDedicatedOpenFailed(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	results := map[string]string{}
	for i := start; i < len(messages); i++ {
		if messages[i].Role == llmadapter.RoleTool {
			results[messages[i].ToolCallID] = messages[i].Content
		}
	}
	for i := start; i < len(messages); i++ {
		for _, call := range messages[i].ToolCalls {
			switch call.Name {
			case "desktop.open", "desktop.browse", "media.play", "skill.invoke", "mcp.call":
			default:
				continue
			}
			out, ok := results[call.ID]
			if !ok {
				continue
			}
			if companionToolResultFailed(out) && !desktopLadderBlocked(out) {
				return true
			}
		}
	}
	return false
}

// currentTurnOpenUnproved is an open that came back without a passed check.
// The next ladder step is still allowed, so the window can actually be opened.
func currentTurnOpenUnproved(messages []llmadapter.Message) bool {
	out := lastNamedToolOutput(messages, "desktop.open")
	if out == "" || companionToolResultFailed(out) || !strings.Contains(out, "opened ") {
		return false
	}
	proof, ok := extractL0(out)
	if !ok || proof.Kind == "unverified" {
		return true
	}
	return !proof.Passed || proof.Uncertain
}

func currentTurnMediaKeyDelivered(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role == llmadapter.RoleTool && mediaKeyDelivered(m.Content) {
			return true
		}
	}
	return false
}

func currentTurnDesktopBrowserOpened(messages []llmadapter.Message) bool {
	out := strings.TrimSpace(lastNamedToolOutput(messages, "desktop.browse"))
	return strings.HasPrefix(out, "已向系统默认桌面浏览器发送打开请求") || strings.HasPrefix(out, "已打开桌面浏览器：")
}

// browserWindowConfirmed is the receipt after the desktop window was actually seen.
// A sent open request is not that receipt.
func browserWindowConfirmed(messages []llmadapter.Message) bool {
	out := strings.TrimSpace(lastNamedToolOutput(messages, "desktop.browse"))
	if companionToolResultFailed(out) {
		return false
	}
	return strings.HasPrefix(out, "已打开桌面浏览器：")
}

func currentTurnUnconfirmedPlay(messages []llmadapter.Message) bool {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser {
			start = i + 1
			break
		}
	}
	for _, m := range messages[start:] {
		if m.Role != llmadapter.RoleTool {
			continue
		}
		if strings.Contains(m.Content, "MEDIA_UNVERIFIED") || strings.Contains(m.Content, "playback not confirmed") {
			return true
		}
	}
	return false
}

func currentTurnOfficeOrgDenied(messages []llmadapter.Message) bool {
	out := lastNamedToolOutput(messages, "office.generate")
	return strings.Contains(out, "CROSS_ORG") || strings.Contains(out, "组织和已绑定的组织不一致")
}

func replyCarriesStalePlayback(reply, goal string) bool {
	if companionTurnWantsMusicPlay(goal) || strings.Contains(goal, "播放") {
		return false
	}
	return strings.Contains(reply, "汽水") || strings.Contains(reply, "放歌") || strings.Contains(reply, "播放控件") || strings.Contains(reply, "三步都走完")
}

func browseOpenStopsFurtherDesktop(goal string) bool {
	if websiteFirstResultGoal(goal) {
		return false
	}
	for _, verb := range []string{"点击", "点开", "点一下", "填写", "登录", "输入", "保存", "写入", "生成", "下载"} {
		if strings.Contains(goal, verb) {
			return false
		}
	}
	return true
}

// browseAlreadyOpenReceipt is the successful end of an open-and-search turn.
// desktop.browse already handed the page to the system browser. A later
// computer.act or browser.act must not be stored as a failure.
func browseAlreadyOpenReceipt(goal, name string, messages []llmadapter.Message) (string, bool) {
	if name != "computer.act" && name != "browser.act" && !strings.HasPrefix(name, "cc.") {
		return "", false
	}
	if !browserWindowConfirmed(messages) || !browseOpenStopsFurtherDesktop(goal) {
		return "", false
	}
	return "桌面浏览器已经打开搜索页。用已有检索结果直接回答，不要再点页面。", true
}

func guardCurrentTurnToolHistory(goal, name string, messages []llmadapter.Message) error {
	if strings.Contains(lastNamedToolOutput(messages, "media.play"), "MEDIA_CENTER") {
		return errors.New("媒体中心已经开始播放，不要再调用工具。")
	}
	if _, ok := settledWorkSpeech(goal, messages); ok {
		return errors.New("这一步已经做完。直接告诉用户结果，不要再调用工具。")
	}
	if name == "office.generate" && currentTurnOfficeOrgDenied(messages) {
		return errors.New("这一轮已经因为组织不一致没有写入，不要再调用 office.generate。")
	}
	if _, settled := browseAlreadyOpenReceipt(goal, name, messages); settled {
		return errors.New("系统浏览器已经打开。用已有检索结果直接回答，不要再点页面，也不要再启动自动化浏览器。")
	}
	if lookupFollowUpBlocked(goal, name, messages) {
		return errors.New("这一步的查询结果已经返回。直接回答，不要再搜索或点页面。")
	}
	if (name == "computer.act" || strings.HasPrefix(name, "cc.")) && currentTurnMediaKeyDelivered(messages) {
		if currentTurnUnconfirmedPlay(messages) && !observeShowsPlayback(lastNamedToolOutput(messages, "computer.act")) {
			// The play key was sent and the window has not shown playback yet.
		} else {
			return errors.New("播放已经送到播放器，不要再点屏幕。")
		}
	}
	if (name == "computer.act" || strings.HasPrefix(name, "cc.")) && currentTurnCompanionWindowBlocked(messages) {
		return errors.New("前台是月伴，请先说出要操作的软件。")
	}
	if err := guardCurrentTurnTool(goal, name); err != nil {
		// When the guard says "use browser.act instead of computer.act"
		// but browser.act has already failed this turn, allow
		// computer.act as the fallback rather than blocking everything.
		if name == "computer.act" && websiteFirstResultGoal(goal) && currentTurnBrowserActFailed(messages) {
			if currentTurnBrowserLaunchFailed(messages) || currentTurnHasBrowserMCPNotReady(messages) {
				return errors.New("浏览器没能启动，不要改用屏幕点击。")
			}
			return nil
		}
		// Same reasoning for an open-only turn: the dedicated tool has had
		// its go and failed, so let the ladder reach for the screen instead
		// of reporting a second refusal the user can do nothing about.
		if openOnlyFallbackTools(name) && (companionGoalIsOpenOnly(goal) || companionDesktopFilenameFragment(goal)) && (currentTurnDedicatedOpenFailed(messages) || currentTurnOpenUnproved(messages)) {
			return nil
		}
		return err
	}
	if name == "browser.act" && (currentTurnHasBrowserMCPNotReady(messages) || currentTurnBrowserLaunchFailed(messages)) {
		return errors.New("本轮浏览器没能启动，不得再调用 browser.act。请说明没打开页面并结束。")
	}
	if name == "computer.act" && websiteFirstResultGoal(goal) && currentTurnBrowserLaunchFailed(messages) {
		return errors.New("浏览器没能启动，不要改用屏幕点击。")
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

// settledLookupSpeech is the spoken end of a lookup once the evidence is in.
// Another search or a click after that is a new task.
func settledLookupSpeech(goal string, messages []llmadapter.Message) (string, bool) {
	if newsOpenGoal(goal) {
		for _, receipt := range currentTurnReceipts(messages) {
			if strings.Contains(receipt.Output, "first_hit") || strings.Contains(receipt.Output, "已打开第一条") || strings.Contains(receipt.Output, "已经打开第一条") {
				return "已经打开第一条。", true
			}
		}
		return "", false
	}
	if lookupHasMoreWork(goal) || (!looksLikeCurrentLookupTurn(goal) && !browserLookupOnlyGoal(goal)) {
		return "", false
	}
	if !currentLookupEvidence(messages) {
		return "", false
	}
	if browserLookupOnlyGoal(goal) {
		browse := lastNamedToolOutput(messages, "desktop.browse")
		if companionToolResultFailed(browse) || !browserWindowConfirmed(messages) {
			return "", false
		}
	}
	speech := lookupDoneSpeech(lastNamedToolOutput(messages, "web.search"))
	if speech == "查询完成，结果已经返回。" && !lookupBodyProved(messages) {
		return "", false
	}
	return speech, true
}

func lookupBodyProved(messages []llmadapter.Message) bool {
	weather := lastNamedToolOutput(messages, "weather.get")
	if strings.Contains(weather, "sampleMinC") || strings.Contains(weather, "sampleMaxC") {
		return !companionToolResultFailed(weather)
	}
	fetched := strings.TrimSpace(lastNamedToolOutput(messages, "web.fetch"))
	return len(fetched) > 40 && !companionToolResultFailed(fetched)
}

// settledWorkSpeech is the end of a single task once its own receipt is in.
// A later tool call is a new task. The turn says this sentence and stops.
// A timed WeChat chat and any goal that still has a later action stay open.
func settledWorkSpeech(goal string, messages []llmadapter.Message) (string, bool) {
	if _, _, wechat := parseWeChatChatGoal(goal); wechat {
		return "", false
	}
	if speech, ok := settledLookupSpeech(goal, messages); ok {
		return speech, true
	}
	if speech := mediaTransportSpeech(goal, messages); speech != "" {
		return speech, true
	}
	if moviePlayGoal(goal) || ownedMediaCenterGoal(goal) || playbackOnlyGoal(goal) {
		out := lastNamedToolOutput(messages, "media.play")
		if out != "" && !companionToolResultFailed(out) && !strings.Contains(out, "ok:false") && !strings.Contains(out, "MEDIA_CENTER_STOP") {
			if strings.Contains(out, "MEDIA_CENTER") {
				return mediaCenterReadySpeech(out), true
			}
			if observeShowsPlayback(lastNamedToolOutput(messages, "computer.act")) {
				return "已经在播了。", true
			}
			if !unverifiedMediaPlay("media.play", out, "") {
				speech := companionToolResultSpeech("media.play", out)
				proved := speech != "" && !strings.HasPrefix(speech, "已发送") && !strings.Contains(speech, "还没有确认") && speech != "完成。"
				if proved && mediaControlReceiptSpeech(out) != "" {
					proof, ok := extractL0(out)
					proved = ok && proof.Kind == "media-session" && proof.Passed && !proof.Uncertain
				}
				if proved {
					return speech, true
				}
			}
		}
	}
	if companionGoalIsOpenOnly(goal) {
		if text := computerReceiptCloseout(messages, goal); strings.Contains(text, "已打开目标") || strings.Contains(text, "已在系统浏览器打开") {
			return text, true
		}
	}
	if quitOnlyGoal(goal) {
		if text := computerReceiptCloseout(messages, goal); text != "" && !strings.Contains(text, "未确认") {
			return text, true
		}
	}
	if typedFieldOnlyGoal(goal) {
		if text := computerReceiptCloseout(messages, goal); strings.Contains(text, "已在目标输入框写入并核对") {
			return text, true
		}
	}
	if composerSendSettled(goal, messages) || sendOnlySettled(goal, messages) {
		return "已经发出去了。", true
	}
	if closeCurrentBrowserGoal(goal) || closeOpenDocumentGoal(goal) {
		if speech, ok := confirmedCloseSpeech(messages); ok {
			return speech, true
		}
	}
	if speech, ok := generatedDeliverableSpeech(goal, messages); ok {
		return speech, true
	}
	out := lastToolOutput(messages)
	if !containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "接着", "试用", "发布"}) && skillAuthoringSettled(toolNamesThisTurn(messages), out) && strings.Contains(out, "skillId=") {
		return "技能已经保存。", true
	}
	return "", false
}

func generatedDeliverableSpeech(goal string, messages []llmadapter.Message) (string, bool) {
	if containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "接着", "发给", "发送", "回读", "核对", "试用", "发布"}) {
		return "", false
	}
	if out := lastNamedToolOutput(messages, "canvas.present"); out != "" && !companionToolResultFailed(out) && !strings.Contains(out, "ok:false") && strings.Contains(out, "generated ") && !strings.Contains(out, "(0 bytes)") && !strings.Contains(out, "(0 byte)") {
		return "已经放到画布上。", true
	}
	for _, name := range []string{"pdf.gen", "docx.gen", "pptx.gen", "excel.gen", "html.gen", "office.generate"} {
		out := lastNamedToolOutput(messages, name)
		if out == "" || companionToolResultFailed(out) || strings.Contains(out, "ok:false") {
			continue
		}
		if !strings.Contains(out, "generated ") {
			continue
		}
		if strings.Contains(out, "(0 bytes)") || strings.Contains(out, "(0 byte)") {
			continue
		}
		return "文件已经生成。", true
	}
	return "", false
}

func confirmedCloseSpeech(messages []llmadapter.Message) (string, bool) {
	out := ""
	for _, name := range []string{"computer.act", "desktop.quit", "browser.act"} {
		if v := lastNamedToolOutput(messages, name); v != "" {
			out = v
		}
	}
	if out == "" || companionToolResultFailed(out) {
		return "", false
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "unverified") || strings.Contains(lower, "screen unchanged") {
		return "", false
	}
	if strings.Contains(lower, "closed") || strings.Contains(out, "已关闭") || strings.Contains(out, "已彻底退出") || strings.Contains(lower, "window close") {
		return "已经关掉了。", true
	}
	return "", false
}

func mediaTransportSpeech(goal string, messages []llmadapter.Message) string {
	if containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "接着"}) {
		return ""
	}
	if !containsAnyFold(goal, strings.ToLower(goal), []string{"下一", "上一", "暂停", "停止播放"}) {
		return ""
	}
	out := lastNamedToolOutput(messages, "media.play")
	proof, ok := extractL0(out)
	if !ok || proof.Kind != "media-session" || !proof.Passed || proof.Uncertain {
		return ""
	}
	speech := mediaControlReceiptSpeech(out)
	if speech == "" || strings.HasPrefix(speech, "已发送") {
		return ""
	}
	return speech
}

func sendOnlySettled(goal string, messages []llmadapter.Message) bool {
	if !strings.Contains(goal, "发") {
		return false
	}
	if containsAnyFold(goal, strings.ToLower(goal), []string{"然后", "之后", "接着", "查询", "搜索", "打开", "播放", "写入", "保存", "下载", "分钟"}) {
		return false
	}
	out := lastNamedToolOutput(messages, "im.send")
	return strings.Contains(out, "sent via") && !companionToolResultFailed(out) && !strings.Contains(out, "pending_external")
}

func toolNamesThisTurn(messages []llmadapter.Message) []string {
	var names []string
	for _, m := range messages {
		if m.Role == llmadapter.RoleUser {
			names = nil
		}
		for _, call := range m.ToolCalls {
			names = append(names, call.Name)
		}
	}
	return names
}

func lookupHasMoreWork(goal string) bool {
	for _, action := range []string{"写入", "保存", "生成", "制作", "编辑", "发送", "发给", "填写", "下载", "登录", "然后", "之后", "接着"} {
		if strings.Contains(goal, action) {
			return true
		}
	}
	return false
}

func lookupFollowUpBlocked(goal, name string, messages []llmadapter.Message) bool {
	if newsOpenGoal(goal) || lookupHasMoreWork(goal) {
		return false
	}
	if _, ok := settledLookupSpeech(goal, messages); !ok {
		return false
	}
	switch name {
	case "web.search", "web.fetch", "computer.act", "browser.act":
		return true
	case "desktop.browse":
		return currentTurnDesktopBrowserOpened(messages)
	default:
		return false
	}
}

func lookupDoneSpeech(search string) string {
	var titles []string
	for _, line := range strings.Split(search, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 4 || line[0] < '1' || line[0] > '9' || !strings.Contains(line, ". ") {
			continue
		}
		title := strings.TrimSpace(line[strings.Index(line, ". ")+2:])
		if title == "" {
			continue
		}
		titles = append(titles, title)
		if len(titles) == 3 {
			break
		}
	}
	if len(titles) == 0 {
		return "查询完成，结果已经返回。"
	}
	joined := strings.Join(titles, "；")
	if utf8.RuneCountInString(joined) > 80 {
		joined = string([]rune(joined)[:80])
	}
	return "查询完成。" + joined
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
	if !strings.HasPrefix(browse, "已打开桌面浏览器：") || !currentLookupEvidence(messages) {
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
	if currentTurnDesktopBrowserOpened(messages) && !browserWindowConfirmed(messages) && browseOpenStopsFurtherDesktop(goal) && !lookupHasMoreWork(goal) {
		return "已向默认浏览器发送打开请求，尚未核对页面。"
	}
	if browserWindowConfirmed(messages) && browseOpenStopsFurtherDesktop(goal) {
		if replyCarriesStalePlayback(reply, goal) {
			return "已经在桌面浏览器打开搜索页。"
		}
		if browserLookupOnlyGoal(goal) {
			if text := strings.TrimSpace(reply); text != "" && !looksLikeCompanionWaitPromise(reply) && !isCompanionLeadInOnly(reply) {
				return text
			}
		}
		return "已经在桌面浏览器打开搜索页。"
	}
	if !browserLookupOnlyGoal(goal) && currentTurnDesktopBrowserOpened(messages) && !browseOpenStopsFurtherDesktop(goal) {
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
