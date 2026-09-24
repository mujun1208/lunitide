package producthub

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestLiveScoreCountsUntestedAndLogFaults(t *testing.T) {
	tasks := []TaskResult{
		{ID: "dictate", Title: "听写", Status: "pass", Evidence: "识别引擎已跑完"},
		{ID: "play", Title: "播放", Status: "fail", Evidence: "播放设备拒绝"},
		{ID: "download", Title: "下载", Status: "untested", Evidence: "没有网络，未收到长度"},
		{ID: "ocr", Title: "图片识别", Status: "pass", Evidence: "读出 OCR"},
	}
	faults := ClassifyLog("voice: download : expected 349418188 bytes, server offered 349906910\n")
	probe, score := ScoreLive(tasks, faults)
	if probe.Passed != 2 || probe.Total != 5 || score != 40 {
		t.Fatalf("probe %+v score %d", probe, score)
	}
	found := TaskFindings(tasks)
	var hit bool
	for _, f := range found {
		if f.ErrorCode == "PH_L02" && strings.Contains(f.Evidence, "播放设备拒绝") && f.Status == "open" {
			hit = true
		}
		if f.Status == "pass" && f.Severity == "error" {
			t.Fatalf("a pass must not look like an error: %#v", f)
		}
	}
	if !hit {
		t.Fatalf("findings %#v", found)
	}
}

func TestClassifyLogQuotesRealFaultsOnly(t *testing.T) {
	logText := strings.Join([]string{
		"chat.start assembling explicit turn after durable assembly failed: timeout once",
		"voice: download : expected 349418188 bytes, server offered 349906910",
		"panic: runtime error: nil pointer",
		"bridge internal failure method=chat.start code=ENGINE_INTERNAL_ERROR",
		"bridge internal failure method=chat.start code=ENGINE_INTERNAL_ERROR",
		"bridge internal failure method=chat.start code=ENGINE_INTERNAL_ERROR",
		"chat preturn path=wait session=abc waited=context deadline exceeded",
		"无法将类型 System.__ComObject 转换为 IAsyncOperation",
		"工具停在无法执行",
	}, "\n")
	got := ClassifyLog(logText)
	want := map[string]string{
		"PH_L10": "349418188",
		"PH_L11": "panic:",
		"PH_L12": "chat.start",
		"PH_L13": "deadline exceeded",
		"PH_L14": "IAsyncOperation",
		"PH_L15": "无法执行",
	}
	seen := map[string]Finding{}
	for _, f := range got {
		seen[f.ErrorCode] = f
	}
	for code, needle := range want {
		f, ok := seen[code]
		if !ok || !strings.Contains(f.Evidence, needle) {
			t.Fatalf("%s missing in %#v", code, got)
		}
	}
	for _, f := range got {
		if strings.Contains(f.Evidence, "assembling explicit turn") {
			t.Fatalf("benign line became a finding: %#v", f)
		}
	}
}

func TestLandscapeWithoutAPickDoesNotChangeTheScore(t *testing.T) {
	notes := LandscapeFindings(nil)
	if len(notes) != 1 || notes[0].ErrorCode != "PH_L90" || !strings.Contains(notes[0].Evidence, "尚未") {
		t.Fatalf("notes %#v", notes)
	}
	probe, score := ScoreLive([]TaskResult{{ID: "ocr", Status: "pass"}}, nil)
	if score != 100 || probe.Total != 1 {
		t.Fatalf("landscape must stay out of the score: %+v %d", probe, score)
	}
	picked := LandscapeFindings([]LandscapeNote{{
		Name: "Cursor", Axis: "技能 / MCP", Score: "strong", Note: "规则 / MCP / skills 是公开主路径。", Source: "公开说明", Date: "2026-09-21",
	}})
	if len(picked) != 1 || picked[0].ErrorCode != "PH_L91" || !strings.Contains(picked[0].Evidence, "Cursor") || !strings.Contains(picked[0].Evidence, "2026-09-21") {
		t.Fatalf("picked %#v", picked)
	}
}

func TestManualRefreshKeepsTheLiveScore(t *testing.T) {
	s := New(&MemoryPersist{})
	ctx := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "play", Title: "播放", Status: "fail", Evidence: "播放设备拒绝"}},
			"panic: runtime error: nil pointer\n", nil
	})
	ed, err := s.Generate(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if ed.HealthScore != 0 || ed.LiveProbe.Passed != 0 || ed.LiveProbe.Total != 2 {
		t.Fatalf("edition score %d probe %+v", ed.HealthScore, ed.LiveProbe)
	}
	ov, err := s.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.HealthScore != 0 || ov.ProbePassed != 0 || ov.ProbeTotal != 2 || !ov.LiveChecked {
		t.Fatalf("overview %+v", ov)
	}
	findings, md, _, err := s.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var play, crash, pending bool
	for _, f := range findings {
		switch f.ErrorCode {
		case "PH_L02":
			play = strings.Contains(f.Evidence, "播放设备拒绝")
		case "PH_L11":
			crash = strings.Contains(f.Evidence, "panic:")
		case "PH_L90":
			pending = true
		}
	}
	if !play || !crash || !pending || !strings.Contains(md, "实测") {
		t.Fatalf("play=%v crash=%v pending=%v md has 实测=%v", play, crash, pending, strings.Contains(md, "实测"))
	}
}

func TestDefaultLiveRunOnce(t *testing.T) {
	if os.Getenv("LUNITIDE_LIVE_PROBE") == "" {
		t.Skip()
	}
	tasks, logText := DefaultLiveRun(context.Background())
	if len(tasks) != 4 {
		t.Fatalf("tasks %#v", tasks)
	}
	for _, task := range tasks {
		if task.Status != "pass" && task.Status != "fail" && task.Status != "untested" {
			t.Fatalf("status %#v", task)
		}
		if strings.TrimSpace(task.Evidence) == "" {
			t.Fatalf("empty evidence %#v", task)
		}
		t.Logf("%s %s %s", task.ID, task.Status, task.Evidence)
	}
	faults := ClassifyLog(logText)
	for _, f := range faults {
		t.Logf("fault %s %s", f.ErrorCode, f.Evidence)
	}
}
