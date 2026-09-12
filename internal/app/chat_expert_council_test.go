package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestPhaseKeyFromWorkbenchLabel(t *testing.T) {
	if got := phaseKeyFromWorkbenchLabel("开发"); got != m8core.PhaseDevelopmentChange {
		t.Fatalf("开发 = %q", got)
	}
	if got := phaseKeyFromWorkbenchLabel("发布"); got != m8core.PhaseReleaseDelivery {
		t.Fatalf("发布 = %q", got)
	}
	if phaseKeyFromWorkbenchLabel("") != "" {
		t.Fatal("empty label should map to empty key")
	}
}

func TestAppendUniqueExpertIDs(t *testing.T) {
	got := appendUniqueExpertIDs([]string{"a", "b"}, "b", "c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %#v", got)
	}
	many := appendUniqueExpertIDs(nil, "1", "2", "3", "4", "5", "6", "7", "8", "9")
	if len(many) != councilMaxExperts {
		t.Fatalf("cap = %d", len(many))
	}
}

func TestFormatCouncilBrief(t *testing.T) {
	brief := formatCouncilBrief("怎么设计登录？", []councilOpinion{
		{ExpertName: "安全", Text: "【立场】要零信任\n【建议】MFA"},
		{ExpertName: "产品", Text: "【立场】先简单\n【建议】短信码"},
	})
	if !strings.Contains(brief, "安全") || !strings.Contains(brief, "产品") || !strings.Contains(brief, "怎么设计登录？") {
		t.Fatalf("brief = %q", brief)
	}
}

func TestCouncilChairInstructionCompanion(t *testing.T) {
	brief := formatCouncilBrief("x", nil)
	chair := councilChairInstruction(brief, true)
	if !strings.Contains(chair, "语音") {
		t.Fatalf("companion chair = %q", chair)
	}
	chairDesktop := councilChairInstruction(brief, false)
	if !strings.Contains(chairDesktop, "## 综合结论") {
		t.Fatalf("desktop chair = %q", chairDesktop)
	}
}

func TestBuildExpertCouncilConfigRequiresTwoExperts(t *testing.T) {
	e := &Engine{m8expert: nil}
	if cfg := e.buildExpertCouncilConfig(t.Context(), expertCouncilInputs{TurnText: "请三位专家评审架构"}); cfg != nil {
		t.Fatal("nil service should not council")
	}
}

func TestBuildExpertCouncilConfigSkipsCompanion(t *testing.T) {
	e := &Engine{}
	if cfg := e.buildExpertCouncilConfig(t.Context(), expertCouncilInputs{Companion: true, TurnText: "请三位专家评审架构"}); cfg != nil {
		t.Fatal("companion voice mode should skip expert council")
	}
}

func TestSkipExpertCouncilStillBlocksCouncil(t *testing.T) {
	e := &Engine{}
	if cfg := e.buildExpertCouncilConfig(t.Context(), expertCouncilInputs{TurnText: "帮我在桌面创建一个文件夹"}); cfg != nil {
		t.Fatal("simple folder task should skip council")
	}
	if cfg := e.buildExpertCouncilConfig(t.Context(), expertCouncilInputs{TurnText: "打开网站播放音乐"}); cfg != nil {
		t.Fatal("companion browser/music task should skip council")
	}
}

func TestExpertDeliberateDigestStable(t *testing.T) {
	a := expertDeliberateDigest("01ARZ3NDEKTSV4RRFFQ69G5FAV", "打开网站播放音乐")
	b := expertDeliberateDigest("01ARZ3NDEKTSV4RRFFQ69G5FAV", "打开网站播放音乐")
	if a == "" || a != b || len(a) != 64 {
		t.Fatalf("digest = %q", a)
	}
}

type stubSessionExperts struct{ ids []string }

func (s stubSessionExperts) ListSessionExpertIDs(context.Context, string) ([]string, error) {
	return append([]string(nil), s.ids...), nil
}

func (s stubSessionExperts) ReplaceSessionExpertIDs(context.Context, string, []string) error {
	return nil
}

func TestSelectedTurnExpertIDsUsesMountedSubsetOnly(t *testing.T) {
	ai, arch := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	got := selectedTurnExpertIDs([]string{ai, arch}, "重新思考，给出一个新的方案。")
	if len(got) != 2 || got[0] != ai || got[1] != arch {
		t.Fatalf("two mounts and no @/chip must still be the roster: %#v", got)
	}
}

func TestSelectedTurnExpertIDsSingleChipDoesNotSpawnOthers(t *testing.T) {
	one := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	got := selectedTurnExpertIDs([]string{one}, "请评审这份架构")
	if len(got) != 1 || got[0] != one {
		t.Fatalf("single chip spawned %#v", got)
	}
}

func TestSelectedTurnExpertIDsTurnRefsBeatStalePMMounts(t *testing.T) {
	ppt, novel, report := "01ARZ3NDEKTSV4RRFFQ69G5FAA", "01ARZ3NDEKTSV4RRFFQ69G5FAB", "01ARZ3NDEKTSV4RRFFQ69G5FAC"
	ai, security := "01ARZ3NDEKTSV4RRFFQ69G5FAD", "01ARZ3NDEKTSV4RRFFQ69G5FAE"
	turn := "[引用专家 AI工程师|" + ai + "]\n[引用专家 安全工程师|" + security + "]\n重新思考，给出一个新的方案。"
	got := selectedTurnExpertIDs([]string{ppt, novel, report}, turn)
	if !containsAllExpertIDs(got, ppt, novel, report, ai, security) {
		t.Fatalf("chips must union with still-mounted experts, not replace them: %#v", got)
	}
	prev := "[引用专家 PPT专家|" + ppt + "]\n[引用专家 小说编写专家|" + novel + "]\n旧方案"
	got = selectedTurnExpertIDs([]string{ppt, novel, report}, turn, prev)
	if !containsAllExpertIDs(got, ppt, novel, report, ai, security) {
		t.Fatalf("current chips must union mounts and ignore previous-turn-only refs: %#v", got)
	}
}

func TestSelectedTurnExpertIDsEmptyPMRethinkDoesNotAttachCatalog(t *testing.T) {
	got := selectedTurnExpertIDs(nil, "重新思考，给出一个新的方案。")
	if len(got) != 0 {
		t.Fatalf("empty chips must not spawn conversation specialists: %#v", got)
	}
}

func TestSelectedTurnExpertIDsMountedOpsYieldsToScriptIntent(t *testing.T) {
	got := selectedTurnExpertIDs([]string{"mx-planning-expert"}, "处理剧本专家的问题，帮我改一版对白")
	if len(got) != 0 {
		t.Fatalf("mounted aviation must yield to script intent: %#v", got)
	}
	keep := selectedTurnExpertIDs([]string{"mx-planning-expert"}, "查一下飞机维修手册隔离步骤")
	if len(keep) != 1 || keep[0] != "mx-planning-expert" {
		t.Fatalf("same-domain ops mount must stay: %#v", keep)
	}
	ppt := selectedTurnExpertIDs([]string{"ppt-expert"}, "处理剧本专家的问题，帮我改一版对白")
	if len(ppt) != 0 {
		t.Fatalf("mounted PPT must also yield to script intent: %#v", ppt)
	}
	same := selectedTurnExpertIDs([]string{"ppt-expert"}, "帮我做一份路演 PPT")
	if len(same) != 1 || same[0] != "ppt-expert" {
		t.Fatalf("same-domain PPT mount must stay: %#v", same)
	}
}

func TestCollectCouncilExpertIDsIgnoresProjectPhaseMatrix(t *testing.T) {
	ai, arch := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	e := &Engine{sessionExperts: stubSessionExperts{ids: []string{ai, arch}}}
	got := e.collectCouncilExpertIDs(t.Context(), expertCouncilInputs{
		SessionID:  "01ARZ3NDEKTSV4RRFFQ69G5FAX",
		ProjectID:  "01ARZ3NDEKTSV4RRFFQ69G5FAY",
		PhaseLabel: "需求架构规范",
		TurnText:   "重新思考，给出一个新的方案。",
	})
	if len(got) != 2 || got[0] != ai || got[1] != arch {
		t.Fatalf("two session mounts must be the council roster: %#v", got)
	}
}

func containsAllExpertIDs(got []string, want ...string) bool {
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestExpertDeliberatePromptDropsToolsWhenLaneForbids(t *testing.T) {
	prompt := expertDeliberateSystemPromptForTools("润色专家", "改句子", false)
	if strings.Contains(prompt, "先调用 web.search") || strings.Contains(prompt, "*.gen") && strings.Contains(prompt, "需要成文") {
		t.Fatalf("no-tool prompt still asks for tools: %q", prompt)
	}
	if !strings.Contains(prompt, "不要调用任何工具") {
		t.Fatalf("missing no-tool instruction: %q", prompt)
	}
}

func TestCouncilChairL1DoesNotForceDocx(t *testing.T) {
	chair := councilChairInstructionForLane(formatCouncilBrief("润色这段", nil), false, LaneL1)
	if strings.Contains(chair, "必须把交付做完") || councilChairMustUseTools(chair) {
		t.Fatalf("L1 chair must not hijack polish into gen/search: %q", chair)
	}
	if !strings.Contains(chair, "原任务") && !strings.Contains(chair, "润色") {
		t.Fatalf("L1 chair = %q", chair)
	}
}

func TestCouncilStepsForLane(t *testing.T) {
	if councilStepsForLane(LaneL1) != 1 || councilToolsForLane(LaneL1) {
		t.Fatal("L1 council is 1 step without tools")
	}
	if councilStepsForLane(LaneL3) != 2 || !councilToolsForLane(LaneL3) {
		t.Fatal("L3 council is 2 steps with tools")
	}
	if councilStepsForLane(LaneL4) != councilExpertMaxSteps {
		t.Fatal("L4 keeps current council steps")
	}
}

func TestPinCouncilInviteLeadT15(t *testing.T) {
	lead := councilInviteSpeech()
	if got := pinCouncilInviteLead("我已经综合两位专家的意见。", lead); !strings.HasPrefix(strings.TrimLeft(got, " \n"), lead) {
		t.Fatalf("must lead with invite, got %q", got)
	}
	dup := lead + "。请在侧栏挂上。\n"
	if got := pinCouncilInviteLead(dup, lead); got != dup {
		t.Fatalf("already-pinned text rewritten: %q", got)
	}
	if pinCouncilInviteLead("ok", "") != "ok" {
		t.Fatal("empty lead must not change text")
	}
}

func TestSelectedTurnExpertIDsChipsAloneAreCouncilRosterT12(t *testing.T) {
	a, b := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	turn := "[引用专家 润色A|" + a + "][引用专家 润色B|" + b + "]\n把这段话润色得更顺：今天开会很顺利"
	got := selectedTurnExpertIDs(nil, turn)
	if !containsAllExpertIDs(got, a, b) || len(got) != 2 {
		t.Fatalf("T12 chips-only roster = %#v", got)
	}
	if !councilShouldRun(LaneL1, got, turn, false) {
		t.Fatal("T12 chips must open council")
	}
	if councilToolsForLane(LaneL1) {
		t.Fatal("T12 L1 experts must have no tools")
	}
}

func TestBuildExpertCouncilConfigChipsAloneT12(t *testing.T) {
	e := newExpertSkillsEngine(t)
	ctx := context.Background()
	a := createNamedLocalExpert(t, e, ctx, "润色A", "req-t12-a")
	b := createNamedLocalExpert(t, e, ctx, "润色B", "req-t12-b")
	turn := "[引用专家 润色A|" + a + "][引用专家 润色B|" + b + "]\n把这段话润色得更顺：今天开会很顺利"
	cfg := e.buildExpertCouncilConfig(ctx, expertCouncilInputs{TurnText: turn, Lane: LaneL1})
	if cfg == nil || !cfg.Enabled || len(cfg.Experts) < 2 {
		t.Fatalf("T12 chips-only council = %+v", cfg)
	}
	if cfg.Tools || cfg.MaxSteps != 1 {
		t.Fatalf("T12 L1 experts must be 1 step no tools: steps=%d tools=%v", cfg.MaxSteps, cfg.Tools)
	}
}

func createNamedLocalExpert(t *testing.T, e *Engine, ctx context.Context, name, requestID string) string {
	t.Helper()
	created := e.Handle(ctx, nominationRequest("expert.create", `{"source":"local","frontmatter":{"name":"`+name+`","division":"engineering","description":"x","semver":"1.0.0"},"sixSection":{"identity":"i","mission":"m","rules":"r","workflow":"w","deliverableTemplate":"d","successMetrics":"s"},"requestId":"`+requestID+`"}`))
	if !created.OK {
		t.Fatalf("expert.create %s: %+v", name, created.Error)
	}
	var payload struct {
		ExpertID string `json:"expertId"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.ExpertID) != 26 {
		t.Fatalf("expert id = %q", payload.ExpertID)
	}
	return payload.ExpertID
}

func TestCouncilShouldRunT11T15T21(t *testing.T) {
	a, b := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	if !councilShouldRun(LaneL1, []string{a, b}, "润色这段", false) {
		t.Fatal("T11: L1 + two mounts must run council")
	}
	if councilShouldRun(LaneL0, []string{a, b}, "你好", false) {
		t.Fatal("T21: L0 must not run council")
	}
	if councilShouldRun(LaneL1, nil, "请两位一起评", false) {
		t.Fatal("T15: no mounts must not run council")
	}
	if !councilInviteNeeded("请两位一起评这份稿", nil) {
		t.Fatal("T15: must ask the user to mount two experts")
	}
	if councilShouldRun(LaneL1, []string{a, b}, "润色这段", true) {
		t.Fatal("companion must not run council")
	}
}
