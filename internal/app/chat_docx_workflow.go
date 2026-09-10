package app

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Product-side Word pipelines (report + novel). Prompt-only experts dump
// empty 11pt docs; runStream tracks stages, shows them in 思考, blocks
// early docx.gen, and nudges until research/outline + body exist.
const (
	referenceOfficeInstruction = "本轮仅使用已有材料或用户指定的离线信息生成文件，不套用联网调研步骤。先完整读取材料，再调用本轮要求格式的生成工具。保留正文与格式校验，不补编事实；生成成功后只报告文件名和必要的核对结果。"
	docxKindReport             = "report"
	docxKindNovel              = "novel"

	docxStageAudience  = "audience"
	docxStageOutline   = "outline"
	docxStageWorld     = "world"
	docxStageResearch1 = "research1"
	docxStageCritique  = "critique"
	docxStageResearch2 = "research2"
	docxStageWrite     = "write"
	docxStageRevise    = "revise"
	docxStageGenerate  = "generate"

	maxDocxNudges     = 6
	minNovelDocxChars = 400

	reportPipelineInstruction = "\n\n[报告流水线 · 产品强制]\n" +
		"写报告必须按序走完，禁止跳到 docx.gen 交特别简单的空稿：\n" +
		"1) 思考受众与目的：用 todo.write 记下读者、场景、要他们做什么。\n" +
		"2) 目录结构：摘要 / 背景与目的 / 事实与论据 / 分析 / 结论与建议 / 待办。可用 mermaid。\n" +
		"3) 论据与数据：必须 web.search，需要原文再 web.fetch，至少两轮。引用出处，禁止编造。\n" +
		"4) 再思考：对照检索结果补缺口，标「待确认」而不是编。\n" +
		"5) 完整章节：每一节都有论证，不是提纲。去AI味。\n" +
		"6) 最后生成：docx.gen（kind=report；heading/heading2/paragraph/quote；封面；桌面则 desktop=true）。空稿或无标题样式会被拒绝。\n" +
		"用户问进度时继续本流水线，不要重开一稿。\n"

	novelPipelineInstruction = "\n\n[小说流水线 · 产品强制]\n" +
		"写小说必须按序走完，禁止把三页提纲当成小说塞进 docx.gen：\n" +
		"1) 思考类型、人设、基调（todo.write）。\n" +
		"2) 大纲：起承转合，不是目录空壳。\n" +
		"3) 人物与世界观要点（欲望/恐惧/秘密、世界规则）。\n" +
		"4) 必要时 web.search/web.fetch 核对时代或设定细节。\n" +
		"5) 分章正文：每章可朗读的场景与对话。\n" +
		"6) 再修订文风与去AI味。\n" +
		"7) 最后生成：docx.gen（kind=novel；title；author 可省略会自动填；各章 Heading 1；桌面则 desktop=true）。\n" +
		"用户问进度时继续本流水线，不要重开一稿。\n"

	reportGenBlockedMsg = "ok:false\ndocx.gen 被流水线拦住：还没做完调研与章节撰写。先 web.search（必要时 web.fetch）至少两轮，写好摘要/背景/分析/结论/待办，再调用 docx.gen。空稿或无标题样式的文件会被拒绝。\n"
	novelGenBlockedMsg  = "ok:false\ndocx.gen 被流水线拦住：还没有大纲和分章正文。先写出起承转合大纲、人物与世界观，再写各章正文并修订文风，最后才 docx.gen。禁止把三页提纲当成小说。\n"
)

func spokenResultReportOnly(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if officeExplicitCreationRE.MatchString(t) {
		return false
	}
	for _, phrase := range []string{"一句话报告", "一句话汇报", "口头报告", "口头汇报", "报告执行结果", "报告播放结果", "报告操作结果", "汇报执行结果"} {
		if strings.Contains(t, phrase) {
			return true
		}
	}
	return false
}

func looksLikeReportTask(text string) bool {
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	// A spoken closeout is not a request to create a report document.
	if spokenResultReportOnly(t) {
		return false
	}
	if t == "" || officeExpertIntroduction(text) || looksLikeStatusFollowUp(t) || looksLikeResume(t) || looksLikePptTask(text) {
		return false
	}
	for _, k := range []string{
		"报告", "周报", "年报", "调研报告", "测试报告", "可行性报告", "总结报告",
		"写一份报告", "报告编写",
	} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func looksLikeNovelTask(text string) bool {
	if capabilityWorkTask(text) || officeMaterialReview(text) {
		return false
	}
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || officeExpertIntroduction(text) || looksLikeStatusFollowUp(t) || looksLikeResume(t) || looksLikePptTask(text) {
		return false
	}
	for _, k := range []string{"小说", "短篇", "长篇", "连载", "写个故事", "虚构", "小说编写", "起承转合", "星座", "爱情小说"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// Only this turn's selected identity counts. Memory, prior user turns and
// assistant answers can mention other specialists without mounting them.
func expertMountedIn(req llmadapter.Request, needles ...string) bool {
	selected := strings.Join(extractExpertRefNames(lastUserChatText(req.Messages)), "、")
	for _, m := range req.Messages {
		if m.Role != llmadapter.RoleSystem {
			continue
		}
		const prefix = "[稳定身份] 你就是「"
		if _, tail, ok := strings.Cut(m.Content, prefix); ok {
			if names, _, found := strings.Cut(tail, "」"); found {
				selected += "、" + names
			}
		}
	}
	for _, n := range needles {
		if strings.Contains(selected, n) {
			return true
		}
	}
	return false
}

func reportTaskFromRequest(req llmadapter.Request, goal string) bool {
	if capabilityWorkTask(goal) || officeMaterialReview(goal) {
		return false
	}
	if officeExpertIntroduction(goal) {
		return false
	}
	if looksLikeReportTask(goal) {
		return true
	}
	if !expertMountedIn(req, "报告编写专家", "report-writer") {
		return false
	}
	g := strings.TrimSpace(goal)
	if looksLikeStatusFollowUp(g) || looksLikeResume(g) || looksLikePptTask(g) {
		return false
	}
	for _, k := range []string{"写", "总结", "方案", "周报", "报告", "生成", "docx", "word", "大纲", "调研"} {
		if strings.Contains(strings.ToLower(g), k) {
			return true
		}
	}
	return false
}

func novelTaskFromRequest(req llmadapter.Request, goal string) bool {
	if capabilityWorkTask(goal) || officeMaterialReview(goal) {
		return false
	}
	if officeExpertIntroduction(goal) {
		return false
	}
	if looksLikeNovelTask(goal) {
		return true
	}
	if !expertMountedIn(req, "小说编写专家", "novel-writer") {
		return false
	}
	g := strings.TrimSpace(goal)
	if looksLikeStatusFollowUp(g) || looksLikeResume(g) || looksLikePptTask(g) {
		return false
	}
	for _, k := range []string{"写", "章", "人物", "故事", "小说", "生成", "docx", "大纲", "连载"} {
		if strings.Contains(strings.ToLower(g), k) {
			return true
		}
	}
	return false
}

func docxKindFromRequest(req llmadapter.Request, goal string) string {
	if target := officeGenToolForGoal(goal); target != "" && target != "docx.gen" {
		return ""
	}
	if looksLikePptTask(goal) {
		return ""
	}
	reportEx := expertMountedIn(req, "报告编写专家", "report-writer")
	novelEx := expertMountedIn(req, "小说编写专家", "novel-writer")
	if novelEx && !reportEx && novelTaskFromRequest(req, goal) {
		return docxKindNovel
	}
	if reportEx && !novelEx && reportTaskFromRequest(req, goal) {
		return docxKindReport
	}
	if looksLikeNovelTask(goal) {
		return docxKindNovel
	}
	if looksLikeReportTask(goal) {
		return docxKindReport
	}
	if novelTaskFromRequest(req, goal) {
		return docxKindNovel
	}
	if reportTaskFromRequest(req, goal) {
		return docxKindReport
	}
	return ""
}

func docxWebPasses(turn *chatTurnCheckpoint) int {
	if turn == nil {
		return 0
	}
	n := 0
	for _, name := range turn.DocxTools {
		if name == "web.search" || name == "web.fetch" {
			n++
		}
	}
	return n
}

func docxHasOutline(turn *chatTurnCheckpoint) bool {
	if turn == nil {
		return false
	}
	for _, name := range turn.DocxTools {
		if name == "todo.write" || name == "workspace.write" {
			return true
		}
	}
	return false
}

func noteDocxTools(turn *chatTurnCheckpoint, names []string) {
	if turn == nil || !turn.DocxActive {
		return
	}
	turn.DocxTools = append(turn.DocxTools, names...)
	turn.DocxStage = inferDocxStage(turn)
}

func noteDocxChars(turn *chatTurnCheckpoint, text string) {
	if turn == nil || !turn.DocxActive {
		return
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(text)); n > 0 {
		turn.DocxChars += n
	}
}

func inferDocxStage(turn *chatTurnCheckpoint) string {
	if turn == nil {
		return docxStageAudience
	}
	if turn.DocxGenerated {
		return docxStageGenerate
	}
	if lookupOptedOut(turn.Goal) || referenceOnlyOfficeTurn(turn.Goal) {
		return docxStageWrite
	}
	if turn.DocxKind == docxKindNovel {
		if turn.DocxChars >= minNovelDocxChars {
			return docxStageRevise
		}
		if docxHasOutline(turn) {
			return docxStageWorld
		}
		return docxStageOutline
	}
	web := docxWebPasses(turn)
	hasTodo := false
	for _, name := range turn.DocxTools {
		if name == "todo.write" {
			hasTodo = true
		}
	}
	switch {
	case web >= 2:
		return docxStageWrite
	case web == 1:
		return docxStageCritique
	case hasTodo:
		return docxStageOutline
	default:
		return docxStageAudience
	}
}

func nextDocxStage(turn *chatTurnCheckpoint) string {
	if turn != nil && turn.DocxKind == docxKindNovel {
		switch inferDocxStage(turn) {
		case docxStageAudience, docxStageOutline:
			return docxStageWorld
		case docxStageWorld:
			return docxStageResearch1
		case docxStageResearch1:
			return docxStageWrite
		case docxStageWrite:
			return docxStageRevise
		case docxStageRevise:
			return docxStageGenerate
		default:
			return docxStageGenerate
		}
	}
	switch inferDocxStage(turn) {
	case docxStageAudience:
		return docxStageOutline
	case docxStageOutline:
		return docxStageResearch1
	case docxStageResearch1:
		return docxStageCritique
	case docxStageCritique:
		return docxStageResearch2
	case docxStageResearch2:
		return docxStageWrite
	case docxStageWrite:
		return docxStageGenerate
	default:
		return docxStageGenerate
	}
}

func docxPipelineReady(turn *chatTurnCheckpoint) bool {
	if turn == nil {
		return false
	}
	if turn.DocxStage == docxStageGenerate || turn.DocxStage == docxStageWrite || turn.DocxStage == docxStageRevise {
		return true
	}
	if turn.DocxNudges >= 3 {
		return true
	}
	if turn.DocxKind == docxKindNovel {
		if turn.DocxChars >= minNovelDocxChars {
			return true
		}
		if !docxHasOutline(turn) {
			return false
		}
		return turn.DocxChars >= minNovelDocxChars || turn.DocxNudges >= 2
	}
	web := docxWebPasses(turn)
	if web >= 2 {
		return true
	}
	return web >= 1 && turn.DocxNudges >= 3
}

func docxGenBlocked(turn *chatTurnCheckpoint, name string) (bool, string) {
	if name != "docx.gen" || turn == nil || !turn.DocxActive {
		return false, ""
	}
	if docxPipelineReady(turn) || turn.DocxStage == docxStageGenerate || turn.DocxNudges >= 3 {
		return false, ""
	}
	if turn.DocxKind == docxKindNovel {
		return true, novelGenBlockedMsg
	}
	return true, reportGenBlockedMsg
}

func blockedDocxGenResult(msg string) toolruntime.Result {
	return toolruntime.Result{Output: msg}
}

func shouldContinueDocxTurn(turn *chatTurnCheckpoint, disableReasoning bool) bool {
	_ = disableReasoning
	if turn == nil || !turn.DocxActive {
		return false
	}
	if turn.DocxGenerated || turn.DocxNudges >= maxDocxNudges {
		return false
	}
	return true
}

func docxThinkingBanner(kind, stage string) string {
	if kind == docxKindNovel {
		switch stage {
		case docxStageWorld:
			return "【小说流程 · 人物与世界观】记下欲望、恐惧、秘密和世界规则。\n"
		case docxStageResearch1:
			return "【小说流程 · 核对设定】必要时检索时代或设定细节，不要编造。\n"
		case docxStageWrite:
			return "【小说流程 · 分章正文】写可朗读的场景与对话，不要交提纲。\n"
		case docxStageRevise:
			return "【小说流程 · 修订文风】去AI味，检查人设连续性和下场钩子。\n"
		case docxStageGenerate:
			return "【小说流程 · 最后生成】正文齐了，docx.gen（kind=novel，各章 Heading 1）。\n"
		default:
			return "【小说流程 · 思考】先定类型、人设、基调和起承转合，不要直接生成空稿。\n"
		}
	}
	switch stage {
	case docxStageOutline:
		return "【报告流程 · 目录结构】列出摘要/背景/分析/结论/待办，再动手检索。\n"
	case docxStageResearch1:
		return "【报告流程 · 论据与数据】正在从网上检索可引用的事实与出处。\n"
	case docxStageCritique:
		return "【报告流程 · 再思考】对照已搜集素材检查缺口，禁止编造。\n"
	case docxStageResearch2:
		return "【报告流程 · 再收集】第二轮检索，把缺口补上。\n"
	case docxStageWrite:
		return "【报告流程 · 完整章节】写摘要、背景、分析、结论与待办，每节都要有论证。\n"
	case docxStageGenerate:
		return "【报告流程 · 最后生成】章节齐了，docx.gen（kind=report，含封面与标题样式）。\n"
	default:
		return "【报告流程 · 思考】先弄清受众、目的和要他们做什么，不要直接生成空文件。\n"
	}
}

func docxStageNudge(kind, stage string) llmadapter.Message {
	if kind == docxKindNovel {
		return llmadapter.Message{Role: llmadapter.RoleSystem, Content: "继续小说流水线的下一步（" + stage + "）。" +
			"还没有合格 docx.gen 之前不要结束本轮。先有大纲和分章正文，禁止把提纲当小说。"}
	}
	return llmadapter.Message{Role: llmadapter.RoleSystem, Content: "继续报告流水线的下一步（" + stage + "）。" +
		"还没有合格 docx.gen 之前不要结束本轮。需要网上论据就调用 web.search / web.fetch。" +
		"禁止交无标题样式或特别简单的空稿。"}
}

func defaultNovelAuthor(e *Engine, goal string) string {
	if e != nil && e.identity != nil {
		nick := strings.TrimSpace(e.identity.Public().Nickname)
		if nick != "" && nick != identity.DefaultNickname {
			return nick
		}
	}
	_ = goal
	return officetools.DefaultNovelAuthor
}

func enrichDocxGenArgs(e *Engine, goal string, args json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return args
	}
	kind, _ := m["kind"].(string)
	if strings.ToLower(strings.TrimSpace(kind)) != docxKindNovel {
		return args
	}
	author, _ := m["author"].(string)
	if strings.TrimSpace(author) != "" {
		return args
	}
	m["author"] = defaultNovelAuthor(e, goal)
	raw, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return raw
}

func startDocxWorkflow(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) {
	if req == nil || turn == nil || req.DisableReasoning || turn.PptActive || turn.DocxActive {
		return
	}
	kind := docxKindFromRequest(*req, turn.Goal)
	if kind == "" {
		return
	}
	turn.DocxActive = true
	turn.DocxKind = kind
	if lookupOptedOut(turn.Goal) || referenceOnlyOfficeTurn(turn.Goal) {
		turn.DocxStage = docxStageWrite
		req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: referenceOfficeInstruction})
		return
	}
	turn.DocxStage = docxStageAudience
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: docxThinkingBanner(kind, docxStageAudience)}})
	instr := reportPipelineInstruction
	if kind == docxKindNovel {
		instr = novelPipelineInstruction
	}
	req.Messages = append(req.Messages, llmadapter.Message{Role: llmadapter.RoleSystem, Content: instr})
}

func nudgeDocxWorkflow(req *llmadapter.Request, turn *chatTurnCheckpoint, send func(bridge.Event) error) bool {
	if !shouldContinueDocxTurn(turn, req != nil && req.DisableReasoning) {
		return false
	}
	turn.DocxNudges++
	stage := nextDocxStage(turn)
	turn.DocxStage = stage
	kind := docxKindReport
	if turn != nil && turn.DocxKind != "" {
		kind = turn.DocxKind
	}
	_ = send(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: docxThinkingBanner(kind, stage)}})
	if req != nil {
		nudge := docxStageNudge(kind, stage)
		if lookupOptedOut(turn.Goal) || referenceOnlyOfficeTurn(turn.Goal) {
			nudge.Content = referenceOfficeInstruction
		}
		req.Messages = append(req.Messages, nudge)
	}
	return true
}
