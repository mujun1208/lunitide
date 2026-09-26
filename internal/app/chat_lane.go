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

const (
	LaneL0    ChatLane = "L0"
	LaneL1    ChatLane = "L1"
	LaneL2    ChatLane = "L2"
	LaneL2Ask ChatLane = "L2-ask"
	LaneL3    ChatLane = "L3"
	LaneL4    ChatLane = "L4"
)

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
	Lane                       ChatLane
	Route                      TaskRoute
	DisableReasoning           bool
	SkipOfficeResearchPipeline bool
	AllowWebSearch             bool
	AllowOfficeGen             bool
	MaxMainToolSteps           int
	ContinueNudges             bool
	Council                    CouncilOverlay
	KeepSpecialistTools        bool
}

var numberedPointRE = regexp.MustCompile(`^\d+[\.、)]\s*\S`)

func chatLanesEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("LUNITIDE_CHAT_LANES")), "off")
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
		return LaneL0
	}
	if isShortIdleGreeting(goal) {
		return LaneL0
	}
	// Playing in the media center needs media.play on the first step. The
	// one-step read lane only loads the skill, then tells the user the step
	// budget ran out.
	if ownedMediaCenterGoal(goal) && !laneLooksLikeInPlaceProse(goal) {
		return LaneL4
	}
	// "继续" / "把没做完的做完" is the same task, not a new chat sentence.
	// The one-step read lane makes the model announce the rest and stop.
	if looksLikeFinishOpenWork(goal) {
		return LaneL4
	}
	if laneLooksLikeAgentTurn(goal) {
		return LaneL4
	}
	if capabilityWorkTask(goal) {
		return LaneL3
	}
	if mediaGenerationKind(goal) != "" {
		return LaneL3
	}
	if laneLooksLikeExternalUnderstand(goal) {
		return LaneL3
	}
	if laneLooksLikeResearch(goal) {
		return LaneL3
	}
	if looksLikeNovelTask(goal) {
		return LaneL3
	}
	// Any later pass over an existing deliverable — page, source, report,
	// sheet, or slides — stays on the full tool surface. A read-only or
	// one-step ask lane drops the write, so the model can only paste.
	if laneLooksLikeArtifactRevision(goal) {
		return LaneL3
	}
	// A skill chip, or any later turn after a file was already being produced,
	// stays on the full surface. Wording like "继续完成输出产物" or "把按钮改成蓝色"
	// does not have to repeat 修改/覆盖.
	if strings.Contains(goal, "[引用技能") {
		return LaneL3
	}
	if in.PriorDeliverable && !laneLooksLikeInPlaceProse(goal) {
		return LaneL3
	}
	// A page, HTML file, or POC has to be written and then run. One ask step
	// cannot do both, so the first output stays on the full tool surface.
	if fileNeedsRun(goal) || laneLooksLikeFirstFile(goal) {
		return LaneL3
	}
	materials := in.HasTurnMaterials || hasTurnMaterials(goal, false, false)
	if laneLooksLikeOfficeDeliverable(goal) {
		if materials {
			return LaneL2
		}
		return LaneL2Ask
	}
	if laneLooksLikeVagueTask(goal) {
		return LaneL2Ask
	}
	if laneLooksLikeInPlaceProse(goal) {
		return LaneL1
	}
	return LaneL1
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

// promoteLaneForSkillTrial keeps a draft-skill follow-up on the full tool
// surface. The first trial message is capability work; the next sentence
// ("改成第二版") would otherwise fall into the one-step read-only lane and
// the model can only paste source.
func promoteLaneForSkillTrial(lane ChatLane, trial bool) ChatLane {
	if !trial {
		return lane
	}
	switch lane {
	case LaneL0, LaneL4, LaneL2, LaneL3:
		return lane
	default:
		return LaneL3
	}
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

func buildLaneContract(lane ChatLane, route TaskRoute, overlay CouncilOverlay) LaneContract {
	c := LaneContract{Lane: lane, Route: route, Council: overlay}
	if c.Council.MaxSteps == 0 {
		c.Council.MaxSteps = councilStepsForLane(lane)
	}
	c.Council.Tools = overlay.Tools || councilToolsForLane(lane)
	c.Council.DisableReasoning = overlay.DisableReasoning || !councilToolsForLane(lane)
	switch lane {
	case LaneL0, LaneL1:
		c.DisableReasoning = true
		c.SkipOfficeResearchPipeline = true
		c.MaxMainToolSteps = 1
	case LaneL2:
		c.DisableReasoning = true
		c.SkipOfficeResearchPipeline = true
		c.AllowOfficeGen = true
		c.MaxMainToolSteps = 8
		c.ContinueNudges = true
	case LaneL2Ask:
		c.DisableReasoning = true
		c.SkipOfficeResearchPipeline = true
		c.MaxMainToolSteps = 1
	case LaneL3:
		c.AllowWebSearch = true
		c.AllowOfficeGen = true
		c.MaxMainToolSteps = 24
		c.ContinueNudges = true
	default: // L4
		c.AllowWebSearch = true
		c.AllowOfficeGen = true
		c.MaxMainToolSteps = 24
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
	materials := in.HasTurnMaterials || hasTurnMaterials(goal, false, false)
	if capabilityWorkTask(goal) && lane != LaneL4 && lane != LaneL0 {
		lane = LaneL3
	}
	if laneLooksLikeExternalUnderstand(goal) && lane != LaneL4 && lane != LaneL0 {
		lane = LaneL3
	}
	if route == RouteR2 || route == RouteR3 {
		lane = LaneL4
	}
	if (strings.Contains(goal, "画布") || wantsDefaultCanvas(goal)) && lane != LaneL0 && lane != LaneL4 {
		lane = LaneL3
	}
	if lookupOptedOut(goal) && lane == LaneL3 {
		if laneLooksLikeOfficeDeliverable(goal) || looksLikeNovelTask(goal) {
			if materials {
				lane = LaneL2
			} else {
				lane = LaneL2Ask
			}
		} else {
			lane = LaneL1
		}
	}
	if mediaGenerationKind(goal) != "" && lane != LaneL4 && lane != LaneL0 {
		lane = LaneL3
	}
	if laneWantsForcedSearch(goal) && lane != LaneL4 && lane != LaneL0 {
		if laneLooksLikeOfficeDeliverable(goal) || laneLooksLikeProse(goal) {
			lane = LaneL3
		}
	}
	c := buildLaneContract(lane, route, overlay)
	if (detectTaskRoute(goal) == RouteR1 || looksLikeCurrentLookupTurn(goal)) &&
		lane != LaneL0 && lane != LaneL2 && lane != LaneL2Ask && lane != LaneL4 {
		c.AllowWebSearch = true
		if c.MaxMainToolSteps < 4 {
			c.MaxMainToolSteps = 4
		}
		c.ContinueNudges = true
	}
	if lookupOptedOut(goal) {
		c.AllowWebSearch = false
	}
	if laneWantsDeepThink(goal) && lane != LaneL0 {
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
	if chatLanesEnabled() && c.SkipOfficeResearchPipeline {
		return false
	}
	return true
}

func laneAllowsWebSearch(c LaneContract) bool {
	if !chatLanesEnabled() || c.Lane == "" {
		return true
	}
	return c.AllowWebSearch
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
	if lane.Lane == LaneL0 || lane.Lane == LaneL1 || lane.Lane == LaneL2Ask {
		return false
	}
	return lane.ContinueNudges || lane.MaxMainToolSteps >= maxToolLoopSteps
}

func laneAllowsDesktopContinue(c LaneContract) bool {
	if !chatLanesEnabled() || c.Lane == "" {
		return true
	}
	return c.Lane == LaneL4
}

func startOfficeWorkflowsIfNeeded(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error, lane LaneContract, officeTaskID string, capabilityWork bool) {
	if turn == nil {
		return
	}
	if officeTaskID != "" || capabilityWork {
		turn.PptActive, turn.DocxActive = false, false
		return
	}
	if !shouldStartOfficeResearch(lane, officeTaskID, capabilityWork) {
		turn.PptActive, turn.DocxActive = false, false
		turn.SkipOfficeResearch = lane.SkipOfficeResearchPipeline
		if lane.AllowOfficeGen {
			injectReferenceOfficeOnce(req)
		}
		return
	}
	startPptWorkflow(req, turn, send)
	startDocxWorkflow(req, turn, send)
}

func injectReferenceOfficeOnce(req *llmadapter.Request) {
	if req == nil {
		return
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, referenceOfficeInstruction) {
			return
		}
	}
	req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: referenceOfficeInstruction})
}

func applyLaneTools(defs []llmadapter.ToolDefinition, c LaneContract) []llmadapter.ToolDefinition {
	if !chatLanesEnabled() || len(defs) == 0 {
		return defs
	}
	keep := map[string]bool{}
	switch c.Lane {
	case LaneL0:
		return nil
	case LaneL1:
		for _, d := range defs {
			if d.Name == "workspace.read" || d.Name == "office.inspect" ||
				d.Name == "kb.search" || d.Name == "kb.cite" || d.Name == "graph.expand" ||
				d.Name == "skill.invoke" || d.Name == "skill.try" ||
				d.Name == "skill.catalog.list" || d.Name == "skill.list" || d.Name == "skill.install" || d.Name == "skill.publish" ||
				d.Name == "user.ask" ||
				strings.HasPrefix(d.Name, "office.") {
				keep[d.Name] = true
			}
			if c.AllowWebSearch && (d.Name == "web.search" || d.Name == "web.fetch" ||
				d.Name == "weather.get" || d.Name == "location.get" || d.Name == "video.understand" ||
				d.Name == "memory.search" || d.Name == "memory.get") {
				keep[d.Name] = true
			}
		}
	case LaneL2Ask:
		for _, d := range defs {
			if d.Name == "user.ask" || d.Name == "todo.write" || d.Name == "kb.search" || d.Name == "kb.cite" || d.Name == "graph.expand" ||
				d.Name == "skill.invoke" || d.Name == "skill.try" ||
				d.Name == "skill.catalog.list" || d.Name == "skill.list" || d.Name == "skill.install" || d.Name == "skill.publish" ||
				strings.HasPrefix(d.Name, "office.") {
				keep[d.Name] = true
			}
		}
	case LaneL2:
		for _, d := range defs {
			if d.Name == "web.search" || d.Name == "web.fetch" {
				continue
			}
			keep[d.Name] = true
		}
	default:
		return defs
	}
	if c.KeepSpecialistTools {
		for _, d := range defs {
			if specialistToolAllow[d.Name] {
				keep[d.Name] = true
			}
		}
	}
	out := make([]llmadapter.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		if keep[d.Name] {
			out = append(out, d)
		}
	}
	return out
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

// restoreFileLandingTools puts write, and run when the file has to execute,
// back onto a list that a route or a narrow lane removed. The catalog is the
// tool list from before that stripping. In-place prose and greetings stay narrow.
func restoreFileLandingTools(filtered, catalog []llmadapter.ToolDefinition, goal string, prior bool, lane ChatLane) []llmadapter.ToolDefinition {
	if lane == LaneL0 || lane == "" || len(catalog) == 0 {
		return filtered
	}
	write, run := false, false
	switch lane {
	case LaneL2, LaneL3, LaneL4:
		write, run = true, true
	default:
		write, run = fileLandingNeed(goal, prior)
	}
	if !write && !run {
		return filtered
	}
	want := map[string]bool{}
	if write {
		for _, name := range []string{
			"workspace.write", "workspace.edit", "workspace.restore", "workspace.accept",
			"html.gen", "docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "office.generate", "canvas.present",
		} {
			want[name] = true
		}
	}
	if run {
		want["workspace.write"] = true
		want["workspace.edit"] = true
		want["workspace.restore"] = true
		want["workspace.accept"] = true
		want["command.run"] = true
		want["system.run"] = true
		want["html.gen"] = true
	}
	have := map[string]bool{}
	for _, d := range filtered {
		have[d.Name] = true
	}
	if wantsDefaultCanvas(goal) {
		for _, name := range []string{"html.gen", "docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "office.generate"} {
			delete(want, name)
		}
		want["canvas.present"] = true
	}
	for _, d := range catalog {
		if want[d.Name] && !have[d.Name] {
			filtered = append(filtered, d)
			have[d.Name] = true
		}
	}
	if wantsDefaultCanvas(goal) {
		drop := map[string]bool{"html.gen": true, "docx.gen": true, "pptx.gen": true, "excel.gen": true, "pdf.gen": true, "office.generate": true}
		kept := make([]llmadapter.ToolDefinition, 0, len(filtered))
		for _, d := range filtered {
			if drop[d.Name] {
				continue
			}
			kept = append(kept, d)
		}
		filtered = kept
	}
	return filtered
}

func fileLandingNeed(goal string, prior bool) (write bool, run bool) {
	g := chatRoutingText(goal)
	if g == "" || isShortIdleGreeting(g) || laneLooksLikeInPlaceProse(g) {
		return false, false
	}
	if prior || laneLooksLikeArtifactRevision(g) || strings.Contains(g, "[引用技能") || capabilityWorkTask(g) || runnableSystemRequest(g) || laneLooksLikeFirstFile(g) {
		return true, true
	}
	if laneLooksLikeOfficeDeliverable(g) {
		return true, fileNeedsRun(g)
	}
	return false, false
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
