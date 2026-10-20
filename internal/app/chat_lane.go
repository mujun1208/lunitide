package app

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/videounderstand"
)

type ChatLane string

// v2 budget tiers (PRD 4.1). The tier only owns the budget plane: model tier,
// step baseline, and nudge policy. It never shrinks the tool surface.
const (
	LaneChat  ChatLane = "chat"  // greetings / in-place prose / casual turns
	LaneTask  ChatLane = "task"  // single-turn deliverables and research
	LaneAgent ChatLane = "agent" // multi-step agent turns (continue / code / desktop / media)
)

// Legacy v1.4 lane strings kept only as migrateLaneV1 inputs. Session replay
// and journal readers map them onto the v2 tiers; nothing else may consume
// them (FR-400).
const (
	LaneL0    ChatLane = "L0"
	LaneL1    ChatLane = "L1"
	LaneL2    ChatLane = "L2"
	LaneL2Ask ChatLane = "L2-ask"
	LaneL3    ChatLane = "L3"
	LaneL4    ChatLane = "L4"
)

// migrateLaneV1 is the single source of truth for v1.4 → v2 lane mapping
// (PRD 4.1). Pure function; shared by session restore, journal replay, and
// the grayscale switch. Unknown values fall back to the middle tier.
func migrateLaneV1(old ChatLane) ChatLane {
	switch old {
	case LaneL0, LaneL1:
		return LaneChat
	case LaneL2, LaneL2Ask, LaneL3:
		return LaneTask
	case LaneL4:
		return LaneAgent
	default:
		return LaneTask
	}
}

type LaneInput struct {
	Goal             string
	HasTurnMaterials bool
	Companion        bool
	OfficeTaskID     string
	// PriorDeliverable is true when this session already wrote, ran, or
	// invoked a skill to produce a file. The next turn keeps write and run
	// even when the user does not repeat 修改 or 覆盖.
	PriorDeliverable bool
}

type CouncilOverlay struct {
	Run              bool
	ExpertIDs        []string
	MaxSteps         int
	Tools            bool
	DisableReasoning bool
}

type LaneContract struct {
	Lane             ChatLane
	Route            TaskRoute
	DisableReasoning bool
	MaxMainToolSteps int
	ContinueNudges   bool
	Council          CouncilOverlay
}

var numberedPointRE = regexp.MustCompile(`^\d+[\.、)]\s*\S`)

func chatLanesEnabled() bool {
	// FR-700: "off" disables the whole v2 chain (budget lanes, state gates,
	// upgrade loop) and falls back to the profile full surface with no budget
	// tiers. The v1.4 stripping code no longer exists, so off is the simplest
	// mode, not the old behavior.
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("LUNITIDE_CHAT_LANES")), "off")
}

// stateGatesEnabled reports whether the run-time ToolGate rules (G-301..G-307)
// evaluate and intercept calls. Independent of the lane budget switch so a
// gate regression can be isolated without losing budget tiers (FR-700).
func stateGatesEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("LUNITIDE_STATE_GATES")), "off")
}

func resolveLaneGoal(text, checkpointGoal string) string {
	goal := chatRoutingText(text)
	if looksLikeResume(goal) && strings.TrimSpace(checkpointGoal) != "" {
		return strings.TrimSpace(checkpointGoal)
	}
	return goal
}

func classifyChatLane(in LaneInput) ChatLane {
	goal := chatRoutingText(in.Goal)
	if goal == "" {
		return LaneChat
	}
	if isShortIdleGreeting(goal) {
		return LaneChat
	}
	// Playing in the media center needs media.play on the first step. A
	// one-step chat tier only loads the skill, then tells the user the step
	// budget ran out.
	if ownedMediaCenterGoal(goal) && !laneLooksLikeInPlaceProse(goal) {
		return LaneAgent
	}
	// "继续" / "把没做完的做完" is the same task, not a new chat sentence.
	// The chat tier would make the model announce the rest and stop.
	if looksLikeFinishOpenWork(goal) {
		return LaneAgent
	}
	if laneLooksLikeAgentTurn(goal) {
		return LaneAgent
	}
	if capabilityWorkTask(goal) {
		return LaneTask
	}
	if mediaGenerationKind(goal) != "" {
		return LaneTask
	}
	if laneLooksLikeExternalUnderstand(goal) {
		return LaneTask
	}
	if laneLooksLikeResearch(goal) {
		return LaneTask
	}
	if looksLikeNovelTask(goal) {
		return LaneTask
	}
	// Any later pass over an existing deliverable — page, source, report,
	// sheet, or slides — stays on the task tier. The chat tier's small
	// budget can only paste.
	if laneLooksLikeArtifactRevision(goal) {
		return LaneTask
	}
	// A skill chip, or any later turn after a file was already being produced,
	// stays on the task tier. Wording like "继续完成输出产物" or "把按钮改成
	// 蓝色" does not have to repeat 修改/覆盖.
	if strings.Contains(goal, "[引用技能") {
		return LaneTask
	}
	if in.PriorDeliverable && !laneLooksLikeInPlaceProse(goal) {
		return LaneTask
	}
	// A page, HTML file, or POC has to be written and then run. The chat
	// tier's budget cannot do both, so the first output stays on task.
	if fileNeedsRun(goal) || laneLooksLikeFirstFile(goal) {
		return LaneTask
	}
	// Office deliverables (with or without materials) and vague tasks land on
	// task (PRD maps L2/L2-ask here): the no-materials generation gate (G-301)
	// and the ask-once gate (G-302) own the "ask first, then produce" behavior
	// at run time. Casual prose falls through to chat.
	if laneLooksLikeOfficeDeliverable(goal) || laneLooksLikeVagueTask(goal) {
		return LaneTask
	}
	return LaneChat
}

// laneLooksLikeArtifactRevision is a follow-up that changes an existing
// deliverable in place: another version of a page, source file, report,
// spreadsheet, or deck. In-place prose stays on L1. A fresh “写周报”
// is not a revision.
func laneLooksLikeArtifactRevision(goal string) bool {
	if laneLooksLikeInPlaceProse(goal) {
		return false
	}
	if strings.Contains(goal, "写一篇") || strings.Contains(goal, "写一段") {
		return false
	}
	// Bullet lines are the report's content ("修复支付超时"), not a request
	// to revise an existing file.
	t := strings.ToLower(revisionInstructionText(goal))
	for _, needle := range []string{
		"改版", "在原来", "原文件", "原稿", "上一版", "下一版", "再改一版", "再修一版", "原来的产物", "原来产物",
		"继续完成", "输出产物", "完成产物", "完成输出", "写出产物",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	if !laneHasRevisionVerb(t) {
		return false
	}
	if laneLooksLikeOfficeDeliverable(t) {
		return true
	}
	for _, needle := range []string{"覆盖", "落盘", "保存为", "第二版", "2.0", "v2", "v1", "产物", "文件", "版本", "页面", "源码", "交付", ".html", ".md", "readme", "docx", "xlsx", "pptx", "pdf"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func revisionInstructionText(goal string) string {
	var b strings.Builder
	for _, line := range strings.Split(goal, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") || numberedPointRE.MatchString(line) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// promoteLaneForSkillTrial keeps a draft-skill follow-up on the task tier.
// The first trial message is capability work; the next sentence ("改成第二
// 版") would otherwise fall into the chat tier and the model can only paste.
func promoteLaneForSkillTrial(lane ChatLane, trial bool) ChatLane {
	if !trial {
		return lane
	}
	if lane == LaneAgent {
		return lane
	}
	return LaneTask
}

func laneLooksLikeAgentTurn(goal string) bool {
	switch detectTaskRoute(goal) {
	case RouteR2, RouteR3:
		return true
	}
	if looksLikeComputerControlTurn(goal) || wantsAgentHostAct(goal) {
		return true
	}
	lower := strings.ToLower(goal)
	return containsAnyFold(goal, lower, []string{"命令", "终端", "脚本", "编译", "shell", "terminal", "command.run"})
}

func laneLooksLikeResearch(goal string) bool {
	for _, needle := range []string{
		"网上公开", "根据网上", "去网上查", "上网查", "检索", "调研", "找资料",
		"先搜索", "你先搜", "先搜",
	} {
		if strings.Contains(goal, needle) {
			return true
		}
	}
	return false
}

func laneHasRevisionVerb(goal string) bool {
	t := strings.ToLower(goal)
	for _, needle := range []string{"修改", "修复", "升级", "改版", "调整", "改进", "迭代", "覆盖", "改成", "落盘", "保存为", "第二版", "2.0", "v2", "v1"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func laneLooksLikeOfficeDeliverable(goal string) bool {
	if looksLikeReportTask(goal) || looksLikePptTask(goal) || looksLikePdfTask(goal) ||
		looksLikeExcelTask(goal) || wantsOfficeGen(goal) {
		return true
	}
	t := strings.ToLower(goal)
	return strings.Contains(t, "周报") || strings.Contains(t, "做ppt") || strings.Contains(t, "做一份ppt")
}

// looksLikeFinishOpenWork is a follow-up that asks to finish work already
// in progress. In-place prose that merely mentions those words stays L1.
func looksLikeFinishOpenWork(goal string) bool {
	if looksLikeResume(goal) {
		return true
	}
	if laneLooksLikeInPlaceProse(goal) {
		return false
	}
	t := revisionInstructionText(goal)
	for _, needle := range []string{"没做完", "未做完", "还没做完", "剩下的做", "做完剩下", "把拒绝的"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func laneLooksLikeVagueTask(goal string) bool {
	t := strings.TrimSpace(strings.TrimRight(goal, "。.!！？? "))
	switch t {
	case "帮我做个任务", "帮我做一下任务", "帮我做个事", "帮我做一下", "帮我做个任务吧":
		return true
	}
	return t == "做个任务"
}

func laneLooksLikeProse(goal string) bool {
	return laneLooksLikeInPlaceProse(goal)
}

// In-place polish/translate of provided text. Creating skills, summarizing
// URLs, and office deliverables are not L1 even if the sentence contains 写/总结.
func laneLooksLikeInPlaceProse(goal string) bool {
	if capabilityWorkTask(goal) || laneLooksLikeExternalUnderstand(goal) || mediaGenerationKind(goal) != "" {
		return false
	}
	if laneLooksLikeOfficeDeliverable(goal) {
		return false
	}
	for _, needle := range []string{"润色", "扩写", "翻译", "改写", "把这段"} {
		if strings.Contains(goal, needle) {
			return true
		}
	}
	if strings.Contains(goal, "写一篇") || strings.Contains(goal, "写一段") {
		return true
	}
	if strings.Contains(goal, "总结这段") || strings.Contains(goal, "总结一下这段") {
		return true
	}
	return false
}

func laneLooksLikeExternalUnderstand(goal string) bool {
	if _, _, ok := videounderstand.DetectShareURL(goal); ok {
		return true
	}
	lower := strings.ToLower(goal)
	hasURL := strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
	if !hasURL {
		return false
	}
	for _, needle := range []string{"总结", "解读", "解析", "分析", "看看", "帮我看", "视频"} {
		if strings.Contains(goal, needle) {
			return true
		}
	}
	return false
}

func hasTurnMaterials(goal string, hasAttachment, officeTaskHasUserFiles bool) bool {
	if hasAttachment || officeTaskHasUserFiles {
		return true
	}
	t := chatRoutingText(goal)
	if t == "" {
		return false
	}
	for _, needle := range []string{"见附件", "以下材料", "如下材料"} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	if hasAsAbovePointer(t) {
		return true
	}
	if strings.Contains(t, `:\`) || strings.Contains(t, `:/`) || strings.Contains(t, "/Users/") ||
		strings.Contains(t, "工作区") && (strings.Contains(t, "路径") || strings.Contains(t, "文件")) {
		return true
	}
	if countMaterialPoints(t) >= 2 {
		return true
	}
	if countFactClauses(t) >= 2 {
		return true
	}
	body := strings.NewReplacer("写周报", "", "周报", "", "请", "", "帮我", "").Replace(t)
	body = strings.TrimSpace(body)
	return utf8.RuneCountInString(body) >= 80
}

func countMaterialPoints(t string) int {
	n := 0
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") {
			if strings.TrimSpace(strings.TrimLeft(line, "-•* ")) != "" {
				n++
			}
			continue
		}
		if numberedPointRE.MatchString(line) {
			n++
		}
	}
	return n
}

func countFactClauses(t string) int {
	compact := strings.ReplaceAll(t, "写周报", "")
	parts := strings.FieldsFunc(compact, func(r rune) bool {
		return r == '。' || r == '；' || r == ';'
	})
	n := 0
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if utf8.RuneCountInString(p) >= 4 {
			n++
		}
	}
	return n
}

// v2 budget baselines (PRD 4.2). The tier owns the budget plane only.
const (
	laneChatBudgetSteps  = 4
	laneTaskBudgetSteps  = 12
	laneAgentBudgetSteps = 24
)

func buildLaneContract(lane ChatLane, route TaskRoute, overlay CouncilOverlay) LaneContract {
	c := LaneContract{Lane: lane, Route: route, Council: overlay}
	if c.Council.MaxSteps == 0 {
		c.Council.MaxSteps = councilStepsForLane(lane)
	}
	c.Council.Tools = overlay.Tools || councilToolsForLane(lane)
	c.Council.DisableReasoning = overlay.DisableReasoning || !councilToolsForLane(lane)
	switch lane {
	case LaneChat:
		c.DisableReasoning = true
		c.MaxMainToolSteps = laneChatBudgetSteps
	case LaneTask:
		c.MaxMainToolSteps = laneTaskBudgetSteps
		c.ContinueNudges = true
	default: // agent
		c.MaxMainToolSteps = laneAgentBudgetSteps
		c.ContinueNudges = true
	}
	return c
}

func hasAsAbovePointer(t string) bool {
	idx := 0
	for {
		i := strings.Index(t[idx:], "如上")
		if i < 0 {
			return false
		}
		i += idx
		rest := t[i+len("如上"):]
		if rest == "" || !strings.HasPrefix(rest, "周") {
			return true
		}
		idx = i + len("如上")
	}
}

func applyLaneOverrides(lane ChatLane, in LaneInput, route TaskRoute, overlay CouncilOverlay) (ChatLane, LaneContract) {
	goal := chatRoutingText(in.Goal)
	if lane != LaneAgent && lane != LaneChat {
		// Tier-promotion overrides only push task-tier turns around; chat
		// (greetings) and agent (desktop/media/continue) keep their tier.
		if capabilityWorkTask(goal) || laneLooksLikeExternalUnderstand(goal) ||
			strings.Contains(goal, "画布") || wantsDefaultCanvas(goal) ||
			mediaGenerationKind(goal) != "" ||
			(laneWantsForcedSearch(goal) && (laneLooksLikeOfficeDeliverable(goal) || laneLooksLikeProse(goal))) {
			lane = LaneTask
		}
	}
	if route == RouteR2 || route == RouteR3 {
		lane = LaneAgent
	}
	c := buildLaneContract(lane, route, overlay)
	// A greeting turn spends no tool steps: one model call answers directly.
	if lane == LaneChat && (strings.TrimSpace(goal) == "" || isShortIdleGreeting(goal)) {
		c.MaxMainToolSteps = 1
	}
	// FR-200 budget baselines (PRD 4.5): the route (keyword or flash
	// conclusion) may only shift the budget baseline, never the tool
	// surface. R1→task, R2/R3→agent, R4→task, NONE/low-confidence→agent
	// (conservative = full capability). Greeting turns keep their one-step
	// direct reply; every other turn gets the route floor.
	if !isShortIdleGreeting(goal) && strings.TrimSpace(goal) != "" {
		switch {
		case route == RouteR1 || detectTaskRoute(goal) == RouteR1 || looksLikeCurrentLookupTurn(goal):
			if c.MaxMainToolSteps < laneTaskBudgetSteps {
				c.MaxMainToolSteps = laneTaskBudgetSteps
			}
			c.ContinueNudges = true
		case route == RouteR2 || route == RouteR3:
			if c.MaxMainToolSteps < laneAgentBudgetSteps {
				c.MaxMainToolSteps = laneAgentBudgetSteps
			}
		case route == RouteR4:
			if c.MaxMainToolSteps < laneTaskBudgetSteps {
				c.MaxMainToolSteps = laneTaskBudgetSteps
			}
		case route == RouteUnspecified:
			if c.MaxMainToolSteps < laneAgentBudgetSteps {
				c.MaxMainToolSteps = laneAgentBudgetSteps
			}
			c.ContinueNudges = true
		}
	}
	if laneWantsDeepThink(goal) {
		c.DisableReasoning = false
	}
	return lane, c
}

func laneWantsForcedSearch(goal string) bool {
	return strings.Contains(goal, "先搜索") || strings.Contains(goal, "你先搜") || strings.Contains(goal, "上网查")
}

func laneWantsDeepThink(goal string) bool {
	return strings.Contains(goal, "深度思考") || strings.Contains(goal, "认真想")
}

func shouldStartOfficeResearch(c LaneContract, officeTaskID string, capabilityWork bool) bool {
	if officeTaskID != "" || capabilityWork {
		return false
	}
	_ = c
	return true
}

func laneAllowsContinueNudges(c LaneContract) bool {
	if !chatLanesEnabled() || c.Lane == "" {
		return true
	}
	return c.ContinueNudges
}

func capToolLoopLimit(base int, lane LaneContract) int {
	if !chatLanesEnabled() || lane.Lane == "" || lane.MaxMainToolSteps <= 0 {
		return base
	}
	if lane.MaxMainToolSteps < base {
		return lane.MaxMainToolSteps
	}
	return base
}

func laneMayExtendToolLoop(lane LaneContract) bool {
	if !chatLanesEnabled() || lane.Lane == "" {
		return true
	}
	if lane.Lane == LaneChat {
		return false
	}
	return lane.ContinueNudges || lane.MaxMainToolSteps >= maxToolLoopSteps
}

func laneAllowsDesktopContinue(c LaneContract) bool {
	if !chatLanesEnabled() || c.Lane == "" {
		return true
	}
	return c.Lane == LaneAgent
}

func startOfficeWorkflowsIfNeeded(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error, lane LaneContract, officeTaskID string, capabilityWork bool) {
	if turn == nil {
		return
	}
	if officeTaskID != "" || capabilityWork {
		turn.PptActive, turn.DocxActive = false, false
		return
	}
	// v2: the workflow starters own the offline/materials routing themselves —
	// pptGoesStraightToFile and the docx reference branch send the turn to
	// write-from-reference mode (stage=write, no web-research pipeline
	// prompt) while keeping the generator reachable for the auto-gen
	// fallback. The run-time gates (G-301/G-304) own "no materials → ask or
	// verify first" at call time.
	if !shouldStartOfficeResearch(lane, officeTaskID, capabilityWork) {
		turn.PptActive, turn.DocxActive = false, false
		return
	}
	startPptWorkflow(req, turn, send)
	startDocxWorkflow(req, turn, send)
}

// applyLaneTools is the v2 instrumentation point between the lane contract and
// the request tool list. FR-100: no lane may shrink the tool surface — the
// profile full set flows through unchanged; guards live in the run-time state
// gates. The shell is kept so future per-lane budget instrumentation has one
// place to hook without reintroducing whitelists.
func applyLaneTools(defs []llmadapter.ToolDefinition, c LaneContract) []llmadapter.ToolDefinition {
	if !chatLanesEnabled() || len(defs) == 0 {
		return defs
	}
	_ = c
	return defs
}

// checkpointWasDeliverable reports that an earlier turn in this session was
// already producing a file. The follow-up must be able to overwrite it.
func checkpointWasDeliverable(cp chatTurnCheckpoint) bool {
	if cp.CapabilityWork || cp.DocxGenerated || cp.PptGenerated || cp.DocxActive || cp.PptActive {
		return true
	}
	goal := chatRoutingText(cp.Goal)
	if goal != "" && !isShortIdleGreeting(goal) && !laneLooksLikeInPlaceProse(goal) {
		if laneLooksLikeOfficeDeliverable(goal) || laneLooksLikeArtifactRevision(goal) || fileNeedsRun(goal) || laneLooksLikeFirstFile(goal) || strings.Contains(goal, "[引用技能") || capabilityWorkTask(goal) {
			return true
		}
	}
	for _, name := range cp.LastTools {
		switch name {
		case "workspace.write", "workspace.edit", "command.run", "run_terminal_cmd",
			"html.gen", "docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "office.generate", "canvas.present",
			"skill.invoke", "skill.try":
			return true
		}
	}
	return false
}

func taskHasSeveralSteps(goal string) bool {
	g := chatRoutingText(goal)
	return containsAnyFold(g, strings.ToLower(g), []string{"然后", "并且", "同时", "接着", "另外", "之后再", "再把", "第一步"})
}

func directTaskTurn(goal string) bool {
	g := chatRoutingText(goal)
	if g == "" || isShortIdleGreeting(g) || taskHasSeveralSteps(g) || laneLooksLikeVagueTask(g) {
		return false
	}
	if strings.Contains(g, "周报") && !hasTurnMaterials(g, false, false) {
		return false
	}
	return true
}

func fileNeedsRun(goal string) bool {
	g := chatRoutingText(goal)
	if g == "" || isShortIdleGreeting(g) || laneLooksLikeInPlaceProse(g) {
		return false
	}
	lower := strings.ToLower(g)
	if strings.Contains(lower, "html") || strings.Contains(g, "源码") || strings.Contains(lower, "poc") || strings.Contains(g, "小游戏") {
		return true
	}
	if strings.Contains(g, "页面") && (strings.Contains(g, "运行") || strings.Contains(g, "代码") || strings.Contains(g, "做") || strings.Contains(g, "写") || strings.Contains(g, "生成") || strings.Contains(g, "输出")) {
		return true
	}
	return runnableSystemRequest(g)
}

func laneLooksLikeFirstFile(goal string) bool {
	g := chatRoutingText(goal)
	if g == "" || isShortIdleGreeting(g) || laneLooksLikeInPlaceProse(g) {
		return false
	}
	lower := strings.ToLower(g)
	for _, n := range []string{"readme", "说明文档", "交付物", "写到文件", "写成文件", "生成文件", "输出文件", "保存成文件", "保存为文件"} {
		if strings.Contains(lower, n) || strings.Contains(g, n) {
			return true
		}
	}
	return false
}
