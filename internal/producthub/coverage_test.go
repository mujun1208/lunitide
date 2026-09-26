package producthub

import (
	"context"
	"strings"
	"testing"
)

func TestLiveCoverageIsCompleteAndReadable(t *testing.T) {
	s := New(&MemoryPersist{})
	ctx := context.Background()
	ed, err := s.Generate(ctx, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if ed.CatalogProbe.Total < 40 || ed.CatalogProbe.Passed != ed.CatalogProbe.Total {
		t.Fatalf("coverage %+v findings %#v", ed.CatalogProbe, ed.Findings)
	}
	var invoke bool
	for _, f := range ed.Findings {
		if f.ErrorCode == "PH_021" && f.StableKey == "feature.assets.mcp.invoke" && f.Status == "open" {
			invoke = true
		}
	}
	if !invoke {
		t.Fatal("calling an MCP tool with no bridge method was not reported")
	}
	ov, err := s.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.ProbeTotal < 40 || ov.ProbePassed != ov.ProbeTotal {
		t.Fatalf("coverage %d/%d", ov.ProbePassed, ov.ProbeTotal)
	}
	if ov.EditionID != ed.EditionID {
		t.Fatal("opening the overview must not write a new edition")
	}
	home, ok, err := s.FeatureCard(ctx, "feature.dialog.page.home")
	if err != nil || !ok || !strings.Contains(home.Summary, "从导航打开") {
		t.Fatalf("home card %#v %v", home, err)
	}
	voice, ok, err := s.FeatureCard(ctx, "feature.dialog.companion.voice-talk")
	if err != nil || !ok || !strings.Contains(voice.Summary, "唤醒后用语音连续对话") {
		t.Fatalf("voice card %#v", voice)
	}
	findings, md, _, err := s.Diagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "活源覆盖") || !strings.Contains(md, "不是语音听写") {
		t.Fatal("report missing the coverage verdict")
	}
	for _, f := range findings {
		if f.ErrorCode == "PH_000" && f.Status != "clear" {
			t.Fatalf("aligned catalog must not look like a closed ticket: %#v", f)
		}
	}
	for _, f := range findings {
		if f.ErrorCode == "PH_019" || f.ErrorCode == "PH_020" {
			t.Fatalf("unexpected gap %s %s", f.ErrorCode, f.StableKey)
		}
	}
	latest, err := s.Latest(ctx)
	if err != nil || latest == nil || latest.EditionID != ed.EditionID {
		t.Fatal("diagnostics must not persist a new edition")
	}
}

func TestMissingLiveCardIsARealFinding(t *testing.T) {
	findings, probe, _ := diagnoseCatalog(nil, LiveCatalog())
	if probe.Total < 40 || probe.Passed != 0 {
		t.Fatalf("probe %+v", probe)
	}
	var gaps int
	for _, f := range findings {
		if f.ErrorCode == "PH_019" && f.Status == "open" {
			gaps++
		}
	}
	if gaps != probe.Total {
		t.Fatalf("gaps %d probe %+v", gaps, probe)
	}
}

func TestUnknownPageIsAWarning(t *testing.T) {
	live := []Candidate{{
		StableKey: "feature.dialog.page.missing", Name: "进入不存在", Domain: "dialog", Module: "home",
		Summary: "从导航打开「不存在」。", ChainClass: "page-enter",
		Scaffold: Scaffold{Pages: []string{"not-a-page"}},
	}}
	cards := []Card{{
		StableKey: live[0].StableKey, Name: live[0].Name, Domain: "dialog", Module: "home",
		Summary: live[0].Summary, Methods: []Method{{Type: "menu", Entry: "打开"}},
		Scaffold: live[0].Scaffold,
	}}
	findings, probe, _ := diagnoseCatalog(cards, live)
	if probe.Passed != 0 || probe.Total != 1 {
		t.Fatalf("probe %+v", probe)
	}
	var hit bool
	for _, f := range findings {
		if f.ErrorCode == "PH_020" && strings.Contains(f.Evidence, "not-a-page") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("findings %#v", findings)
	}
}

type refuseCollab struct{ t *testing.T }

func (r refuseCollab) Consult(context.Context, string) (ConsultResult, error) {
	r.t.Fatal("wont_fix must not consult a skill")
	return ConsultResult{}, nil
}

func TestWontFixPersistsWithoutConsult(t *testing.T) {
	s := New(&MemoryPersist{})
	s.SetCollaborator(refuseCollab{t: t})
	ctx := context.Background()
	ed, err := s.Generate(ctx, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(ed.Findings) == 0 {
		t.Fatal("expected a finding")
	}
	f := ed.Findings[0]
	res, err := s.Apply(ctx, "wont_fix", f.ErrorCode+"|"+f.StableKey)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "wont_fix" || res.SkillName != "" {
		t.Fatalf("apply %#v", res)
	}
	findings, _, _, err := s.Diagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range findings {
		if item.ErrorCode == "PH_000" && item.Status == "wont_fix" {
			t.Fatalf("alignment note must not stay a closed ticket: %#v", item)
		}
	}
}

func TestUserWontFixStaysOnARealFinding(t *testing.T) {
	prev := []Finding{
		{ErrorCode: "PH_020", StableKey: "k", Status: "wont_fix", Plan: "leave"},
		{ErrorCode: "PH_000", StableKey: "product.lunitide", Status: "wont_fix"},
	}
	next := []Finding{
		{ErrorCode: "PH_020", StableKey: "k", Status: "open"},
		{ErrorCode: "PH_000", StableKey: "product.lunitide", Status: "clear"},
	}
	got := mergeFindingStatus(prev, next)
	if got[0].Status != "wont_fix" || got[0].Plan != "leave" {
		t.Fatalf("real finding lost the user decision: %#v", got[0])
	}
	if got[1].Status != "clear" {
		t.Fatalf("alignment note kept a fake wont_fix: %#v", got[1])
	}
}
