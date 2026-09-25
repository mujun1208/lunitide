package producthub

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
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

func TestPassedProbeIsNotAWorkItem(t *testing.T) {
	found := TaskFindings([]TaskResult{{ID: "play", Title: "播放", Status: "pass", Evidence: "已播放 0.2 秒探测音"}})
	if len(found) != 1 || found[0].Fix != "这项已经通过，不用再处理。" || found[0].Status != "pass" {
		t.Fatalf("%#v", found)
	}
	miss := TaskFindings([]TaskResult{{ID: "dictate", Title: "听写", Status: "untested", Evidence: "voice model not installed: runtime"}})
	if !strings.Contains(miss[0].Fix, "精识别运行时") {
		t.Fatalf("%#v", miss[0].Fix)
	}
}

func TestClassifyLogDropsYesterdayAndKeepsTodaysSummaryFailure(t *testing.T) {
	logClock = func() time.Time { return time.Date(2026, 9, 25, 16, 30, 0, 0, time.Local) }
	t.Cleanup(func() { logClock = time.Now })
	got := ClassifyLog(strings.Join([]string{
		"2026/09/24 16:30:00 chat stream x failed: code=UPSTREAM_OUTCOME_UNKNOWN stage=connect",
		"2026/09/25 07:51:29 companion weekly archive: weekly companion archive did not complete: failed SUMMARY_FAILED",
	}, "\n"))
	for _, f := range got {
		if f.ErrorCode == "PH_L17" {
			t.Fatalf("yesterday's model miss stayed: %#v", f)
		}
	}
	var summary bool
	for _, f := range got {
		if f.ErrorCode == "PH_L18" && strings.Contains(f.Evidence, "SUMMARY_FAILED") {
			summary = true
		}
	}
	if !summary {
		t.Fatalf("today's summary failure missing: %#v", got)
	}
}

func TestLoadedFindingsKeepTheMeasuredScore(t *testing.T) {
	findings := []Finding{
		{ErrorCode: "PH_L01", Status: "open"},
		{ErrorCode: "PH_L02", Status: "pass"},
		{ErrorCode: "PH_L03", Status: "pass"},
		{ErrorCode: "PH_L04", Status: "pass"},
		{ErrorCode: "PH_L17", Status: "open"},
		{ErrorCode: "PH_L90", Status: "note"},
	}
	score, probe, live := displayedScore(Edition{}, findings, ProbeScore{Passed: 138, Total: 170})
	if !live || probe.Passed != 3 || probe.Total != 5 || score != 60 {
		t.Fatalf("score %d probe %+v live %v", score, probe, live)
	}
}

func TestPurifyMarksAProbeFixedOnlyAfterItPasses(t *testing.T) {
	s := New(&MemoryPersist{})
	fail := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "play", Title: "播放", Status: "fail", Evidence: "播放设备拒绝"}}, "", nil
	})
	if _, err := s.Generate(fail, "manual"); err != nil {
		t.Fatal(err)
	}
	pass := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "play", Title: "播放", Status: "pass", Evidence: "已播放 0.2 秒探测音"}}, "", nil
	})
	res, err := s.Apply(pass, "PH_L02", "probe.play")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied || res.Status != "fixed" || !strings.Contains(res.Plan, "已播放 0.2 秒探测音") || res.SkillOutput != "" {
		t.Fatalf("%#v", res)
	}
	ed, err := s.Latest(context.Background())
	if err != nil || ed == nil || ed.HealthScore != 100 {
		t.Fatalf("score after a passing recheck: err=%v ed=%+v", err, ed)
	}
}

func TestPurifyKeepsAProbeOpenWhenItStillFails(t *testing.T) {
	s := New(&MemoryPersist{})
	ctx := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "play", Title: "播放", Status: "fail", Evidence: "播放设备拒绝"}}, "", nil
	})
	if _, err := s.Generate(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	res, err := s.Apply(ctx, "PH_L02", "probe.play")
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Status != "open" || !strings.Contains(res.Plan, "探测音") {
		t.Fatalf("%#v", res)
	}
	ed, err := s.Latest(context.Background())
	if err != nil || ed == nil || ed.HealthScore != 0 {
		t.Fatalf("a failed recheck must stay at 0: err=%v score=%v", err, ed)
	}
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_L02" && (f.Status != "open" || !strings.Contains(f.Evidence, "播放设备拒绝")) {
			t.Fatalf("finding %#v", f)
		}
	}
}

func TestPurifyClearsALogLineOnlyWhenItIsGoneToday(t *testing.T) {
	logClock = func() time.Time { return time.Date(2026, 9, 25, 16, 30, 0, 0, time.Local) }
	t.Cleanup(func() { logClock = time.Now })
	line := "2026/09/25 07:51:29 companion weekly archive: weekly companion archive did not complete: failed SUMMARY_FAILED"
	s := New(&MemoryPersist{})
	ctx := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "ocr", Title: "图片识别", Status: "pass", Evidence: "读出 OCR"}}, line, nil
	})
	if _, err := s.Generate(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	still, err := s.Apply(WithEngineLog(context.Background(), line), "PH_L18", "log.PH_L18")
	if err != nil {
		t.Fatal(err)
	}
	if still.Applied || still.Status != "open" || !strings.Contains(still.Plan, "失败码") {
		t.Fatalf("still present %#v", still)
	}
	held, err := s.Latest(context.Background())
	if err != nil || held == nil {
		t.Fatal(err)
	}
	var kept bool
	for _, f := range held.Findings {
		if f.ErrorCode == "PH_L18" && strings.Contains(f.Evidence, "SUMMARY_FAILED") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("evidence lost: %#v", held.Findings)
	}
	gone, err := s.Apply(WithEngineLog(context.Background(), "2026/09/25 08:00:00 chat.start ok"), "PH_L18", "log.PH_L18")
	if err != nil {
		t.Fatal(err)
	}
	if !gone.Applied || gone.Status != "fixed" || !strings.Contains(gone.Plan, "不再占当前故障") {
		t.Fatalf("gone %#v", gone)
	}
}

func TestPurifyDoesNotScoreLandscapeNotes(t *testing.T) {
	s := New(&MemoryPersist{})
	ctx := WithLiveTasks(context.Background(), func(context.Context) ([]TaskResult, string, []LandscapeNote) {
		return []TaskResult{{ID: "ocr", Title: "图片识别", Status: "pass", Evidence: "读出 OCR"}}, "", nil
	})
	ed, err := s.Generate(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if ed.HealthScore != 100 {
		t.Fatalf("landscape must stay out of the score: %d", ed.HealthScore)
	}
	res, err := s.Apply(ctx, "PH_L90", "landscape.none")
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.Status == "fixed" || !strings.Contains(res.Plan, "图景页") {
		t.Fatalf("%#v", res)
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
