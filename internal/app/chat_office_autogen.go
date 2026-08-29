package app

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/oklog/ulid/v2"
)

var mermaidNodeRe = regexp.MustCompile(`\["([^"\]]+)"\]`)

func officeGenToolForGoal(goal string) string {
	if looksLikeArchitectMermaidTurn(goal) && !wantsOfficeFileOnDesktop(goal) {
		return ""
	}
	if looksLikePptTask(goal) {
		return "pptx.gen"
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
	if turn == nil {
		return ""
	}
	if turn.PptActive || looksLikePptTask(turn.Goal) {
		return "pptx.gen"
	}
	if turn.DocxActive || looksLikeReportTask(turn.Goal) || looksLikeNovelTask(turn.Goal) {
		return "docx.gen"
	}
	return officeGenToolForGoal(turn.Goal)
}

func shouldAutoOfficeGen(turn *chatTurnCheckpoint, streamErr error) bool {
	if turn == nil {
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
	if name == "html.gen" || name == "excel.gen" {
		return wantsOfficeFileOnDesktop(turn.Goal) || streamErr != nil
	}
	return wantsOfficeFileOnDesktop(turn.Goal) && streamErr != nil
}

func fallbackOfficeGenArgs(name, goal, assistant string) json.RawMessage {
	desktop := wantsOfficeFileOnDesktop(goal) || strings.Contains(goal, "桌面")
	switch name {
	case "pptx.gen":
		title := clipOfficeTitle(goal, "演示文稿")
		slides := slidesFromAssistant(goal, assistant)
		raw, _ := json.Marshal(map[string]any{
			"path": "介绍.pptx", "desktop": desktop || true, "title": title, "slides": slides,
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
		body := strings.TrimSpace(assistant)
		if utf8Len := len([]rune(body)); utf8Len > 1200 {
			body = string([]rune(body)[:1200])
		}
		if body == "" {
			body = title
		}
		payload := map[string]any{
			"path": "文档.docx", "desktop": desktop, "title": title, "kind": kind,
			"blocks": []map[string]any{
				{"type": "heading", "text": title},
				{"type": "paragraph", "text": body},
			},
		}
		if kind == docxKindNovel {
			payload["author"] = officetools.DefaultNovelAuthor
		}
		raw, _ := json.Marshal(payload)
		return raw
	case "excel.gen":
		raw, _ := json.Marshal(map[string]any{
			"path": "表格.xlsx", "desktop": desktop,
			"sheets": []map[string]any{{
				"name":    "汇总",
				"headers": []string{"项目", "说明"},
				"rows":    [][]any{{titleOrGoal(goal), "由对话要点生成"}},
			}},
		})
		return raw
	case "html.gen":
		raw, _ := json.Marshal(map[string]any{
			"path": "小游戏.html", "desktop": desktop, "template": "penalty-shootout",
			"title": clipOfficeTitle(goal, "小游戏"),
		})
		return raw
	default:
		return nil
	}
}

func titleOrGoal(goal string) string {
	return clipOfficeTitle(goal, "汇总")
}

func slidesFromAssistant(goal, text string) []map[string]any {
	var slides []map[string]any
	for _, m := range mermaidNodeRe.FindAllStringSubmatch(text, -1) {
		raw := strings.ReplaceAll(m[1], "<br/>", "\n")
		raw = strings.ReplaceAll(raw, "<br>", "\n")
		parts := strings.SplitN(raw, "\n", 2)
		title := strings.TrimSpace(parts[0])
		if title == "" {
			continue
		}
		slide := map[string]any{"title": title, "layout": "content", "bullets": []string{"详见介绍要点"}}
		if len(slides) == 0 {
			slide["layout"] = "title"
		}
		if len(parts) > 1 {
			if sub := strings.TrimSpace(parts[1]); sub != "" {
				slide["subtitle"] = sub
			}
		}
		slides = append(slides, slide)
		if len(slides) >= 12 {
			break
		}
	}
	if len(slides) == 0 {
		title := clipOfficeTitle(goal, "个人介绍")
		slides = []map[string]any{
			{"title": title, "layout": "title", "subtitle": "个人介绍"},
			{"title": "关于我", "layout": "content", "bullets": []string{"详见对话中的介绍要点"}},
		}
	}
	return slides
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

func (e *Engine) tryFinishOfficeGen(ctx context.Context, mode executionMode, sessionID string, turn *chatTurnCheckpoint, assistant string, streamErr error, send func(bridge.Event) error) (bool, string) {
	if e == nil || e.tools == nil || turn == nil {
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
	if name == "docx.gen" {
		args = enrichDocxGenArgs(e, turn.Goal, args)
	}
	if len(args) == 0 {
		return false, officeGenFailNotice(errOfficeGenEmpty)
	}
	callID := "auto-" + ulid.Make().String()
	if send != nil {
		_ = send(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, Summary: "正在生成文件"}})
	}
	r, err := e.executeUserTool(ctx, mode, sessionID, name, args)
	summary := r.Output
	if err != nil {
		summary = err.Error()
		if send != nil {
			_ = send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, Summary: clipToolSummary(summary)}})
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
	pathNotice := officeGenSuccessNotice(name, wantsOfficeFileOnDesktop(turn.Goal) || strings.Contains(turn.Goal, "桌面"))
	if strings.Contains(summary, "desktop/") || strings.Contains(summary, ".docx") || strings.Contains(summary, ".pptx") || strings.Contains(summary, ".xlsx") || strings.Contains(summary, ".html") {
		if clip := clipOfficeTitle(summary, ""); clip != "" && utf8RuneLen(summary) < 120 {
			pathNotice = pathNotice + " " + strings.TrimSpace(summary)
		}
	}
	if send != nil {
		_ = send(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: callID, Name: name, Summary: clipToolSummary(summary)}})
	}
	return true, pathNotice
}

func utf8RuneLen(s string) int {
	return len([]rune(s))
}

var errOfficeGenEmpty = errors.New("没有可写入的内容")
