package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Product-side PPT pipeline. A prompt alone cannot force tool loops, so
// runStream tracks stages, shows them in 思考, blocks early pptx.gen, and
// nudges until research + copy exist. Stages:
//  1. 思考 clarify  2. 定义结构  3. 梳理写内容  4. 收集素材
//  5. 再思考  6. 再收集素材  7. 思考创作  8. 写完整页  9. pptx.gen
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
		"做演示必须按序走完，禁止跳到 pptx.gen：\n" +
		"1) 思考：用 todo.write 记下受众、时长、语气、页数。\n" +
		"2) 定义结构：先输出封面/目录/分节/收尾大纲（mermaid flowchart，节点双引号+<br/>），再继续。\n" +
		"3) 梳理写内容：为每一页写主张标题 + 3-5 条要点 + 演讲备注。\n" +
		"4) 收集素材：必须 web.search，需要原文再 web.fetch；可选用 image.generate 做封面图。禁止编造数据与出处。\n" +
		"5) 再思考：对照网上素材改结构，删空话页。\n" +
		"6) 再收集素材：第二次 web.search 或 web.fetch 补缺口。\n" +
		"7) 思考创作：定 layout=title/section/content，深色页必须浅色字。\n" +
		"8) 写完整 PPT：每一页都有可见标题和正文，禁止空画布。\n" +
		"9) 最后生成：只有正文齐了才 pptx.gen（桌面则 desktop=true）。生成不完整不要写空文件冒充成功。\n" +
		"用户问进度时继续本流水线，不要重开一稿。\n"

	pptGenBlockedMsg = "ok:false\npptx.gen 被流水线拦住：还没做完素材收集与正文撰写。先 web.search（必要时 web.fetch）至少两轮，写好每页标题+要点，再调用 pptx.gen。空页或只铺深色底的文件会被拒绝。\n"
)

func looksLikePptTask(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || looksLikeStatusFollowUp(t) || looksLikeResume(t) {
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

func pptTaskFromRequest(req gateway.Request, goal string) bool {
	if looksLikePptTask(goal) {
		return true
	}
	expertMounted := false
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "PPT专家") || strings.Contains(m.Content, "ppt-expert") {
			expertMounted = true
			break
		}
	}
	if !expertMounted {
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
	for _, name := range names {
		seen := false
		for _, existing := range turn.PptTools {
			if existing == name {
				seen = true
				break
			}
		}
		if !seen {
			turn.PptTools = append(turn.PptTools, name)
		}
	}
	turn.PptStage = inferPptStage(turn)
}

func inferPptStage(turn *chatTurnCheckpoint) string {
	if turn == nil {
		return pptStageClarify
	}
	if pptHasGen(turn) {
		return pptStageGenerate
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

func pptGenBlocked(turn *chatTurnCheckpoint, name string) (bool, string) {
	if name != "pptx.gen" || turn == nil || !turn.PptActive {
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
	if disableReasoning || turn == nil || !turn.PptActive {
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
		return "【PPT 流程 · 思考创作】定版式与对比度：深色底必须浅色字。\n"
	case pptStageWrite:
		return "【PPT 流程 · 写完整页】每一页都要有可见标题和正文，禁止空画布。\n"
	case pptStageGenerate:
		return "【PPT 流程 · 最后生成】正文齐了，调用 pptx.gen 写出可打开的 PPTX。\n"
	default:
		return "【PPT 流程 · 思考】先弄清目标、受众、语气和页数，不要直接生成空文件。\n"
	}
}

func pptStageNudge(stage string) gateway.Message {
	return gateway.Message{Role: gateway.RoleSystem, Content: "继续 PPT 九步流水线的下一步（" + stage + "）。" +
		"还没有合格 pptx.gen 之前不要结束本轮。需要网上素材就调用 web.search / web.fetch。" +
		"禁止写只有深色底没有文字的幻灯片。"}
}

func startPptWorkflow(req *gateway.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) {
	if req == nil || turn == nil || req.DisableReasoning {
		return
	}
	if turn.PptActive {
		injectPptPipelineOnce(req)
		return
	}
	if !pptTaskFromRequest(*req, turn.Goal) {
		return
	}
	turn.PptActive = true
	if turn.PptStage == "" {
		turn.PptStage = pptStageClarify
	}
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: pptThinkingBanner(turn.PptStage)}})
	injectPptPipelineOnce(req)
}

func injectPptPipelineOnce(req *gateway.Request) {
	if req == nil {
		return
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "九步流水线") {
			return
		}
	}
	req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleSystem, Content: pptPipelineInstruction})
}

func nudgePptWorkflow(req *gateway.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) bool {
	if !shouldContinuePptTurn(turn, req != nil && req.DisableReasoning) {
		return false
	}
	turn.PptNudges++
	stage := nextPptStage(turn)
	turn.PptStage = stage
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: pptThinkingBanner(stage)}})
	if req != nil {
		req.Messages = append(req.Messages, pptStageNudge(stage))
	}
	return true
}
