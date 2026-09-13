package projecttask

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
)

func TestParseKeepsMixedAttachmentsAndParseStrictRejects(t *testing.T) {
	md := []byte("# 接口清单\n这不是 JSON，但仍可能被绑成附件。")
	doc, err := Parse(md)
	if err != nil || len(doc.Items) != 0 || doc.Version != 1 {
		t.Fatalf("mixed parse: %+v %v", doc, err)
	}
	if _, err = ParseStrict(md); err != projectapp.ErrBoardSourceInvalid {
		t.Fatalf("strict: %v", err)
	}
	ok, err := ParseStrict([]byte(`{"version":1,"items":[{"id":"I001","title":"登录","status":"pending"}]}`))
	if err != nil || len(ok.Items) != 1 || ok.Items[0].ID != "I001" {
		t.Fatalf("strict json: %+v %v", ok, err)
	}
}

func TestOpenReportCompleteAndReturnLoop(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "pending", Acceptance: "能登录"}}}
	brief, err := Open(&dev, "F001", `D:\work\mall`, "src", project.ExecutorLunitide)
	if err != nil || !strings.Contains(brief.Text, "F001") || !strings.Contains(brief.Text, "D:\\work\\mall") {
		t.Fatalf("brief: %+v %v", brief, err)
	}
	if dev.Items[0].Status != "in_progress" {
		t.Fatalf("status %q", dev.Items[0].Status)
	}
	if err = Report(&dev, "F001", "写了登录", project.ExecutorLunitide, ""); err != nil {
		t.Fatal(err)
	}
	test := Doc{}
	dev.Items[0].SelfTestPass = true
	if err = Complete(&dev, &test, "F001"); err != nil || dev.Items[0].Status != "dev_done" {
		t.Fatalf("complete: %+v %v", dev, err)
	}
	if err = DevComplete(dev); err != nil {
		t.Fatal(err)
	}
	test = BuildTestItems(dev)
	if len(test.Items) != 1 || test.Items[0].SourceID != "F001" {
		t.Fatalf("test items: %+v", test)
	}
	if err = ReturnFromTest(&dev, &test, test.Items[0].ID, ""); err != projectapp.ErrTestReasonRequired {
		t.Fatalf("empty reason: %v", err)
	}
	if err = ReturnFromTest(&dev, &test, test.Items[0].ID, "登录失败"); err != nil {
		t.Fatal(err)
	}
	if test.Items[0].Status != "test_fail" || dev.Items[0].Status != "in_progress" || dev.Items[0].TestReturn == nil {
		t.Fatalf("return: dev=%+v test=%+v", dev.Items[0], test.Items[0])
	}
	brief, err = Open(&dev, "F001", `D:\work\mall`, "src", project.ExecutorCursor)
	if err != nil || !strings.Contains(brief.Text, "登录失败") {
		t.Fatalf("reopen brief: %+v %v", brief, err)
	}
	_ = Report(&dev, "F001", "已修", project.ExecutorCursor, "thread1")
	dev.Items[0].SelfTestPass = true
	if err = Complete(&dev, &test, "F001"); err != nil {
		t.Fatal(err)
	}
	if test.Items[0].Status != "pending" {
		t.Fatalf("retest status %q", test.Items[0].Status)
	}
}

func TestBriefOrderSameForAllExecutors(t *testing.T) {
	for _, exec := range []project.Executor{project.ExecutorLunitide, project.ExecutorCursor, project.ExecutorCodex} {
		dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "pending", Acceptance: "能登录", TestReturn: &TestReturn{ID: "T-F001", Reason: "登录失败", At: "2026-09-13T00:00:00Z"}}}}
		brief, err := Open(&dev, "F001", `D:\work\mall`, "src", exec)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(brief.Text, "\n")
		if len(lines) < 6 || !strings.HasPrefix(lines[0], "任务 F001") || !strings.HasPrefix(lines[1], "验收：") || !strings.HasPrefix(lines[2], "目标路径：") || !strings.HasPrefix(lines[3], "项目根：") || !strings.HasPrefix(lines[4], "测试退回") || !strings.Contains(lines[len(lines)-1], "不要 git") {
			t.Fatalf("%s brief order: %q", exec, brief.Text)
		}
	}
}

func TestCompleteRejectsWithoutSelfTest(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "in_progress", LastResultSummary: "写了"}}}
	if err := Complete(&dev, &Doc{}, "F001"); err != projectapp.ErrSelfTestRequired {
		t.Fatalf("got %v", err)
	}
}

func TestCompleteRejectsPendingWithoutSummary(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "pending"}}}
	if err := Complete(&dev, &Doc{}, "F001"); err != projectapp.ErrDevIncomplete {
		t.Fatalf("got %v", err)
	}
}

func TestCompleteRejectsInProgressWithoutSummary(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "in_progress", SelfTestPass: true}}}
	if err := Complete(&dev, &Doc{}, "F001"); err != projectapp.ErrDevIncomplete {
		t.Fatalf("got %v", err)
	}
}

func TestReturnFromTestRequiresSource(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "dev_done"}}}
	test := Doc{Version: 1, Items: []Item{{ID: "T001", Title: "测", Status: "pending"}}}
	if err := ReturnFromTest(&dev, &test, "T001", "失败"); err != projectapp.ErrTestNoSource {
		t.Fatalf("got %v", err)
	}
}

func TestReportDoesNotChangeStatus(t *testing.T) {
	dev := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "登录", Status: "in_progress"}}}
	if err := Report(&dev, "F001", "写了登录", project.ExecutorLunitide, ""); err != nil {
		t.Fatal(err)
	}
	if dev.Items[0].Status != "in_progress" || dev.Items[0].LastResultSummary != "写了登录" {
		t.Fatalf("%+v", dev.Items[0])
	}
}

func TestImportMissingDedupes(t *testing.T) {
	src := Doc{Version: 1, Items: []Item{{ID: "F001", Title: "A", Status: "pending"}, {ID: "F002", Title: "B", Status: "pending"}}}
	dst := ImportMissing(Doc{Version: 1, Items: []Item{{ID: "F001", Title: "A", Status: "in_progress"}}}, src)
	if len(dst.Items) != 2 || dst.Items[0].Status != "in_progress" {
		t.Fatalf("%+v", dst)
	}
}
