package producthub

import (
	"strings"
	"testing"
)

func TestRecheckTracesHandlersTimesProbesAndComparesSavedLandscape(t *testing.T) {
	if FindProductRoot() == "" {
		t.Fatal("product root missing")
	}
	ed := Edition{
		Features: []Card{
			{
				StableKey: "feature.office.studio.task-create", Name: "创建办公任务", Domain: "office", Module: "office",
				ChainClass: "crud-bridge", Chain: defaultChain("crud-bridge", "创建办公任务"),
				Methods:  []Method{{Type: "menu", Entry: "office"}},
				Scaffold: Scaffold{Bridge: []string{"office.task.create"}},
			},
			{
				StableKey: "feature.office.studio.missing", Name: "缺失任务", Domain: "office", Module: "office",
				ChainClass: "crud-bridge", Chain: defaultChain("crud-bridge", "缺失任务"),
				Scaffold: Scaffold{Bridge: []string{"office.missing.run"}},
			},
			{
				StableKey: "feature.dialog.music.play", Name: "播放音乐", Domain: "dialog", Module: "music",
				ChainClass: "media-transport", Chain: defaultChain("media-transport", "播放音乐"),
				Scaffold: Scaffold{Bridge: []string{"media.play"}},
			},
			{
				StableKey: "feature.office.studio.blank", Name: "空白任务", Domain: "office", Module: "office",
			},
		},
		LiveProbe: ProbeScore{Passed: 1, Total: 4},
		Findings: []Finding{
			{Severity: "error", ErrorCode: "PH_L02", StableKey: "probe.play", Title: "播放", Status: "open", Evidence: "耗时 9s：设备拒绝"},
			{Severity: "info", ErrorCode: "PH_L91", StableKey: "landscape.Cursor", Title: "对照 Cursor", Status: "note", Evidence: "Cursor：媒体核验=weak 不是媒体产品（公开说明 2026-09-21）"},
		},
	}
	md, _ := RenderReport(ed)
	for _, want := range []string{
		"office.task.create → handleOfficeStudio",
		"方法分支在",
		"office.missing.run",
		"没有处理函数",
		"任务完成不了",
		"超过探测预算",
		"不是媒体产品",
		"不编对手分数",
		"先修这次的播放",
		"requireIdempotency",
		"CreateOfficeTask",
		"executeMediaPlayWithCC",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %s", want)
		}
	}
	calls := between(md, "### 真实调用", "### 速度与效率")
	play := lineContaining(calls, "media.play")
	if play == "" || strings.Contains(play, "没有处理函数") {
		t.Fatalf("runtime tool was treated as unwired: %s", play)
	}
	if strings.Contains(md, "排名第") || strings.Contains(md, "100分") {
		t.Fatal("report invented a rank or a perfect score")
	}
	for _, banned := range []string{"本轮没有单独实测", "后面的步骤没有从源码追完", "带副作用的命令没有逐条执行"} {
		if strings.Contains(md, banned) {
			t.Fatalf("report still hedges: %s", banned)
		}
	}
	tasks := between(md, "### 任务完成", "### 宕机")
	blank := lineContaining(tasks, "空白任务")
	if !strings.Contains(blank, "没有入口") || !strings.Contains(blank, "任务完成不了") {
		t.Fatalf("a task with no entry was left unmeasured: %s", blank)
	}
	create := sentenceContaining(tasks, "创建办公任务")
	if strings.Contains(create, "处理函数已挂上") || strings.Contains(create, "已跑完") {
		t.Fatalf("an unrun task was called finished: %s", create)
	}
	if !strings.Contains(create, "尚未跑完") {
		t.Fatalf("an unrun task was not marked unfinished: %s", create)
	}
	if strings.Contains(md, "只按这四项") {
		t.Fatal("accuracy still counts only the four probes")
	}
	ran := ed
	ran.Findings = append(append([]Finding{}, ed.Findings...), Finding{
		Severity: "info", ErrorCode: "PH_L00", StableKey: "probe.office.task.create", Title: "创建办公任务", Status: "pass",
		Evidence: "已跑完：读回标题「诊断探测」",
	})
	ranMD, _ := RenderReport(ran)
	ranTask := sentenceContaining(between(ranMD, "### 任务完成", "### 宕机"), "创建办公任务")
	if !strings.Contains(ranTask, "已跑完") || strings.Contains(ranTask, "尚未跑完") {
		t.Fatalf("a finished run was not recorded: %s", ranTask)
	}
	ran.Findings[len(ran.Findings)-1].Status = "open"
	ran.Findings[len(ran.Findings)-1].Evidence = "创建返回 FEATURE_DISABLED"
	failedMD, _ := RenderReport(ran)
	failedTask := sentenceContaining(between(failedMD, "### 任务完成", "### 宕机"), "创建办公任务")
	if strings.Contains(failedTask, "已跑完") || !strings.Contains(failedTask, "任务完成不了") {
		t.Fatalf("a failed run was still called finished: %s", failedTask)
	}

	ed.Findings[0].Evidence = "已播放 0.2 秒探测音，耗时 100ms"
	ed.Findings[0].Status = "pass"
	ed.Findings[0].Severity = "info"
	fixed, _ := RenderReport(ed)
	if strings.Contains(fixed, "超过探测预算") || strings.Contains(fixed, "先修这次的播放") {
		t.Fatal("a probe that now passes is still reported slow or still queued for repair")
	}
	if !strings.Contains(fixed, "在探测预算") {
		t.Fatal("a fast probe was not recorded")
	}
}

func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	text = text[i:]
	j := strings.Index(text[len(start):], end)
	if j < 0 {
		return text
	}
	return text[:len(start)+j]
}

func sentenceContaining(text, needle string) string {
	for _, part := range strings.Split(text, "。") {
		if strings.Contains(part, needle) {
			return part
		}
	}
	return ""
}

func lineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
