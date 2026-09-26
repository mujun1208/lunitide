package producthub

import (
	"strings"
	"testing"
)

func TestFiveRoundsReplaceUnrunWithAFinishedVerdict(t *testing.T) {
	base := Edition{Features: []Card{{
		StableKey: "feature.office.studio.task-create", Name: "创建办公任务", Domain: "office", Module: "office",
		ChainClass: "crud-bridge", Chain: defaultChain("crud-bridge", "创建办公任务"),
		Methods:  []Method{{Type: "menu", Entry: "office"}},
		Scaffold: Scaffold{Bridge: []string{"office.task.create"}},
	}}}
	reached := base
	reached.Findings = []Finding{{
		Severity: "info", ErrorCode: "PH_L00", StableKey: "probe.office.task.create", Title: "创建办公任务", Status: "pass",
		Evidence: "入口已跑到：BRIDGE_SCHEMA_INVALID",
	}}
	md, _ := RenderReport(reached)
	if strings.Contains(md, "三种都算查完") {
		t.Fatal("close-out still treats three classes as done")
	}
	if !strings.Contains(md, "已跑完只表示临时库里的读回") || !strings.Contains(md, "模板步骤不能当成已经走通") {
		t.Fatal("accurate close-out missing")
	}
	line := sentenceContaining(between(md, "### 任务完成", "### 宕机"), "创建办公任务")
	if !strings.Contains(line, "入口已跑到") || strings.Contains(line, "已跑完") || strings.Contains(line, "尚未跑完") {
		t.Fatalf("round 1: reached entry was not a finished check: %s", line)
	}
	skipped := base
	skipped.Features[0].Scaffold.Bridge = []string{"computer.control"}
	skipped.Findings = []Finding{{
		Severity: "info", ErrorCode: "PH_L00", StableKey: "probe.computer.control", Title: "电脑控制", Status: "pass",
		Evidence: "不代跑：会改本机",
	}}
	md, _ = RenderReport(skipped)
	line = sentenceContaining(between(md, "### 任务完成", "### 宕机"), "创建办公任务")
	if !strings.Contains(line, "不代跑") || strings.Contains(line, "已跑完") || strings.Contains(line, "尚未跑完") {
		t.Fatalf("round 5: skipped entry was treated as finished or still unrun: %s", line)
	}
}

func TestFifteenRoundsKeepMediaAliasAndPickerVerdictsFinished(t *testing.T) {
	// Module office so isTaskCard keeps these lines in ### 任务完成.
	base := Edition{Features: []Card{{
		StableKey: "feature.office.media.play", Name: "媒体中心播放", Domain: "office", Module: "office",
		ChainClass: "media-transport", Chain: defaultChain("media-transport", "媒体中心播放"),
		Methods:  []Method{{Type: "menu", Entry: "media"}},
		Scaffold: Scaffold{Bridge: []string{"media.play"}},
	}, {
		StableKey: "feature.office.people.pick-file", Name: "选择本地文件", Domain: "office", Module: "office",
		ChainClass: "file-open", Chain: defaultChain("file-open", "选择本地文件"),
		Methods:  []Method{{Type: "menu", Entry: "people"}},
		Scaffold: Scaffold{Bridge: []string{"people.file.pick"}},
	}}}
	base.Findings = []Finding{
		{Severity: "info", ErrorCode: "PH_L00", StableKey: "probe.media.play", Title: "媒体中心播放", Status: "pass", Evidence: "入口已跑到：BRIDGE_METHOD_NOT_ALLOWED"},
		{Severity: "info", ErrorCode: "PH_L00", StableKey: "probe.people.file.pick", Title: "选择本地文件", Status: "pass", Evidence: "不代跑：会打开文件选择框"},
	}
	md, _ := RenderReport(base)
	section := between(md, "### 任务完成", "### 宕机")
	play := sentenceContaining(section, "媒体中心播放")
	if !strings.Contains(play, "入口已跑到") || strings.Contains(play, "尚未跑完") {
		t.Fatalf("round 15 media alias must stay finished: %s", play)
	}
	pick := sentenceContaining(section, "选择本地文件")
	if !strings.Contains(pick, "不代跑") || strings.Contains(pick, "已跑完") || strings.Contains(pick, "尚未跑完") {
		t.Fatalf("round 20 picker skip must stay finished without pretending完: %s", pick)
	}
}
