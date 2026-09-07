package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestOfficeGenToolForAllConversationSpecialists(t *testing.T) {
	cases := []struct {
		id   string
		goal string
		want string
	}{
		{"ppt-expert", "PPT专家做一份介绍PPT输出到桌面", "pptx.gen"},
		{"report-writer", "报告编写专家写一份调研报告Word到桌面", "docx.gen"},
		{"novel-writer", "小说编写专家写一份12星座爱情小说Word到桌面", "docx.gen"},
		{"excel-maker", "Excel表格制作专家做半年财报到桌面", "excel.gen"},
		{"ui-designer", "UI专家做个点球大战小游戏放到桌面", "html.gen"},
		{"pm-expert", "产品经理专家写一份PRD Word到桌面", "docx.gen"},
		{"architect-expert", "系统架构师专家画系统架构图", ""},
		{"db-expert", "数据库设计专家画ER图", ""},
		{"repo-expert", "系统项目结构规范专家画目录树", ""},
		{"standards-expert", "开发规范专家写AGENTS.md", ""},
		{"test-expert", "系统测试专家写测试计划", ""},
		{"hardware-expert", "硬件配置专家出一份BOM放到桌面", "excel.gen"},
		{"dev-expert", "开发专家改这段代码", ""},
		{"mro-expert", "航空机务专家出一份检查单Excel放到桌面", "excel.gen"},
		{"uas-airworthiness-expert", "低空适航专家出一份履历Word到桌面", "docx.gen"},
		{"tooling-chemical-expert", "航空工具化工品专家出一份校准清单Excel到桌面", "excel.gen"},
		{"parts-expert", "航空航材专家出一份询价Excel到桌面", "excel.gen"},
		{"mx-planning-expert", "航空维修计划专家出一份工作包Word到桌面", "docx.gen"},
	}
	if len(cases) != len(m8app.ConversationExpertIDs) {
		t.Fatalf("audit %d specialists, catalog has %d", len(cases), len(m8app.ConversationExpertIDs))
	}
	for _, c := range cases {
		got := officeGenToolForGoal(c.goal)
		if got != c.want {
			t.Fatalf("%s goal %q → %q, want %q", c.id, c.goal, got, c.want)
		}
		if c.want != "" && !wantsOfficeFileOnDesktop(c.goal) && c.id != "architect-expert" {
			if c.id == "ppt-expert" || c.id == "report-writer" || c.id == "novel-writer" || c.id == "excel-maker" || c.id == "ui-designer" || c.id == "pm-expert" || c.id == "hardware-expert" {
				if !wantsOfficeFileOnDesktop(c.goal) {
					t.Fatalf("%s should count as desktop office gen: %q", c.id, c.goal)
				}
			}
		}
	}
}

func TestOfficeGenNeverForMusicPlayOrWeather(t *testing.T) {
	for _, goal := range []string{"汽水音乐随便播放", "打开桌面上的协议文档", "在身份证号码后面写210404", "查天气"} {
		if officeGenToolForGoal(goal) != "" {
			t.Fatalf("computer-control %q must not map to a gen tool", goal)
		}
		if includeOfficeGenWorkflow(goal) {
			t.Fatalf("computer-control %q must not inject office-gen workflow", goal)
		}
	}
}

func TestFallbackOfficeGenArgsDesktopAndKind(t *testing.T) {
	novel := fallbackOfficeGenArgs("docx.gen", "写一份12星座爱情小说输出到桌面", "白羊座的人把戒指藏进袖口。")
	if !strings.Contains(string(novel), `"desktop":true`) || !strings.Contains(string(novel), `"kind":"novel"`) {
		t.Fatalf("novel args = %s", novel)
	}
	if !strings.Contains(string(novel), `"author":"佚名"`) {
		t.Fatalf("novel fallback must include default author: %s", novel)
	}
	report := fallbackOfficeGenArgs("docx.gen", "写一份调研报告放到桌面", "摘要")
	if !strings.Contains(string(report), `"kind":"report"`) || !strings.Contains(string(report), `"desktop":true`) {
		t.Fatalf("report args = %s", report)
	}
	ppt := fallbackOfficeGenArgs("pptx.gen", "做一份介绍PPT到桌面", officeFallbackProse)
	if !strings.Contains(string(ppt), `"desktop":true`) {
		t.Fatalf("ppt args = %s", ppt)
	}
	excel := fallbackOfficeGenArgs("excel.gen", "硬件BOM放到桌面", "| 项目 | 数量 |\n| --- | --- |\n| 主板 | 1 |")
	if !strings.Contains(string(excel), `"desktop":true`) {
		t.Fatalf("excel args = %s", excel)
	}
}

func TestShouldAutoOfficeGenOnIncompleteNovel(t *testing.T) {
	turn := &chatTurnCheckpoint{Goal: "写一份12星座爱情小说Word到桌面", DocxActive: true, DocxKind: docxKindNovel}
	if !shouldAutoOfficeGen(turn, errors.New("incomplete")) {
		t.Fatal("incomplete novel-to-desktop must auto docx.gen")
	}
	if officeGenToolForTurn(turn) != "docx.gen" {
		t.Fatalf("tool = %s", officeGenToolForTurn(turn))
	}
}

func TestOfficeGenFailNoticeFriendlyCause(t *testing.T) {
	got := officeGenFailNotice(errors.New("officetools: novel needs chapter Heading 1"))
	if strings.Contains(got, "officetools:") {
		t.Fatalf("raw error leaked: %q", got)
	}
	if !strings.Contains(got, "章节标题") {
		t.Fatalf("expected friendly chapter message: %q", got)
	}
}

func TestStripOfficeGenLectureNeverUserVisible(t *testing.T) {
	leaked := "无法执行。" + officeGenInternalHint
	if got := stripOfficeGenLecture(leaked); strings.Contains(got, "写到桌面请用") || strings.Contains(got, "*.gen") {
		t.Fatalf("strip left lecture: %q", got)
	}
	if userVisibleToolSummary("ok:false\n"+officeGenInternalHint) != "正在生成到桌面…" {
		t.Fatal("tool summary must hide the lecture")
	}
	ev := bridge.Event{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: leaked}}
	sanitizeOutgoingEvent(&ev)
	if strings.Contains(ev.Delta.Text, "写到桌面请用") || strings.Contains(ev.Delta.Text, "desktop=true") {
		t.Fatalf("delta still leaked: %q", ev.Delta.Text)
	}
	next, delta := appendAssistantNotice("```mermaid\nA-->B", "无法执行。"+officeGenInternalHint)
	if strings.Contains(next, "写到桌面请用") || strings.Contains(next, "desktop=true") || strings.Contains(next, "*.gen") {
		t.Fatalf("assistant notice leaked: next=%q delta=%q", next, delta)
	}
	if delta != "" && (strings.Contains(delta, "写到桌面请用") || strings.Contains(delta, "desktop=true")) {
		t.Fatalf("delta leaked lecture: %q", delta)
	}
}

func TestTurnFailureCauseNoGenLecture(t *testing.T) {
	err := errors.New("upstream")
	for _, goal := range []string{
		"写一份12星座爱情小说Word到桌面",
		"做一份介绍PPT到桌面",
		"写一份调研报告放到桌面",
		"半年财报表格放到桌面",
		"汽水音乐随便播放",
		"请系统架构师专家画系统架构图",
		"帮我把报告输出到桌面",
	} {
		got := turnFailureCause(err, goal, nil)
		if strings.Contains(got, officeGenInternalHint) || strings.Contains(got, "写到桌面请用") || strings.Contains(got, "不要用 command.run") {
			t.Fatalf("cause leaked for %q: %q", goal, got)
		}
		if strings.Contains(got, "desktop=true") || strings.Contains(got, "*.gen") {
			t.Fatalf("cause leaked tool hint for %q: %q", goal, got)
		}
	}
	if got := turnFailureCause(err, "汽水音乐随便播放", []string{"media.play"}); !strings.Contains(got, "这次操作没成功") {
		t.Fatalf("music control = %q", got)
	}
	if got := turnFailureCause(err, "写一份Word到桌面", nil); !strings.HasPrefix(got, "生成失败：") {
		t.Fatalf("plain desktop docx = %q", got)
	}
}

func TestOfficeWorkflowDoesNotReuseHistoricalExpert(t *testing.T) {
	req := llmadapter.Request{Messages: []llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "历史偏好：@PPT专家你在吗；报告编写专家、小说编写专家"},
		{Role: llmadapter.RoleUser, Content: "[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAV] 旧项目"},
		{Role: llmadapter.RoleAssistant, Content: "PPT专家已经完成"},
		{Role: llmadapter.RoleUser, Content: "[引用专家 AI工程师|01ARZ3NDEKTSV4RRFFQ69G5FAV] 介绍下自己能力"},
	}}
	for _, goal := range []string{"介绍下自己能力", "帮我分析下一步方案", "帮我写代码"} {
		if pptTaskFromRequest(req, goal) || reportTaskFromRequest(req, goal) || novelTaskFromRequest(req, goal) {
			t.Fatalf("historical identity started workflow for %q", goal)
		}
	}
	req.Messages[0].Content = "[专家装备]\n[稳定身份] 你就是「PPT专家」。"
	if !pptTaskFromRequest(req, "做一份产品介绍") {
		t.Fatal("current explicitly mounted expert must still work")
	}
	if pptTaskFromRequest(req, "介绍下自己能力") {
		t.Fatal("expert introduction is not a file request")
	}
}

func TestOfficeFallbackApprovalIsActionableAndHasArtifact(t *testing.T) {
	e := newArtifactEngine(t)
	turn := chatTurnCheckpoint{Goal: "做一份介绍PPT", PptActive: true, PptStage: pptStageGenerate, StreamID: "test"}
	var events []bridge.Event
	finished, notice := e.tryFinishOfficeGen(context.Background(), executionModeApproval, artifactSession, &turn, officeFallbackProse, nil, func(event bridge.Event) error { events = append(events, event); return nil })
	if finished || strings.Contains(notice, "生成失败") {
		t.Fatalf("finished=%v notice=%s", finished, notice)
	}
	var pending *bridge.ToolEvent
	for _, event := range events {
		if event.Tool != nil && len(event.Tool.ArgsDigest) != 64 {
			t.Fatalf("unusable tool digest: %+v", event.Tool)
		}
		if event.Type == bridge.EventApprovalRequired {
			pending = event.Tool
		}
	}
	if pending == nil {
		t.Fatalf("no approval: events=%+v notice=%s", events, notice)
	}
	// A real one-time decision creates the file; repeated decisions replay it.
	result, err := e.tools.DecideScoped(context.Background(), artifactSession, pending.CallID, pending.ArgsDigest, true, toolruntime.ApprovalScopeOnce)
	if err != nil || result.Artifact == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var args map[string]any
	_ = json.Unmarshal(fallbackOfficeGenArgs("pptx.gen", "做一份介绍PPT", officeFallbackProse), &args)
	if args["desktop"] != false {
		t.Fatal("workspace request unexpectedly writes to Desktop")
	}
}

func TestPoliteDocumentRequestsRemainCreationTasks(t *testing.T) {
	for _, goal := range []string{"你可以帮我做一份产品介绍 PPT 吗", "你可以帮我写调研报告吗", "介绍自己并做成 PPT", "[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAV] 请做一份介绍"} {
		if officeExpertIntroduction(goal) || officeGenToolForGoal(goal) == "" {
			t.Fatalf("creation suppressed: %q", goal)
		}
	}
}

func TestDeclinedDocumentTaskDoesNotOverrideExpertIntroduction(t *testing.T) {
	goal := "不要写报告，先介绍一下自己"
	if officeGenToolForGoal(goal) != "" || looksLikeReportTask(goal) || reportTaskFromRequest(llmadapter.Request{}, goal) {
		t.Fatal("declined report started workflow")
	}
}
