package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestLooksLikeReportAndNovelTasks(t *testing.T) {
	if !looksLikeReportTask("请报告编写专家写一份调研报告") || !looksLikeReportTask("帮我写周报") {
		t.Fatal("report asks must start the pipeline")
	}
	if !looksLikeNovelTask("请小说编写专家写个短篇") || !looksLikeNovelTask("帮我写小说") {
		t.Fatal("novel asks must start the pipeline")
	}
	if looksLikeReportTask("做好了没有") || looksLikeNovelTask("还要多久") {
		t.Fatal("status follow-ups are not a new Word job")
	}
	if looksLikeReportTask("做一份介绍 PPT") || looksLikeNovelTask("打开桌面协议") {
		t.Fatal("PPT and unrelated tasks must not start the docx pipeline")
	}
}

func TestReportDocxGenBlockedUntilResearch(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindReport}
	if blocked, msg := docxGenBlocked(turn, "docx.gen"); !blocked || !strings.Contains(msg, "拦住") {
		t.Fatalf("empty report pipeline must block docx.gen: %v %q", blocked, msg)
	}
	if blocked, _ := docxGenBlocked(turn, "web.search"); blocked {
		t.Fatal("research tools must not be blocked")
	}
	noteDocxTools(turn, []string{"web.search"})
	if docxPipelineReady(turn) {
		t.Fatal("one search is not enough")
	}
	noteDocxTools(turn, []string{"web.fetch"})
	if !docxPipelineReady(turn) {
		t.Fatal("two web passes must unlock docx.gen")
	}
	if blocked, _ := docxGenBlocked(turn, "docx.gen"); blocked {
		t.Fatal("ready report pipeline still blocked")
	}
}

func TestNovelDocxGenBlockedUntilOutlineAndProse(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindNovel}
	if blocked, msg := docxGenBlocked(turn, "docx.gen"); !blocked || !strings.Contains(msg, "提纲") {
		t.Fatalf("empty novel pipeline must block: %v %q", blocked, msg)
	}
	noteDocxTools(turn, []string{"todo.write"})
	if docxPipelineReady(turn) {
		t.Fatal("outline without chapter text is not enough")
	}
	turn.DocxChars = minNovelDocxChars
	if !docxPipelineReady(turn) {
		t.Fatal("outline + substantial chapter text must unlock docx.gen")
	}
	if blocked, _ := docxGenBlocked(turn, "docx.gen"); blocked {
		t.Fatal("ready novel pipeline still blocked")
	}
}

func TestNovelTwelveStoriesUnlocksAfterNudges(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindNovel, DocxNudges: 3, Goal: "写一份12星座爱情小说Word到桌面"}
	if !docxPipelineReady(turn) {
		t.Fatal("12 short stories must unlock after last-stage nudges, not wait forever")
	}
	if blocked, _ := docxGenBlocked(turn, "docx.gen"); blocked {
		t.Fatal("last-stage novel gen must run")
	}
}

func TestDocxStageNudgeVisibleInThinking(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindReport}
	req := gateway.Request{Model: "m"}
	var thinking []string
	if !shouldContinueDocxTurn(turn, false) {
		t.Fatal("active report turn must keep going until docx.gen")
	}
	ok := nudgeDocxWorkflow(&req, turn, func(event bridge.Event) error {
		if event.Thinking != nil {
			thinking = append(thinking, event.Thinking.Text)
		}
		return nil
	})
	if !ok || turn.DocxNudges != 1 || len(thinking) == 0 {
		t.Fatalf("nudge = %v nudges=%d thinking=%v", ok, turn.DocxNudges, thinking)
	}
	if !strings.Contains(strings.Join(thinking, ""), "报告流程") {
		t.Fatalf("stage must be visible in 思考: %v", thinking)
	}
	turn.DocxGenerated = true
	if shouldContinueDocxTurn(turn, false) {
		t.Fatal("finished docx.gen must stop nudging")
	}
}

func TestStartDocxWorkflowInjectsReportPipeline(t *testing.T) {
	req := gateway.Request{Model: "m", Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "写一份调研报告"}}}
	turn := &chatTurnCheckpoint{Goal: "写一份调研报告"}
	var banners []string
	startDocxWorkflow(&req, turn, func(event bridge.Event) error {
		if event.Thinking != nil {
			banners = append(banners, event.Thinking.Text)
		}
		return nil
	})
	if !turn.DocxActive || turn.DocxKind != docxKindReport || !strings.Contains(req.Messages[len(req.Messages)-1].Content, "报告流水线") {
		t.Fatalf("report pipeline not injected: active=%v kind=%s msgs=%v", turn.DocxActive, turn.DocxKind, req.Messages)
	}
	if len(banners) == 0 || !strings.Contains(banners[0], "思考") {
		t.Fatalf("audience stage missing from 思考: %v", banners)
	}
	startDocxWorkflow(&req, turn, func(bridge.Event) error { return nil })
	count := 0
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "报告流水线") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pipeline injected twice: %d", count)
	}
}

func TestStartDocxWorkflowInjectsNovelPipeline(t *testing.T) {
	req := gateway.Request{Model: "m", Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "请小说编写专家写个短篇"}}}
	turn := &chatTurnCheckpoint{Goal: "请小说编写专家写个短篇"}
	startDocxWorkflow(&req, turn, func(bridge.Event) error { return nil })
	if !turn.DocxActive || turn.DocxKind != docxKindNovel {
		t.Fatalf("novel pipeline not injected: %+v", turn)
	}
	if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "起承转合") {
		t.Fatalf("novel instruction missing: %v", req.Messages)
	}
}

func TestStartDocxWorkflowSkipsWhenPptActive(t *testing.T) {
	req := gateway.Request{Model: "m", Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "写一份调研报告"}}}
	turn := &chatTurnCheckpoint{Goal: "写一份调研报告", PptActive: true}
	startDocxWorkflow(&req, turn, func(bridge.Event) error { return nil })
	if turn.DocxActive {
		t.Fatal("docx pipeline must not steal an active PPT job")
	}
}

func TestBlockedDocxGenResult(t *testing.T) {
	r := blockedDocxGenResult(reportGenBlockedMsg)
	if r.Output == "" || !strings.Contains(r.Output, "ok:false") {
		t.Fatalf("blocked result = %#v", r)
	}
	var _ toolruntime.Result = r
}

func TestNoteDocxCharsCountsProse(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindNovel}
	noteDocxChars(turn, "夜色压上码头，潮水先碰到石阶。")
	if turn.DocxChars < 10 {
		t.Fatalf("chars = %d", turn.DocxChars)
	}
}
