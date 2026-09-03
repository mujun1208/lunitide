package app

import (
	"errors"
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
	ppt := fallbackOfficeGenArgs("pptx.gen", "做一份介绍PPT到桌面", "")
	if !strings.Contains(string(ppt), `"desktop":true`) {
		t.Fatalf("ppt args = %s", ppt)
	}
	excel := fallbackOfficeGenArgs("excel.gen", "硬件BOM放到桌面", "")
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
