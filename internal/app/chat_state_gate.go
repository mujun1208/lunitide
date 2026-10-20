package app

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// FR-300: run-time state signals and the tool gate table (PRD 4.3/4.4).
//
// First principle: the only legal basis for intercepting a call is the
// observable task state or an explicit user action — never a text-intent
// prediction. Gates intercept individual calls; they never shrink the tool
// list. Every interception is user-visible (thinking notice) and feeds a
// structured reason back to the model so it can switch to a legal path.

// stateSignals is the narrow, observable task state the gates evaluate.
type stateSignals struct {
	// HasMaterials: the turn already carries materials (attachment markers,
	// pasted notes, or fact clauses) — write-from-reference is legal.
	HasMaterials bool
	// GenerationRequested: the call stream is about to hit a generation call
	// (*.gen, office.generate) while no material path exists.
	GenerationRequested bool
	// CouncilMountedGE2: the session has >= 2 experts mounted (council budget
	// cap G-307 applies).
	CouncilMountedGE2 bool
	// FileJustOpened: desktop.open just succeeded and nothing has written
	// since — the goal is already achieved (G-303).
	FileJustOpened bool
	// MediaContinueAction: the turn is a media-session action (play with no
	// query / pause / next / previous / stop) — G-306 forbids web lookups.
	MediaContinueAction bool
	// TicketQueryNoSource: a fail-closed ticket-like query with no dedicated
	// source mounted (G-305).
	TicketQueryNoSource bool
	// BudgetNearExhausted: step >= MaxMainToolSteps-1 with zero successful
	// tool calls — the FR-500 escalation loop arms.
	BudgetNearExhausted bool
	// LookupIntentLowConfidence: flash said NONE or the route is unspecified
	// — budget baselines go conservative (FR-200 already handles it).
	LookupIntentLowConfidence bool
	// ToolFailures: total failed tool calls this turn (ok:false results).
	// G-308's total stop-loss arms at 4.
	ToolFailures int
	// LastFailedTool / LastFailStreak: the tool failing most recently and
	// how many times in a row. G-308 blocks its 3rd retry.
	LastFailedTool string
	LastFailStreak int
}

// collectTurnSignals gathers the turn-start signals (PRD 4.3). Called once
// per turn; the run loop folds call-time states in as the turn progresses.
func collectTurnSignals(goal string, route TaskRoute, mediaSessionActive bool) stateSignals {
	return stateSignals{
		HasMaterials:        hasTurnMaterials(goal, false, false),
		TicketQueryNoSource: inventoryLookupBlocksPublicWeb(goal),
		MediaContinueAction: mediaSessionContinueGoal(goal, mediaSessionActive),
		// GenerationRequested/FileJustOpened/BudgetNearExhausted are
		// stream-time states; the run loop owns them.
		LookupIntentLowConfidence: route == RouteUnspecified,
	}
}

// mediaSessionFromCheckpoint reports whether the previous turn drove the
// in-app media session: a media.* tool call or an owned media-center goal.
// Bare continue words in the next turn inherit the session context from it.
func mediaSessionFromCheckpoint(prev chatTurnCheckpoint) bool {
	for _, name := range prev.LastTools {
		if strings.HasPrefix(name, "media.") {
			return true
		}
	}
	return ownedMediaCenterGoal(prev.Goal)
}

// assistantAsksQuestion reports a reply that ends in a question mark: the
// model is asking the user for missing inputs (G-302).
func assistantAsksQuestion(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasSuffix(t, "？") || strings.HasSuffix(t, "?")
}

// promoteLaneBudget is the FR-500 escalation: chat → task → agent, at most
// twice per turn, refilling the step baseline. Returns false when already
// at the top tier.
func promoteLaneBudget(c *LaneContract, count *int) bool {
	if c == nil || count == nil {
		return false
	}
	switch c.Lane {
	case LaneChat:
		c.Lane = LaneTask
	case LaneTask:
		c.Lane = LaneAgent
	default:
		return false
	}
	*count++
	switch c.Lane {
	case LaneTask:
		c.MaxMainToolSteps = laneTaskBudgetSteps
	default:
		c.MaxMainToolSteps = laneAgentBudgetSteps
	}
	c.DisableReasoning = false
	c.ContinueNudges = true
	return true
}

// mediaSessionContinueGoal reports a media-session control action (play with
// no new song requested / pause / next / previous / stop): the only legal
// path is media.play with the session action, never a web lookup. A bare
// continue word ("下一首") only counts while the session is active (the
// previous turn drove the in-app media session); naming the media center
// explicitly carries its own context.
func mediaSessionContinueGoal(goal string, mediaSessionActive bool) bool {
	if !ownedMediaCenterGoal(goal) && !mediaSessionActive {
		return false
	}
	text := strings.ToLower(goal)
	for _, k := range []string{"暂停", "继续播放", "接着播", "下一首", "上一首", "切歌", "停止播放", "别播了"} {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// isGenerationCall reports a file-generation tool: the office family and
// office.generate. Writing files is the gated class (G-301/G-304).
func isGenerationCall(name string) bool {
	switch name {
	case "office.generate", "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen":
		return true
	}
	return false
}

// isWriteCall reports any write primitive: generation calls plus direct
// file writes. G-303 blocks these right after a successful desktop.open.
func isWriteCall(name string) bool {
	if isGenerationCall(name) {
		return true
	}
	switch name {
	case "workspace.write", "file.write", "pptx.edit", "docx.edit", "excel.edit":
		return true
	}
	return false
}

// fileOpenWantsWrite reports the explicit user actions that legitimately
// follow an open with a write (convert / save-as / modify the file).
func fileOpenWantsWrite(goal string) bool {
	text := strings.ToLower(goal)
	for _, k := range []string{"转换", "转成", "另存", "保存为", "修改", "改一下", "编辑", "追加", "补充"} {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// gateDecision is one rule-table verdict.
type gateDecision struct {
	GateID string
	Deny   bool
	// Reason is fed back to the model as the tool result so it can switch to
	// a legal path instead of retrying the same call.
	Reason string
	// Notice is the user-visible thinking banner.
	Notice string
}

// g308WouldDeny reports whether the G-308 stop-loss alone would deny this
// call. The parallel pre-start path runs before the per-call gate table;
// checking the stop-loss there keeps a denied retry from actually executing
// on a background goroutine (the per-call G-301..G-306 denials are policy
// refusals on read-only calls — pre-starting them wastes at most one lookup,
// but a G-308 denial means the channel is broken, so executing it is exactly
// the waste the stop-loss exists to prevent).
func g308WouldDeny(sig stateSignals, toolName string) bool {
	if toolName == "user.ask" {
		return false
	}
	return sig.ToolFailures >= 4 || (sig.LastFailStreak >= 2 && toolName == sig.LastFailedTool)
}

// evalToolGate evaluates the ordered rule table (G-301..G-306) before a
// tool call. First hit wins; the rules are mutually exclusive by design
// (G-301 skips report tasks — G-304 owns them). G-307 is a budget cap, not
// a call gate: it lives in councilStepsForLane and is journaled when the
// council starts.
//
// officePipelineOwns: the PPT/DOCX pipelines enforce their own research
// stages (pptGenBlocked and the docx twin) — double-gating the same call
// would deadlock a legal pipeline, so the table skips those turns.
func evalToolGate(sig stateSignals, goal string, toolName string, args json.RawMessage, messages []llmadapter.Message, officePipelineOwns bool) gateDecision {
	if !stateGatesEnabled() {
		return gateDecision{}
	}
	// Signals that only exist at call time. Conversion/save-as goals carry
	// their source file by definition ("把这个文件转换成PDF") — the user
	// already pointed at the material, so G-301/G-304 must not fire.
	generationRequested := isGenerationCall(toolName) && !sig.HasMaterials &&
		!turnAttemptedWebLookup(messages) && !fileOpenWantsWrite(goal)
	switch {
	// G-301 no-materials generation gate (non-report turns): ask once.
	// user.ask and web lookup stay legal paths.
	case generationRequested && !looksLikeReportTask(goal) && !officePipelineOwns:
		return gateDecision{
			GateID: "G-301",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-301 材料未就绪] 本轮还没有可写入的材料。先用自然语言向用户确认缺什么，一次问清即停；或先 web.search 核实事实。禁止凭空生成文件。",
			Notice: "【闸口 G-301】材料未就绪：先问清缺什么或核实，不直接生成。",
		}
	// G-304 research-pipeline gate (report-like turns): verify first.
	case generationRequested && looksLikeReportTask(goal) && !officePipelineOwns:
		return gateDecision{
			GateID: "G-304",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-304 报告需先核实] 报告类交付禁止无材料直接生成。先 web.search 查证缺的事实并标注来源；核实不到就向用户问一句缺什么。user.ask 与 web 查询不受限。",
			Notice: "【闸口 G-304】报告先核实再生成，禁止编造。",
		}
	// G-303 file-open-settled gate: the open already achieved the goal.
	case sig.FileJustOpened && isWriteCall(toolName) && !fileOpenWantsWrite(goal):
		return gateDecision{
			GateID: "G-303",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-303 打开已完成] 文件已打开，用户目标已达成。不要读取后再生成一份；如用户要求转换/修改，等用户明说。",
			Notice: "【闸口 G-303】文件已打开即完成，不再生成副本。",
		}
	// G-305 ticket fail-closed gate: no public-web substitute.
	case sig.TicketQueryNoSource && (toolName == "web.search" || toolName == "web.fetch" || toolName == "browser.act"):
		return gateDecision{
			GateID: "G-305",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-305 实时票务未接入] 这类查询依赖专用接口，公开网页结果不可信。先 mcp.search 找票务/行情接口；没有就向用户说明未接入并结束，不要用网页数据冒充。",
			Notice: "【闸口 G-305】实时票务未接入，fail-closed，不用公开网页替代。",
		}
	// G-306 media-continue gate: session action, not a lookup.
	case sig.MediaContinueAction && (toolName == "web.search" || toolName == "web.fetch"):
		return gateDecision{
			GateID: "G-306",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-306 续播走会话] 这是媒体会话控制（暂停/切歌/继续），直接用 media.play 的 action，不要抽歌名去搜索。",
			Notice: "【闸口 G-306】续播直接走媒体会话，不搜索。",
		}
	// G-308 channel-failure stop-loss (total): the turn is drowning in
	// failures — likely an environment fault (dead system proxy, offline).
	// Only user.ask stays legal; everything else must answer from what the
	// turn already has. This is what keeps a broken web stack from burning
	// the full agent-tier budget on retries.
	case sig.ToolFailures >= 4 && toolName != "user.ask":
		return gateDecision{
			GateID: "G-308",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-308 通道故障止损] 本轮工具已累计失败 " + strconv.Itoa(sig.ToolFailures) + " 次，疑似环境故障（如系统代理不可用、断网）。停止调用工具：基于已有信息直接回答用户；信息不足时，明确告诉用户哪个通道故障，请用户检查网络或代理设置。",
			Notice: "【闸口 G-308】工具多次失败已止损：基于已有信息回答，并告知用户疑似环境故障。",
		}
	// G-308 channel-failure stop-loss (per tool): the same tool just failed
	// twice in a row — a third blind retry is a waste. Switching channels
	// (a different tool) stays legal.
	case sig.LastFailStreak >= 2 && toolName == sig.LastFailedTool && toolName != "user.ask":
		return gateDecision{
			GateID: "G-308",
			Deny:   true,
			Reason: "ok:false\n[闸口 G-308 单通道止损] 工具 " + toolName + " 已连续失败 " + strconv.Itoa(sig.LastFailStreak) + " 次，该通道疑似故障。不要再次调用它：改用其他工具通道，或基于已有信息直接回答；无法继续时明确告知用户故障情况。",
			Notice: "【闸口 G-308】工具 " + toolName + " 连续失败已止损：换通道或直接回答。",
		}
	}
	return gateDecision{}
}

// journalGate writes the interception record (PRD 4.4): gate id, signal
// snapshot, tool name. The unified audit consumer reads these lines.
func journalGate(streamID, gateID, toolName string, sig stateSignals) {
	log.Printf("chat.gate stream=%s gate=%s tool=%s signals=materials:%t,gen:%t,council:%t,fileOpen:%t,media:%t,ticket:%t,budget:%t,lowConf:%t,failures:%d,lastFail:%s/%d",
		streamID, gateID, toolName,
		sig.HasMaterials, sig.GenerationRequested, sig.CouncilMountedGE2,
		sig.FileJustOpened, sig.MediaContinueAction, sig.TicketQueryNoSource,
		sig.BudgetNearExhausted, sig.LookupIntentLowConfidence,
		sig.ToolFailures, sig.LastFailedTool, sig.LastFailStreak)
}

// gateOfficePipelineOwns reports whether the office pipeline's own stage
// machine owns generation gating for this turn (PPT or DOCX active): its
// research-stage blocks (pptGenBlocked et al) are stricter and know the
// turn's progress, so the generic table steps aside.
func gateOfficePipelineOwns(turn *chatTurnCheckpoint) bool {
	return turn != nil && (turn.PptActive || turn.DocxActive)
}

// updateSignalsAfterTool folds a completed tool call into the signal state.
// Called once per tool completion (success or failure — the output's
// ok:false prefix marks failures).
func updateSignalsAfterTool(sig *stateSignals, name string, args json.RawMessage, output string) {
	if sig == nil {
		return
	}
	// FR-300 G-308 fold: count failures and track the failing streak. A
	// later success of the same tool clears its streak; a different tool's
	// outcome leaves the streak untouched. Gate denials ("ok:false\n[闸口")
	// are refusals, not faults — they must not pollute the failure count.
	out := strings.TrimSpace(output)
	failed := strings.HasPrefix(out, "ok:false") && !strings.HasPrefix(out, "ok:false\n[闸口")
	if failed {
		sig.ToolFailures++
		if sig.LastFailedTool == name {
			sig.LastFailStreak++
		} else {
			sig.LastFailedTool = name
			sig.LastFailStreak = 1
		}
	} else if name == sig.LastFailedTool && !strings.HasPrefix(out, "ok:false") {
		sig.LastFailedTool = ""
		sig.LastFailStreak = 0
	}
	switch {
	case name == "desktop.open" && !strings.Contains(strings.ToLower(output), "ok:false"):
		// Fresh open: goal achieved unless the turn already wrote something
		// (conversion/modify flows keep their write rights).
		sig.FileJustOpened = true
	case isWriteCall(name):
		// Any write invalidates the "just opened, done" state.
		sig.FileJustOpened = false
	case name == "web.search" || name == "web.fetch":
		// Web evidence collected: write-from-verified is legal now. Only a
		// real success counts — a failed lookup must not fake materials.
		if !strings.HasPrefix(strings.TrimSpace(output), "ok:false") {
			sig.HasMaterials = true
		}
	}
	_ = args
}
