package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/producthub"
)

func TestCatalogProbeFiveRounds(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	if !strings.Contains(byID["office.task.create"].Evidence, "已跑完") {
		t.Fatalf("round create: %+v", byID["office.task.create"])
	}
	for _, id := range []string{"system.health", "office.task.list", "office.task.get", "automation.job.set"} {
		item := byID[id]
		kind := probeEvidenceKind(item.Evidence, item.Status)
		if kind == "已跑完" && item.Status != "pass" {
			t.Fatalf("round 2-4 %s: kind 已跑完 but status %q: %+v", id, item.Status, item)
		}
		if kind == "入口已跑到" && item.Status != "reached" {
			t.Fatalf("round 2-4 %s: kind 入口已跑到 but status %q: %+v", id, item.Status, item)
		}
		if kind == "不代跑" && item.Status != "skipped" {
			t.Fatalf("round 2-4 %s: kind 不代跑 but status %q: %+v", id, item.Status, item)
		}
		if kind == "尚未跑完" || kind == "失败" {
			t.Fatalf("round 2-4 %s: %+v", id, item)
		}
	}
	for _, id := range []string{"br.navigate", "people.file.open", "people.thread.send", "people.file.pick", "meetings.start", "agentHub.task.start", "agentHub.file.open", "computer.control", "provider.test"} {
		ev := byID[id].Evidence
		if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") {
			t.Fatalf("round 72 %s: %+v", id, byID[id])
		}
	}
}

func TestCatalogProbeRunsOfficeTaskCreate(t *testing.T) {
	got := runOfficeTaskCreate(context.Background())
	if got.Status != "pass" || !strings.Contains(got.Evidence, "已跑完") || !strings.Contains(got.Evidence, "诊断探测") {
		t.Fatalf("office task was not run to completion: %+v", got)
	}
}

func TestCatalogProbeRound6OCRAndReview(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "入口已跑到", "ocr.routing.get", "mcp.security.review")
}

func TestCatalogProbeRound7PluginToggle(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "plugin.toggle")
}

func TestCatalogProbeRound8MemoryCreate(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "memory.create")
}

func TestCatalogProbeRound9ProviderCreate(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "provider.create")
}

func TestCatalogProbeRound10SessionUpdate(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "session.update")
}

func TestCatalogProbeRound11SessionExpertsSet(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "session.experts.set")
}

func TestCatalogProbeRound12SessionDelete(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "session.delete")
}

func TestCatalogProbeRound13MessageAppend(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "message.append")
}

func TestCatalogProbeRound14SkillInvoke(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "skill.invoke")
}

func TestCatalogProbeRound15MediaTransportA(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "入口已跑到", "media.play", "media.pause")
}

func TestCatalogProbeRound16MediaTransportB(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "入口已跑到", "media.stop", "media.toggle", "media.next", "media.previous")
}

func TestCatalogProbeRound17MediaTransportC(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "入口已跑到", "media.seek", "media.set_volume", "media.mute", "media.unmute")
}

func TestCatalogProbeRound18MediaQueue(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "入口已跑到", "media.create", "media.clear", "media.move", "media.remove", "media.jump")
}

func TestCatalogProbeRound19MediaAssetOpen(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "media.asset.open")
}

func TestCatalogProbeRound20SkipDangerous(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "people.file.pick", "agentHub.task.start")
}

func TestCatalogProbeCoversAllLiveCatalogBridges(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	var still []string
	counts := map[string]int{}
	for _, id := range liveCatalogBridgeIDs() {
		item, ok := byID[id]
		if !ok {
			still = append(still, id+" (missing)")
			counts["尚未跑完"]++
			continue
		}
		kind := probeEvidenceKind(item.Evidence, item.Status)
		counts[kind]++
		if kind == "尚未跑完" {
			still = append(still, id)
		}
	}
	t.Logf("counts 已跑完=%d 入口已跑到=%d 不代跑=%d 失败=%d 尚未跑完=%d",
		counts["已跑完"], counts["入口已跑到"], counts["不代跑"], counts["失败"], counts["尚未跑完"])
	if len(still) > 0 {
		t.Fatalf("catalog bridges still unfinished: %v", still)
	}
}

func TestCatalogProbeRound21ProviderCreateCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "provider.create")
}

func TestCatalogProbeRound22SessionUpdateCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "session.update")
}

func TestCatalogProbeRound23MessageSearchCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "message.search")
}

func TestCatalogProbeRound24OCRRoutingGetCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "ocr.routing.get")
}

func TestCatalogProbeRound25MemoryCreateCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "memory.create")
}

func TestCatalogProbeRound26MemorySearchCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "memory.search")
}

func TestCatalogProbeRound27SessionDeleteCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "session.delete")
}

func TestCatalogProbeRound28SessionExpertsSetCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "session.experts.set")
}

func TestCatalogProbeRound29AutomationJobSetCompletes(t *testing.T) {
	got := runCatalogProbes(context.Background())
	for _, item := range got {
		if item.ID != "automation.job.set" {
			continue
		}
		if item.Status != "pass" || !strings.Contains(item.Evidence, "诊断探测任务") || strings.Contains(item.Evidence, "「」") {
			t.Fatalf("%+v", item)
		}
		return
	}
	t.Fatal("automation.job.set missing")
}

func TestCatalogProbeRound30AutomationJobTriggerCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "automation.job.trigger")
}

func TestCatalogProbeRound31OfficeArtifactExportCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "office.artifact.export")
}

func TestCatalogProbeRound32MeetingsSummarySourceCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "meetings.summary.source.get")
}

func TestCatalogProbeRound33MROManualRegisterCompletes(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "mro.manual.register")
}

func TestCatalogProbeRound34MediaAssetOpenReachedOrCompletes(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	item := byID["media.asset.open"]
	if !strings.HasPrefix(item.Evidence, "已跑完") || !strings.Contains(item.Evidence, "读回") {
		t.Fatalf("media.asset.open want readback: %+v", item)
	}
}

func TestCatalogProbeRound35MediaAliasesNoteNotWhitelisted(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	for _, id := range []string{"media.play", "media.pause", "media.create"} {
		ev := byID[id].Evidence
		if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") {
			t.Fatalf("%s want 已跑完 readback: %+v", id, byID[id])
		}
	}
}

func TestCatalogProbeRound36AllMediaAliasesDocumented(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	ids := []string{
		"media.stop", "media.toggle", "media.next", "media.previous",
		"media.seek", "media.set_volume", "media.mute", "media.unmute",
		"media.clear", "media.move", "media.remove", "media.jump",
	}
	for _, id := range ids {
		ev := byID[id].Evidence
		if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") {
			t.Fatalf("%s: %+v", id, byID[id])
		}
	}
}

func TestCatalogProbeRound37MROPlanPublishReached(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	item := byID["mro.plan.publish"]
	if !strings.HasPrefix(item.Evidence, "已跑完") || !strings.Contains(item.Evidence, "读回") {
		t.Fatalf("mro.plan.publish want readback: %+v", item)
	}
}

func TestCatalogProbeRound38PluginToggleReached(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "", "plugin.toggle")
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	ev := byID["plugin.toggle"].Evidence
	if strings.HasPrefix(ev, "已跑完") {
		return
	}
	if !strings.HasPrefix(ev, "入口已跑到") {
		t.Fatalf("plugin.toggle: %+v", byID["plugin.toggle"])
	}
}

func TestCatalogProbeRound39MCPSecurityReviewReached(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "mcp.security.review")
}

func TestCatalogProbeRound40SkillInvokeReached(t *testing.T) {
	assertProbeFinished(t, runCatalogProbes(context.Background()), "已跑完", "skill.invoke")
}

func TestCatalogProbeRound41SpecificSkipReasons(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	ev := byID["provider.test"].Evidence
	if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") {
		t.Fatalf("provider.test: %+v", byID["provider.test"])
	}
}

func TestCatalogProbeRounds42to54DangerousSkipReasons(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	cases := []struct {
		round      int
		id, needle string
	}{
		{42, "computer.control", "读回"},
		{43, "desktop.files.readChunk", "读回"},
		{44, "agentHub.file.open", "读回"},
		{45, "agent.run.start", "读回"},
		{46, "people.file.open", "读回"},
		{47, "people.thread.send", "读回"},
		{48, "skill.package.upload.commit", "读回"},
		{49, "people.file.pick", "读回"},
		{50, "agentHub.task.start", "读回"},
		{51, "computer.control", "读回"},
		{52, "br.navigate", "读回"},
		{53, "meetings.start", "读回"},
		{54, "provider.test", "读回"},
		{71, "appUpdate.check", "读回"},
	}
	for _, tc := range cases {
		ev := byID[tc.id].Evidence
		if tc.id == "desktop.files.readChunk" || tc.id == "skill.package.upload.commit" || tc.id == "agent.run.start" || tc.id == "appUpdate.check" || tc.id == "people.file.open" || tc.id == "people.thread.send" || tc.id == "br.navigate" || tc.id == "agentHub.file.open" || tc.id == "people.file.pick" || tc.id == "agentHub.task.start" || tc.id == "meetings.start" || tc.id == "computer.control" || tc.id == "provider.test" {
			if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, tc.needle) {
				t.Fatalf("round %d %s: %+v", tc.round, tc.id, byID[tc.id])
			}
			continue
		}
		if !strings.HasPrefix(ev, "不代跑") || !strings.Contains(ev, tc.needle) {
			t.Fatalf("round %d %s: %+v", tc.round, tc.id, byID[tc.id])
		}
	}
}

func TestCatalogProbeRounds55to69MediaAliasReasons(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	ids := []string{
		"media.play", "media.pause", "media.stop", "media.toggle",
		"media.next", "media.previous", "media.seek", "media.set_volume",
		"media.mute", "media.unmute", "media.create", "media.clear",
		"media.move", "media.remove", "media.jump",
	}
	for i, id := range ids {
		ev := byID[id].Evidence
		if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") {
			t.Fatalf("round %d %s: %+v", 55+i, id, byID[id])
		}
	}
}

func TestCatalogProbeRound70FinalCounts(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	var leftovers []string
	counts := map[string]int{}
	for _, id := range liveCatalogBridgeIDs() {
		item := byID[id]
		kind := probeEvidenceKind(item.Evidence, item.Status)
		counts[kind]++
		if kind != "已跑完" {
			leftovers = append(leftovers, id+"="+kind+":"+item.Evidence)
		}
	}
	t.Logf("round70 counts 已跑完=%d 入口已跑到=%d 不代跑=%d 失败=%d 尚未跑完=%d",
		counts["已跑完"], counts["入口已跑到"], counts["不代跑"], counts["失败"], counts["尚未跑完"])
	t.Logf("not finished: %s", strings.Join(leftovers, " | "))
	if counts["尚未跑完"] != 0 || counts["失败"] != 0 {
		t.Fatalf("unexpected unfinished/fail: %+v", counts)
	}
	if counts["已跑完"] < 3 {
		t.Fatalf("已跑完 regressed: %+v", counts)
	}
}

func assertProbeFinished(t *testing.T, got []producthub.TaskResult, wantPrefix string, ids ...string) {
	t.Helper()
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	for _, id := range ids {
		item, ok := byID[id]
		if !ok {
			t.Fatalf("missing probe %s", id)
		}
		kind := probeEvidenceKind(item.Evidence, item.Status)
		if kind == "尚未跑完" {
			t.Fatalf("%s still unfinished: %+v", id, item)
		}
		switch kind {
		case "已跑完":
			if item.Status != "pass" {
				t.Fatalf("%s kind 已跑完 but status %q: %+v", id, item.Status, item)
			}
		case "入口已跑到":
			if item.Status != "reached" {
				t.Fatalf("%s kind 入口已跑到 but status %q: %+v", id, item.Status, item)
			}
		case "不代跑":
			if item.Status != "skipped" {
				t.Fatalf("%s kind 不代跑 but status %q: %+v", id, item.Status, item)
			}
		case "失败":
			if item.Status != "fail" {
				t.Fatalf("%s kind 失败 but status %q: %+v", id, item.Status, item)
			}
		}
		if wantPrefix != "" && !strings.HasPrefix(item.Evidence, wantPrefix) && !strings.Contains(item.Evidence, wantPrefix) {
			// allow 已跑完 when empty wantPrefix; when wantPrefix set, require it or sibling finished kinds for flexible rounds
			if wantPrefix == "入口已跑到" && (strings.HasPrefix(item.Evidence, "已跑完") || strings.HasPrefix(item.Evidence, "不代跑")) {
				continue
			}
			if wantPrefix == "不代跑" && !strings.HasPrefix(item.Evidence, "不代跑") {
				t.Fatalf("%s want 不代跑: %+v", id, item)
			}
			if wantPrefix != "不代跑" && wantPrefix != "入口已跑到" {
				t.Fatalf("%s want prefix %q: %+v", id, wantPrefix, item)
			}
		}
	}
}

func probeEvidenceKind(evidence, status string) string {
	ev := strings.TrimSpace(evidence)
	switch {
	case strings.HasPrefix(ev, "不代跑"):
		return "不代跑"
	case strings.HasPrefix(ev, "入口已跑到"):
		return "入口已跑到"
	case strings.HasPrefix(ev, "已跑完"):
		return "已跑完"
	case status == "fail" || strings.Contains(ev, "任务完成不了") || strings.HasPrefix(ev, "没有正常返回"):
		return "失败"
	default:
		return "尚未跑完"
	}
}

func TestCatalogProbeDoesNotOpenTheSystemEditor(t *testing.T) {
	entered := 0
	prev := openArtifactShell
	openArtifactShell = func(string) error { entered++; return nil }
	t.Cleanup(func() { openArtifactShell = prev })
	catalogProbeSuppressShell.Store(true)
	t.Cleanup(func() { catalogProbeSuppressShell.Store(false) })
	if err := openLocalArtifactPath("probe-artifact.txt"); err != nil || entered != 0 {
		t.Fatalf("err=%v entered=%d", err, entered)
	}
}

func TestCatalogProbeEvidenceNamesItsLimit(t *testing.T) {
	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		byID[item.ID] = item
	}
	cases := map[string]string{
		"br.navigate":         "没有打开本机浏览器",
		"people.file.open":    "没有用系统打开",
		"people.thread.send":  "没有发给真实同事",
		"people.file.pick":    "没有弹出选择框",
		"meetings.start":      "没有打开麦克风",
		"agentHub.task.start": "没有拉起真实进程",
		"agentHub.file.open":  "没有用系统打开",
		"computer.control":    "没有操控本机键鼠",
		"provider.test":       "没有连接真实供应商",
	}
	for id, needle := range cases {
		ev := byID[id].Evidence
		if !strings.HasPrefix(ev, "已跑完") || !strings.Contains(ev, "读回") || !strings.Contains(ev, needle) {
			t.Fatalf("%s: %+v", id, byID[id])
		}
	}
}

func TestOverlappingCatalogProbesKeepTheFileReadback(t *testing.T) {
	var wg sync.WaitGroup
	errc := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := runCatalogProbes(context.Background())
			for _, item := range got {
				if item.ID == "agentHub.file.open" && !strings.Contains(item.Evidence, "没有用系统打开") {
					errc <- item.Evidence
				}
			}
		}()
	}
	wg.Wait()
	close(errc)
	for ev := range errc {
		t.Fatalf("overlapping checks lost the file readback: %s", ev)
	}
}

func liveCatalogBridgeIDs() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range producthub.LiveCatalog() {
		for _, b := range c.Scaffold.Bridge {
			if strings.Count(b, ".") < 1 || strings.HasPrefix(b, "capability.") {
				continue
			}
			if _, ok := seen[b]; ok {
				continue
			}
			seen[b] = struct{}{}
			out = append(out, b)
		}
	}
	return out
}

// TestCatalogProbeSweepCoversRegisteredVerbs proves the verb sweep covers the
// full runtime dispatch table once it is registered: every user-facing verb is
// probed by a curated round or the sweep, the evidence kind matches the status
// tier, engine plumbing gets no probe, and no ID appears twice.
func TestCatalogProbeSweepCoversRegisteredVerbs(t *testing.T) {
	methods := make([]string, 0, len(RuntimeHandlers)+len(internalRuntimeHandlers))
	for method := range RuntimeHandlers {
		methods = append(methods, string(method))
	}
	for method := range internalRuntimeHandlers {
		methods = append(methods, string(method))
	}
	producthub.SetBridgeMethods(methods)
	defer producthub.SetBridgeMethods(nil)

	got := runCatalogProbes(context.Background())
	byID := map[string]producthub.TaskResult{}
	for _, item := range got {
		if _, dup := byID[item.ID]; dup {
			t.Fatalf("重复的探测 ID：%s", item.ID)
		}
		byID[item.ID] = item
	}
	var problems []string
	counts := map[string]int{}
	for _, id := range liveCatalogBridgeIDs() {
		item, ok := byID[id]
		if !ok {
			problems = append(problems, id+" (missing)")
			counts["尚未跑完"]++
			continue
		}
		kind := probeEvidenceKind(item.Evidence, item.Status)
		counts[kind]++
		switch kind {
		case "已跑完":
			if item.Status != "pass" {
				problems = append(problems, fmt.Sprintf("%s kind=已跑完 status=%s", id, item.Status))
			}
		case "入口已跑到":
			if item.Status != "reached" {
				problems = append(problems, fmt.Sprintf("%s kind=入口已跑到 status=%s", id, item.Status))
			}
		case "不代跑":
			if item.Status != "skipped" {
				problems = append(problems, fmt.Sprintf("%s kind=不代跑 status=%s", id, item.Status))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s kind=%s status=%s evidence=%s", id, kind, item.Status, item.Evidence))
		}
	}
	t.Logf("sweep counts 已跑完=%d 入口已跑到=%d 不代跑=%d 失败=%d 尚未跑完=%d total=%d",
		counts["已跑完"], counts["入口已跑到"], counts["不代跑"], counts["失败"], counts["尚未跑完"], len(byID))
	if len(problems) > 0 {
		t.Fatalf("动词表覆盖未收口 %d 项：%v", len(problems), problems)
	}
	checked := 0
	for method, reason := range sweepSkipProbes {
		item, ok := byID[method]
		if !ok {
			continue // 该动词不在当前运行时注册表里，无卡也无探测
		}
		checked++
		if item.Status != "skipped" || item.Evidence != reason {
			t.Fatalf("危险动词 %s 应为不代跑（%s），实际：%+v", method, reason, item)
		}
	}
	if checked == 0 {
		t.Fatalf("危险动词清单里没有任何动词被注册，抽查失效")
	}
	for method := range RuntimeHandlers {
		m := string(method)
		if strings.HasPrefix(m, "internal.") || strings.HasPrefix(m, "fs.") {
			if _, ok := byID[m]; ok {
				t.Fatalf("引擎管道动词 %s 不应出现在探测结果里", m)
			}
		}
	}
	for method := range internalRuntimeHandlers {
		if _, ok := byID[string(method)]; ok {
			t.Fatalf("运行时内部动词 %s 不应出现在探测结果里", method)
		}
	}
}
