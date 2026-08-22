package app

import (
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
