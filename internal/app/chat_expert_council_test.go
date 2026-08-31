package app

import (
	"context"
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
	if len(got) != 0 {
		t.Fatalf("two mounts and no @/chip must not start council: %#v", got)
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
	if len(got) != 2 || got[0] != ai || got[1] != security {
		t.Fatalf("PM rethink must not keep ppt/novel/report: %#v", got)
	}
	prev := "[引用专家 PPT专家|" + ppt + "]\n[引用专家 小说编写专家|" + novel + "]\n旧方案"
	got = selectedTurnExpertIDs([]string{ppt, novel, report}, turn, prev)
	if len(got) != 2 || got[0] != ai || got[1] != security {
		t.Fatalf("current chips must ignore previous-turn catalog refs: %#v", got)
	}
}

func TestSelectedTurnExpertIDsEmptyPMRethinkDoesNotAttachCatalog(t *testing.T) {
	got := selectedTurnExpertIDs(nil, "重新思考，给出一个新的方案。")
	if len(got) != 0 {
		t.Fatalf("empty chips must not spawn conversation specialists: %#v", got)
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
	if len(got) != 0 {
		t.Fatalf("mount-only rethink must not open council: %#v", got)
	}
}
