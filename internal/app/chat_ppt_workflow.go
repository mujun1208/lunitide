package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Product-side PPT pipeline. A prompt alone cannot force tool loops, so
// runStream tracks stages, shows them in 思考, blocks early pptx.gen and
// office.generate(pptx), and nudges until research + copy exist. Stages:
//  1. 思考 clarify  2. 定义结构  3. 梳理写内容  4. 收集素材
//  5. 再思考  6. 再收集素材  7. 思考创作  8. 写完整页  9. office.generate 成稿
const (
	pptStageClarify   = "clarify"
	pptStageOutline   = "outline"
	pptStageCopy      = "copy"
	pptStageResearch1 = "research1"
	pptStageCritique  = "critique"
	pptStageResearch2 = "research2"
	pptStageCraft     = "craft"
	pptStageWrite     = "write"
	pptStageGenerate  = "generate"

	maxPptNudges = 6

	pptPipelineInstruction = "\n\n[PPT 九步流水线 · 产品强制]\n" +
		"做演示必须按序走完，禁止跳到 office.generate 或 pptx.gen：\n" +
		"1) 思考：用 todo.write 记下受众、时长、文类、语气、密度、页数。\n" +
		"2) 定义结构：先输出封面/目录/分节/收尾大纲（mermaid flowchart，节点双引号+<br/>），再继续。\n" +
		"3) 梳理写内容：为每一页写主张标题 + 3-5 条要点 + 演讲备注。\n" +
		"4) 收集素材：必须 web.search，需要原文再 web.fetch；可选用 image.generate 做封面图（生成后可在 office.generate 的 slides[].images 里用 sourceId+sha256 引用）。禁止编造数据与出处。\n" +
		"5) 再思考：对照网上素材改结构，删空话页。\n" +
		"6) 再收集素材：第二次 web.search 或 web.fetch 补缺口。\n" +
		"7) 思考创作：每页选 cover、section、agenda、content、two-column、comparison、quote、metrics、timeline、closing、table、conclusion、trend、structure、process、evidence 或 diagram。深色底必须浅色字。数字进 metrics，对照放 comparison，引用放 quote，流程放 process/timeline，趋势放 trend（trend 页必须配 charts 折线/柱状图呈现变化），层级放 structure，结论页 conclusion，证据页 evidence，数据表用 table，系统/审批/依赖等有向关系画 diagram（slides[].diagram 传 nodes+edges，2–12 节点、无环，连接语义写 edge label）。不要整本都是要点列表，不要问风格或品牌。\n" +
		"8) 写完整 PPT：每一页都有可见标题和正文，禁止空画布。\n" +
		"9) 最后生成：正文齐了才 office.generate（spec.kind 用 pptx）。一次写完 spec：每页 slides[].layout 用上面 17 种版式；数字用 metrics 并在 facts 里登记；有数据变化就配 charts（原生可编辑图表，series 值写成字符串小数）；已上传或生成的图片用 images（sourceId+sha256）；关系图用 diagram（原生可编辑节点+连接符）；指标/议程/流程页可加 icons 点睛（slides[].icons 传内置 MIT 图标名+x/y/size，size 建议 480000–960000 EMU，默认品牌色）；slides[].notes 写演讲备注，出处放 source。生成器自动做排版与质量检查，缺字或空页会被拒绝。只有 office.generate 明确不可用时才退回 pptx.gen。生成不完整不要写空文件冒充成功。\n" +
		"用户问进度时继续本流水线，不要重开一稿。\n"

	pptGenBlockedMsg = "ok:false\nPPT 生成被流水线拦住（office.generate / pptx.gen）：还没做完素材收集与正文撰写。先 web.search（必要时 web.fetch）至少两轮，写好每页标题+要点，再调用 office.generate。空页或只铺深色底的文件会被拒绝。\n"

	pptDirectInstruction = "\n\n[PPT 直接成稿]\n" +
		"用户已经把页数和事实写在这句话里。不要 web.search，不要 web.fetch，不要九步流水线，不要先勘查目录或问风格。\n" +
		"直接一次 office.generate 写完（spec.kind 用 pptx）：每页用 cover、section、agenda、content、two-column、comparison、quote、metrics、timeline、closing、table、conclusion、trend、structure、process、evidence 或 diagram。深色底必须浅色字，封面要有主标题和副标题，正文页要有标题和要点。数字用 metrics，对照放 comparison，引用放 quote，趋势页配 charts，有向关系画 diagram，出处放 source，slides[].notes 写演讲备注。只有 office.generate 明确不可用时才退回 pptx.gen。写完后报出文件名与存放位置。\n"
)

func pptContentInHand(text string) bool {
	if strings.TrimSpace(text) == "" || lookupActionRe.MatchString(text) {
		return false
	}
	if !looksLikePptTask(text) {
		return false
	}
	if countFactClauses(text) >= 2 {
		return true
	}
	if utf8.RuneCountInString(text) < 24 {
		return false
	}
	for _, mark := range []string{"岁", "担任", "简介", "个人介绍", "自我介绍", "工作经历", "副总"} {
		if strings.Contains(text, mark) {
			return true
		}
	}
	return false
}

func pptGoesStraightToFile(goal string) bool {
	return lookupOptedOut(goal) || referenceOnlyOfficeTurn(goal) || pptContentInHand(goal) ||
		hasTurnMaterials(goal, false, false)
}

func pptStraightInstruction(goal string) string {
	if referenceOnlyOfficeTurn(goal) && !pptContentInHand(goal) {
		return referenceOfficeInstruction
	}
	return pptDirectInstruction
}

func looksLikePptTask(text string) bool {
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || officeExpertIntroduction(text) || looksLikeStatusFollowUp(t) || looksLikeResume(t) {
		return false
	}
	for _, k := range []string{
		"ppt", "pptx", "幻灯片", "幻灯", "演示文稿", "演示稿", "ppt专家", "做ppt", "做一份ppt",
		"powerpoint", "slides",
	} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func pptTaskFromRequest(req llmadapter.Request, goal string) bool {
	if capabilityWorkTask(goal) || officeMaterialReview(goal) {
		return false
	}
	if looksLikePptTask(goal) {
		return true
	}
	if target := officeGenToolForGoal(goal); target != "" && target != "pptx.gen" {
		return false
	}
	if officeExpertIntroduction(goal) || !expertMountedIn(req, "PPT专家", "ppt-expert") {
		return false
	}
	g := strings.TrimSpace(goal)
	for _, k := range []string{"介绍", "汇报", "方案", "大纲", "改", "重做", "重新", "生成", "页"} {
		if strings.Contains(g, k) {
			return true
		}
	}
	return false
}

func pptWebPasses(turn *chatTurnCheckpoint) int {
	if turn == nil {
		return 0
	}
	n := 0
	for _, name := range turn.PptTools {
		if name == "web.search" || name == "web.fetch" {
			n++
		}
	}
	return n
}

func pptHasGen(turn *chatTurnCheckpoint) bool {
	return turn != nil && turn.PptGenerated
}

func notePptTools(turn *chatTurnCheckpoint, names []string) {
	if turn == nil || !turn.PptActive {
		return
	}
	turn.PptTools = append(turn.PptTools, names...)
	turn.PptStage = inferPptStage(turn)
}

func inferPptStage(turn *chatTurnCheckpoint) string {
	if turn == nil {
		return pptStageClarify
	}
	if pptHasGen(turn) {
		return pptStageGenerate
	}
	if lookupOptedOut(turn.Goal) || referenceOnlyOfficeTurn(turn.Goal) || pptContentInHand(turn.Goal) {
		return pptStageWrite
	}
	web := pptWebPasses(turn)
	hasTodo := false
	for _, name := range turn.PptTools {
		if name == "todo.write" {
			hasTodo = true
		}
	}
	switch {
	case web >= 2:
		return pptStageCraft
	case web == 1:
		return pptStageCritique
	case hasTodo:
		return pptStageCopy
	default:
		return pptStageOutline
	}
}

func nextPptStage(turn *chatTurnCheckpoint) string {
	switch inferPptStage(turn) {
	case pptStageClarify:
		return pptStageOutline
	case pptStageOutline:
		return pptStageCopy
	case pptStageCopy:
		return pptStageResearch1
	case pptStageResearch1:
		return pptStageCritique
	case pptStageCritique:
		return pptStageResearch2
	case pptStageResearch2:
		return pptStageCraft
	case pptStageCraft:
		return pptStageWrite
	case pptStageWrite:
		return pptStageGenerate
	default:
		return pptStageGenerate
	}
}

func pptPipelineReady(turn *chatTurnCheckpoint) bool {
	if turn == nil {
		return false
	}
	if turn.PptStage == pptStageWrite || turn.PptStage == pptStageGenerate {
		return true
	}
	if turn.PptNudges >= 4 {
		return true
	}
	web := pptWebPasses(turn)
	if web >= 2 {
		return true
	}
	// One successful research pass plus several nudges: don't deadlock if
	// the second search is empty, but never generate with zero web tools.
	return web >= 1 && turn.PptNudges >= 3
}

// officeGenerateIsPptx reports whether an office.generate call targets a
// PPTX deck. The strong office.generate chain serves every office kind, so
// the PPT pipeline only guards its pptx specs; docx/xlsx calls pass through.
func officeGenerateIsPptx(args json.RawMessage) bool {
	var a struct {
		Spec struct {
			Kind string `json:"kind"`
		} `json:"spec"`
	}
	if len(args) == 0 || json.Unmarshal(args, &a) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(a.Spec.Kind), "pptx")
}

func pptGenBlocked(turn *chatTurnCheckpoint, name string, args json.RawMessage) (bool, string) {
	isPptGen := name == "pptx.gen" || (name == "office.generate" && officeGenerateIsPptx(args))
	if !isPptGen || turn == nil || !turn.PptActive {
		return false, ""
	}
	if turn.SkipOfficeResearch || pptContentInHand(turn.Goal) {
		return false, ""
	}
	if pptPipelineReady(turn) || turn.PptStage == pptStageGenerate || turn.PptStage == pptStageWrite {
		return false, ""
	}
	return true, pptGenBlockedMsg
}

func blockedPptGenResult(msg string) toolruntime.Result {
	return toolruntime.Result{Output: msg}
}

func shouldContinuePptTurn(turn *chatTurnCheckpoint, disableReasoning bool) bool {
	_ = disableReasoning
	if turn == nil || !turn.PptActive {
		return false
	}
	if pptHasGen(turn) || turn.PptNudges >= maxPptNudges {
		return false
	}
	return true
}

func pptThinkingBanner(stage string) string {
	switch stage {
	case pptStageOutline:
		return "【PPT 流程 · 定义结构】先列出封面/目录/分节/收尾，用 mermaid 画页序。\n"
	case pptStageCopy:
		return "【PPT 流程 · 梳理写内容】为每一页写主张标题和 3-5 条要点。\n"
	case pptStageResearch1:
		return "【PPT 流程 · 收集素材】正在从网上检索可引用的事实与出处。\n"
	case pptStageCritique:
		return "【PPT 流程 · 再思考】对照已搜集素材检查结构是否站得住。\n"
	case pptStageResearch2:
		return "【PPT 流程 · 再收集素材】第二轮检索，补缺口，禁止编造。\n"
	case pptStageCraft:
		return "【PPT 流程 · 思考创作】定版式与对比度：深色底必须浅色字，16 种版式按内容选。\n"
	case pptStageWrite:
		return "【PPT 流程 · 写完整页】每一页都要有可见标题和正文，禁止空画布。\n"
	case pptStageGenerate:
		return "【PPT 流程 · 最后生成】正文齐了，调用 office.generate（spec.kind=pptx）产出带图表和图片的可编辑 PPTX。\n"
	default:
		return "【PPT 流程 · 思考】先弄清目标、受众、语气和页数，不要直接生成空文件。\n"
	}
}

func pptStageNudge(stage string) llmadapter.Message {
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: "继续 PPT 九步流水线的下一步（" + stage + "）。" +
		"还没有合格 office.generate（spec.kind=pptx）之前不要结束本轮。需要网上素材就调用 web.search / web.fetch。" +
		"禁止写只有深色底没有文字的幻灯片。"}
}

func startPptWorkflow(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) {
	if req == nil || turn == nil || req.DisableReasoning {
		return
	}
	if turn.PptActive {
		if pptGoesStraightToFile(turn.Goal) {
			turn.SkipOfficeResearch = true
			turn.PptStage = pptStageWrite
			req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: pptStraightInstruction(turn.Goal)})
		} else {
			injectPptPipelineOnce(req)
		}
		return
	}
	if !pptTaskFromRequest(*req, turn.Goal) {
		return
	}
	turn.PptActive = true
	if pptGoesStraightToFile(turn.Goal) {
		turn.SkipOfficeResearch = true
		turn.PptStage = pptStageWrite
		req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: pptStraightInstruction(turn.Goal)})
		return
	}
	if turn.PptStage == "" {
		turn.PptStage = pptStageClarify
	}
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: pptThinkingBanner(turn.PptStage)}})
	injectPptPipelineOnce(req)
}

func injectPptPipelineOnce(req *llmadapter.Request) {
	if req == nil {
		return
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "九步流水线") {
			return
		}
	}
	req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: pptPipelineInstruction})
}

func nudgePptWorkflow(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) bool {
	if !shouldContinuePptTurn(turn, req != nil && req.DisableReasoning) {
		return false
	}
	turn.PptNudges++
	stage := nextPptStage(turn)
	turn.PptStage = stage
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: pptThinkingBanner(stage)}})
	if req != nil {
		nudge := pptStageNudge(stage)
		if pptGoesStraightToFile(turn.Goal) {
			nudge.Content = pptStraightInstruction(turn.Goal)
		}
		req.Messages = append(req.Messages, nudge)
	}
	return true
}
