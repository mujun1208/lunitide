package app

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
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
	if laneLooksLikeAgentTurn(goal) {
		return LaneL4
	}
	if laneLooksLikeResearch(goal) {
		return LaneL3
	}
	if looksLikeNovelTask(goal) {
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
	if laneLooksLikeProse(goal) {
		return LaneL1
	}
	return LaneL1
}

func laneLooksLikeAgentTurn(goal string) bool {
	switch detectTaskRoute(goal) {
	case RouteR2, RouteR3:
		return true
	}
	if looksLikeComputerControlTurn(goal) {
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

func laneLooksLikeOfficeDeliverable(goal string) bool {
	if looksLikeReportTask(goal) || looksLikePptTask(goal) || looksLikePdfTask(goal) ||
		looksLikeExcelTask(goal) || wantsOfficeGen(goal) {
		return true
	}
	t := strings.ToLower(goal)
	return strings.Contains(t, "周报") || strings.Contains(t, "做ppt") || strings.Contains(t, "做一份ppt")
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
	for _, needle := range []string{"写", "润色", "总结", "扩写", "翻译", "改写"} {
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
	if route == RouteR2 || route == RouteR3 {
		lane = LaneL4
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
	return lane.MaxMainToolSteps >= maxToolLoopSteps
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
				strings.HasPrefix(d.Name, "office.") {
				keep[d.Name] = true
			}
			if c.AllowWebSearch && (d.Name == "web.search" || d.Name == "web.fetch" ||
				d.Name == "weather.get" || d.Name == "video.understand" ||
				d.Name == "memory.search" || d.Name == "memory.get" || d.Name == "user.ask") {
				keep[d.Name] = true
			}
		}
	case LaneL2Ask:
		for _, d := range defs {
			if d.Name == "user.ask" || d.Name == "kb.search" || d.Name == "kb.cite" || d.Name == "graph.expand" ||
				d.Name == "skill.invoke" || d.Name == "skill.try" ||
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
