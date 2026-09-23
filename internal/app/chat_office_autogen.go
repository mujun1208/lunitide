package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

// Specialist labels are equipment, not an instruction to create a document.
var officeExpertRefRE = regexp.MustCompile(`\[引用专家[^\]]+\]`)
var officeExplicitCreationRE = regexp.MustCompile(`(?:制作|做|生成|创建|写|输出|保存|做成).{0,40}(?:ppt|word|docx|excel|xlsx|pdf|html|报告|周报|年报|小说|演示|表格)`)

// Match output instructions, not format names in source attachments or roles.
var officeOutputFormatRE = regexp.MustCompile(`(?i)(?:生成|制作|输出|导出|保存为|另存为|转成|转换成|做成|写成|做一个|做一份|做个|做份|create\b|generate\b|export\b|save as\b|convert to\b)[^，。；;\n]{0,24}?(pdf|docx|word|pptx|ppt|xlsx|excel|html)`)
var officeFormatTokenRE = regexp.MustCompile(`(?i)pptx|docx|xlsx|pdf|word|ppt|excel|html`)
var officeFormatChoiceRE = regexp.MustCompile(`还是|或者|或是|\bor\b`)

func officeOutputFormatTools(goal string) []string {
	var tools []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(chatRoutingText(goal), func(r rune) bool { return strings.ContainsRune("，,。；;\n", r) }) {
		lower := strings.ToLower(part)
		negated := false
		for _, word := range []string{"不要", "不用", "无需", "不需要", "别", "怎么", "如何", "how to", "how do", "without"} {
			negated = negated || strings.Contains(lower, word)
		}
		if negated {
			continue
		}
		add := func(token string) {
			next := map[string]string{"pdf": "pdf.gen", "docx": "docx.gen", "word": "docx.gen", "pptx": "pptx.gen", "ppt": "pptx.gen", "xlsx": "excel.gen", "excel": "excel.gen", "html": "html.gen"}[strings.ToLower(token)]
			if next == "" || seen[next] {
				return
			}
			seen[next] = true
			tools = append(tools, next)
		}
		for _, match := range officeOutputFormatRE.FindAllStringSubmatch(part, -1) {
			add(match[1])
		}
		if officeFormatChoiceRE.MatchString(lower) {
			for _, token := range officeFormatTokenRE.FindAllString(lower, -1) {
				add(token)
			}
		}
	}
	return tools
}

func explicitOfficeOutputTool(goal string) string {
	tools := officeOutputFormatTools(goal)
	if len(tools) != 1 {
		return ""
	}
	return tools[0]
}

func officeOutputFormatAmbiguous(goal string) bool {
	return len(officeOutputFormatTools(goal)) > 1
}

func officeExpertIntroduction(goal string) bool {
	text := strings.ToLower(officeExpertRefRE.ReplaceAllString(goal, ""))
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("，,。；;", r) }) {
		negated := false
		for _, word := range []string{"不要", "别做", "不用", "无需", "先不", "不需要"} {
			if strings.Contains(part, word) {
				negated = true
				break
			}
		}
		if !negated && officeExplicitCreationRE.MatchString(part) {
			return false
		}
	}
	for _, phrase := range []string{"介绍自己", "介绍下自己", "介绍一下自己", "介绍你的能力", "介绍下你的", "你能做什么", "你可以做什么", "你可以帮我什么", "你可以帮我做什么", "你在吗", "你是谁"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// officeToolMisroutedToCode is true when a code/POC turn is being sent through
// an Office generator. "开始干 / 落盘" after a Python POC must write and run the
// source file, not call office.generate.
func officeToolMisroutedToCode(name, goal string, messages []llmadapter.Message, args json.RawMessage) bool {
	switch name {
	case "office.generate", "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen":
	default:
		return false
	}
	if officeGenerateTargetsCode(args) {
		return true
	}
	if runnableSystemRequest(goal) {
		return true
	}
	if wantsOfficeGen(goal) {
		return false
	}
	blob := strings.ToLower(goal)
	seen := 0
	for i := len(messages) - 1; i >= 0 && seen < 8; i-- {
		role := messages[i].Role
		if role != llmadapter.RoleUser && role != llmadapter.RoleAssistant {
			continue
		}
		blob += "\n" + strings.ToLower(messages[i].Content)
		seen++
	}
	code := strings.Contains(blob, ".py") || strings.Contains(blob, "python") || strings.Contains(blob, "poc") || strings.Contains(blob, "源码")
	act := strings.Contains(blob, "落盘") || strings.Contains(blob, "开始干") || strings.Contains(blob, "运行") || strings.Contains(blob, "跑一下") || strings.Contains(blob, "跑起来")
	return code && act
}

// confineSessionArtifactArgs keeps a generated file in this conversation's
// folder unless the user explicitly asked for the Desktop.
func confineSessionArtifactArgs(goal, name string, args json.RawMessage) json.RawMessage {
	switch name {
	case "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "html.gen":
	default:
		return args
	}
	if wantsOfficeFileOnDesktop(goal) {
		return args
	}
	var m map[string]any
	if len(args) == 0 || json.Unmarshal(args, &m) != nil || m == nil {
		return args
	}
	changed := false
	if desktop, _ := m["desktop"].(bool); desktop {
		m["desktop"] = false
		changed = true
	}
	if catalog, _ := m["catalog"].(bool); catalog {
		if !changed {
			return args
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return args
		}
		return raw
	}
	path, _ := m["path"].(string)
	path = strings.TrimSpace(path)
	lower := strings.ToLower(filepath.ToSlash(path))
	needsName := path == "" || strings.Contains(lower, "desktop") || strings.Contains(path, "桌面") || filepath.IsAbs(path) || filepath.VolumeName(path) != ""
	if needsName {
		base := filepath.Base(path)
		baseLower := strings.ToLower(base)
		if base == "." || base == "" || strings.Contains(baseLower, "desktop") || strings.Contains(base, "桌面") {
			base = defaultSessionArtifactName(name)
		}
		if base != path {
			m["path"] = base
			changed = true
		}
	}
	if !changed {
		return args
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return raw
}

func defaultSessionArtifactName(tool string) string {
	switch tool {
	case "excel.gen":
		return "workbook.xlsx"
	case "docx.gen":
		return "document.docx"
	case "pptx.gen":
		return "deck.pptx"
	case "pdf.gen":
		return "document.pdf"
	default:
		return "preview.html"
	}
}

func officeGenerateTargetsCode(args json.RawMessage) bool {
	var p struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &p) != nil {
		return false
	}
	for _, item := range []string{p.Name, p.Path} {
		switch strings.ToLower(filepath.Ext(item)) {
		case ".py", ".go", ".ts", ".js", ".sh", ".ps1":
			return true
		}
	}
	return false
}

func officeGenToolForGoal(goal string) string {
	if refusesOfficeGen(goal) || spokenResultReportOnly(goal) {
		return ""
	}
	if looksLikeSkillAuthoringTask(goal) || looksLikeExpertAuthoringTask(goal) || officeMaterialReview(goal) || officeHowToQuestion(goal) {
		return ""
	}
	if runnableSystemRequest(goal) {
		return ""
	}
	if capabilityWorkTask(goal) && !wantsOfficeDeliverableDuringTrial(goal) {
		return ""
	}
	if officeExpertIntroduction(goal) {
		return ""
	}
	if looksLikeArchitectMermaidTurn(goal) && !wantsOfficeFileOnDesktop(goal) {
		return ""
	}
	if tool := explicitOfficeOutputTool(goal); tool != "" {
		return tool
	}
	if officeOutputFormatAmbiguous(goal) {
		return ""
	}
	if looksLikePptTask(goal) {
		return "pptx.gen"
	}
	if looksLikePdfTask(goal) {
		return "pdf.gen"
	}
	if looksLikeNovelTask(goal) || looksLikeReportTask(goal) {
		return "docx.gen"
	}
	if looksLikeExcelTask(goal) || looksLikeHardwareBom(goal) {
		return "excel.gen"
	}
	if looksLikeHtmlGenTask(goal) {
		return "html.gen"
	}
	if !wantsOfficeFileOnDesktop(goal) && !wantsOfficeGen(goal) {
		return ""
	}
	g := strings.ToLower(goal)
	switch {
	case strings.Contains(g, "pdf"):
		return "pdf.gen"
	case strings.Contains(g, "ppt") || strings.Contains(g, "pptx") || strings.Contains(g, "幻灯") || strings.Contains(g, "演示"):
		return "pptx.gen"
	case strings.Contains(g, "docx") || strings.Contains(g, "word") || strings.Contains(g, "报告") || strings.Contains(g, "小说") || strings.Contains(g, "prd") || strings.Contains(g, "brd"):
		return "docx.gen"
	case strings.Contains(g, "xlsx") || strings.Contains(g, "excel") || strings.Contains(g, "表格") || strings.Contains(g, "bom"):
		return "excel.gen"
	case strings.Contains(g, "html") || strings.Contains(g, "小游戏"):
		return "html.gen"
	default:
		return ""
	}
}

func officeGenToolForTurn(turn *chatTurnCheckpoint) string {
	if turn == nil || officeMaterialReview(turn.Goal) {
		return ""
	}
	if runnableSystemRequest(turn.Goal) {
		return ""
	}
	if (turn.CapabilityWork || capabilityWorkTask(turn.Goal)) && !wantsOfficeDeliverableDuringTrial(turn.Goal) {
		return ""
	}
	if tool := explicitOfficeOutputTool(turn.Goal); tool != "" {
		return tool
	}
	if turn.PptActive || looksLikePptTask(turn.Goal) {
		return "pptx.gen"
	}
	if turn.DocxActive || looksLikeReportTask(turn.Goal) || looksLikeNovelTask(turn.Goal) {
		return "docx.gen"
	}
	return officeGenToolForGoal(turn.Goal)
}

func skipOfficeAutogen(ctx context.Context, sessionID, goal string) bool {
	if officeTaskContextID(ctx) != "" {
		return true
	}
	if !skillTrialsActive(ctx, sessionID) {
		return false
	}
	return looksLikeSkillAuthoringTask(goal) || looksLikeExpertAuthoringTask(goal)
}

func wantsOfficeDeliverableDuringTrial(goal string) bool {
	if looksLikeSkillAuthoringTask(goal) || looksLikeExpertAuthoringTask(goal) || runnableSystemRequest(goal) {
		return false
	}
	t := strings.ToLower(capabilityRequestBody(goal))
	if strings.Contains(t, "生成") && (strings.Contains(t, "周报") || strings.Contains(t, "报告") || strings.Contains(t, "ppt") || strings.Contains(t, "演示")) {
		return true
	}
	for _, n := range []string{"写周报", "写一份周报", "做周报", "出周报", "写一份报告", "做一份"} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return false
}

func shouldAutoOfficeGen(turn *chatTurnCheckpoint, streamErr error) bool {
	if turn == nil || errors.Is(streamErr, errSkillContextBudget) || runnableSystemRequest(turn.Goal) {
		return false
	}
	if turn.PptGenerated || turn.DocxGenerated {
		return false
	}
	name := officeGenToolForTurn(turn)
	if name == "" {
		return false
	}
	if streamErr != nil || usedCommandRun(turn.LastTools) {
		return true
	}
	if name == "pptx.gen" && (pptPipelineReady(turn) || turn.PptStage == pptStageGenerate || turn.PptStage == pptStageWrite || turn.PptNudges >= 3) {
		return !turn.PptGenerated
	}
	if name == "docx.gen" && (docxPipelineReady(turn) || turn.DocxNudges >= 3) {
		return !turn.DocxGenerated
	}
	if name == "html.gen" || name == "excel.gen" || name == "pdf.gen" {
		return wantsOfficeFileOnDesktop(turn.Goal) || streamErr != nil
	}
	return wantsOfficeFileOnDesktop(turn.Goal) && streamErr != nil
}

func fallbackOfficeGenArgs(name, goal, assistant string) json.RawMessage {
	desktop := wantsOfficeFileOnDesktop(goal)
	switch name {
	case "pdf.gen":
		if !officeContentUsable(assistant) {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{
			"path": "文档.pdf", "desktop": desktop, "title": clipOfficeTitle(goal, "文档"), "body": assistant,
		})
		return raw
	case "pptx.gen":
		title := clipOfficeTitle(goal, "演示文稿")
		slides := officeContentSlides(goal, assistant)
		if len(slides) == 0 {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{
			"path": "介绍.pptx", "desktop": desktop, "title": title, "slides": slides,
		})
		return raw
	case "docx.gen":
		title := clipOfficeTitle(goal, "文档")
		kind := "document"
		if looksLikeNovelTask(goal) {
			kind = docxKindNovel
		} else if looksLikeReportTask(goal) {
			kind = docxKindReport
		}
		blocks := officeDocxBlocks(title, assistant)
		if len(blocks) == 0 {
			return nil
		}
		payload := map[string]any{
			"path": "文档.docx", "desktop": desktop, "title": title, "kind": kind,
			"blocks": blocks,
		}
		if kind == docxKindNovel {
			payload["author"] = officetools.DefaultNovelAuthor
		}
		raw, _ := json.Marshal(payload)
		return raw
	case "excel.gen":
		sheets := officeContentSheets(assistant)
		if len(sheets) == 0 {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{
			"path": "表格.xlsx", "desktop": desktop, "sheets": sheets,
		})
		return raw
	case "html.gen":
		template, title, path := "penalty-shootout", "小游戏", "小游戏.html"
		g := strings.ToLower(goal)
		if strings.Contains(goal, "清单") || strings.Contains(g, "checklist") || strings.Contains(goal, "待办页") {
			template, title, path = "checklist", "清单", "清单.html"
		} else if strings.Contains(goal, "计时") || strings.Contains(g, "timer") {
			template, title, path = "timer", "计时器", "计时器.html"
		}
		raw, _ := json.Marshal(map[string]any{
			"path": path, "desktop": desktop, "template": template,
			"title": clipOfficeTitle(goal, title),
		})
		return raw
	default:
		return nil
	}
}

func officeGenSuccessNotice(name string, desktop bool) string {
	where := "工作区"
	if desktop {
		where = "桌面"
	}
	switch name {
	case "pptx.gen":
		return "已生成 PPT，并写到" + where + "。"
	case "docx.gen":
		return "已生成文档，并写到" + where + "。"
	case "excel.gen":
		return "已生成表格，并写到" + where + "。"
	case "html.gen":
		return "已生成页面，并写到" + where + "。"
	default:
		return "已生成文件，并写到" + where + "。"
	}
}

func officeGenFailNotice(err error) string {
	why := "生成未完成"
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		msg = strings.TrimPrefix(msg, "ok:false")
		msg = strings.TrimSpace(msg)
		if friendly := friendlyOfficeGenCause(msg); friendly != "" {
			why = friendly
		} else if msg != "" && !strings.Contains(msg, officeGenInternalHint) && !strings.Contains(msg, "desktop=true") && !strings.HasPrefix(msg, "officetools:") {
			why = msg
		}
	}
	if utf8Len := len([]rune(why)); utf8Len > 80 {
		why = string([]rune(why)[:80])
	}
	return "生成失败：" + why
}

func friendlyOfficeGenCause(msg string) string {
	msg = strings.TrimPrefix(msg, "officetools: ")
	switch {
	case strings.Contains(msg, "novel needs chapter Heading 1"):
		return "小说缺少章节标题，请按章使用一级标题"
	case strings.Contains(msg, "novel is an outline dump"):
		return "小说正文太短或像提纲，请写完整分章正文"
	case strings.Contains(msg, "report needs section headings"):
		return "报告缺少章节标题或正文太短"
	case strings.Contains(msg, "document needs Heading"):
		return "文档缺少标题样式，请使用 heading/heading2"
	case strings.Contains(msg, "document body is trivial"), strings.Contains(msg, "empty or trivial"):
		return "文档正文为空或太短"
	case strings.Contains(msg, "document title is required"):
		return "缺少文档标题"
	case strings.Contains(msg, "没有可写入的内容"):
		return "没有可写入的内容"
	default:
		return ""
	}
}

func (e *Engine) tryFinishOfficeGen(ctx context.Context, mode executionMode, sessionID string, turn *chatTurnCheckpoint, assistant string, streamErr error, send func(bridge.Event) error, companion ...bool) (bool, string) {
	if e == nil || e.tools == nil || turn == nil || ctx.Err() != nil || errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, errSkillContextBudget) {
		return false, ""
	}
	if skipOfficeAutogen(ctx, sessionID, turn.Goal) {
		return false, ""
	}
	if !shouldAutoOfficeGen(turn, streamErr) && !usedCommandRun(turn.LastTools) {
		return false, ""
	}
	name := officeGenToolForTurn(turn)
	if name == "" {
		return false, ""
	}
	if name == "pptx.gen" && turn.PptActive {
		turn.PptStage = pptStageGenerate
	}
	args := fallbackOfficeGenArgs(name, turn.Goal, assistant)
	if name == "docx.gen" && len(args) > 0 {
		args = enrichDocxGenArgs(e, turn.Goal, args)
	}
	if len(args) == 0 {
		return false, officeGenFailNotice(errOfficeGenEmpty)
	}
	callID := "auto-" + ulid.Make().String()
	digest := argsDigestOrFallback(name, args)
	if send != nil {
		_ = send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: "正在生成文件"}})
	}
	r, err := e.executeUserTool(ctx, mode, sessionID, name, args)
	if errors.Is(err, toolruntime.ErrApprovalRequired) {
		preapproved := conversationGrantsTool(mode, name) || (len(companion) > 0 && companion[0] && companionToolPreapproved(name, e.fullDiskChat(mode), e.companionCcEnabled(ctx)))
		if unattended(ctx) && !preapproved {
			err = errors.New(unattendedApprovalDenial(name))
		} else if _, prepErr := e.tools.Prepare(ctx, turn.StreamID, sessionID, callID, name, args, toolruntime.Mode(mode), 10*time.Minute); prepErr != nil {
			err = prepErr
		} else if preapproved {
			r, err = e.tools.DecideScoped(ctx, sessionID, callID, digest, true, toolruntime.ApprovalScopeOnce)
			if err == nil {
				e.persistApprovedToolResult(ctx, sessionID, callID, digest, r)
			}
		} else {
			if send != nil {
				if sendErr := send(bridge.Event{Type: bridge.EventApprovalRequired, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: approvalRequiredSummary(name, args)}}); sendErr != nil {
					return false, officeGenFailNotice(sendErr)
				}
			}
			return false, "请确认文件生成操作，确认后继续。"
		}
	}
	summary := r.Output
	if err != nil {
		summary = err.Error()
		if send != nil {
			_ = send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(summary)}})
		}
		return false, officeGenFailNotice(err)
	}
	turn.LastTools = append(turn.LastTools, name)
	if name == "pptx.gen" {
		turn.PptGenerated = true
		turn.PptStage = pptStageGenerate
	}
	if name == "docx.gen" {
		turn.DocxGenerated = true
	}
	pathNotice := officeGenSuccessNotice(name, wantsOfficeFileOnDesktop(turn.Goal))
	if strings.Contains(summary, "desktop/") || strings.Contains(summary, ".docx") || strings.Contains(summary, ".pptx") || strings.Contains(summary, ".xlsx") || strings.Contains(summary, ".html") {
		if clip := clipOfficeTitle(summary, ""); clip != "" && utf8RuneLen(summary) < 120 {
			pathNotice = pathNotice + " " + strings.TrimSpace(summary)
		}
	}
	if send != nil {
		event := &bridge.ToolEvent{CallID: callID, Name: name, ArgsDigest: digest, Summary: clipToolSummary(summary)}
		if r.Artifact != nil && artifactKindValid(r.Artifact.Kind) {
			event.Artifact = &bridge.ArtifactEvent{Kind: r.Artifact.Kind, Path: r.Artifact.Path}
		}
		_ = send(bridge.Event{Type: bridge.EventToolCompleted, Tool: event})
	}
	return true, pathNotice
}

func utf8RuneLen(s string) int {
	return len([]rune(s))
}

var errOfficeGenEmpty = errors.New("没有可完整写入的正文或表格；尚未生成文件，请继续补齐实际内容后再生成")
