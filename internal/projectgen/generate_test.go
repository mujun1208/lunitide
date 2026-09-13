package projectgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInterviewRoundTripAndRejectsLongValue(t *testing.T) {
	root := t.TempDir()
	_, err := SavePhase(root, "01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "guide", "2026-09-13T00:00:00Z", []Answer{
		{ID: "core_problem", Prompt: "本项目首先要解决什么？", Value: "商场进销存"},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Load(root)
	if err != nil || doc.Phases["1"].Answers[0].Value != "商场进销存" {
		t.Fatalf("load: %+v %v", doc, err)
	}
	if _, err = SavePhase(root, doc.ProjectID, 1, "nope", "", []Answer{{ID: "x", Value: "y"}}); err != ErrInterviewInvalid {
		t.Fatalf("mode: %v", err)
	}
	if _, err = SavePhase(root, doc.ProjectID, 1, "guide", "", []Answer{{ID: "x", Value: strings.Repeat("字", 2001)}}); err != ErrInterviewInvalid {
		t.Fatalf("long: %v", err)
	}
}

func TestRenderSkeletonContainsProjectAndRejectsApproved(t *testing.T) {
	out, err := Render(Input{
		ProjectName: "商场", ProjectCode: "ITM00001", DocumentType: "biz_req_analysis",
		Title: "业务需求分析报告", Answers: map[string]string{"core_problem": "进销存"},
	})
	if err != nil || !strings.Contains(out, "商场") || !strings.Contains(out, "进销存") || len(out) < MinGeneratedBytes {
		t.Fatalf("render: %q %v", out, err)
	}
	if _, err = Render(Input{DocumentType: "biz_req_analysis", AlreadyApproved: true}); err != ErrGenerateSkipApproved {
		t.Fatalf("approved: %v", err)
	}
	if _, err = os.Stat(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("unexpected")
	}
}

func TestRenderTemplateAndUnknownType(t *testing.T) {
	out, err := Render(Input{DocumentType: "biz_standard", ProjectName: "商场", TemplateBody: "# 规范 {{projectName}}\n\n至少三十二个字节的模版正文占位。"})
	if err != nil || !strings.Contains(out, "商场") {
		t.Fatalf("template: %q %v", out, err)
	}
	if _, err = Render(Input{DocumentType: "not_a_doc"}); err != ErrUnknownDocumentType {
		t.Fatalf("unknown: %v", err)
	}
}

func TestIsChecklistTypeIncludesJSONDocs(t *testing.T) {
	for _, key := range []string{"req_task_list", "biz_flow_list", "api_list", "feature_dev_list", "interface_list", "dev_checklist", "test_checklist", "integration_test_list"} {
		if !IsChecklistType(key) {
			t.Fatalf("missing checklist type %s", key)
		}
	}
	if IsChecklistType("biz_req_analysis") {
		t.Fatal("markdown doc marked checklist")
	}
}

func TestPhase1HasNineSkeletons(t *testing.T) {
	if got := PhaseDocumentTypes(1); len(got) != 9 {
		t.Fatalf("phase1=%d", len(got))
	}
	if got := PhaseDocumentTypes(2); len(got) != 10 {
		t.Fatalf("phase2=%d", len(got))
	}
	for _, key := range PhaseDocumentTypes(1) {
		if _, ok := Skeleton(key); !ok {
			t.Fatalf("missing skeleton %s", key)
		}
	}
}
