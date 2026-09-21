package producthub

import (
	"context"
	"strings"
	"testing"
)

func TestUnlockAndChangePassword(t *testing.T) {
	s := New(&MemoryPersist{})
	ctx := context.Background()
	if _, err := s.Unlock(ctx, "nope", initialPassword); err != ErrBadCredentials {
		t.Fatalf("wrong user: %v", err)
	}
	if _, err := s.Unlock(ctx, AdminUsername, "bad"); err != ErrBadCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	token, err := s.Unlock(ctx, AdminUsername, initialPassword)
	if err != nil || token == "" {
		t.Fatalf("unlock: %v %q", err, token)
	}
	if !s.Check(token) {
		t.Fatal("token should work")
	}
	if s.Check("x") {
		t.Fatal("junk token")
	}
	if err := s.ChangePassword(ctx, token, "wrong", "newpass"); err != ErrBadCredentials {
		t.Fatalf("old password must match: %v", err)
	}
	if err := s.ChangePassword(ctx, token, initialPassword, "newpass-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Unlock(ctx, AdminUsername, "newpass-1"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestUnauthorizedWithoutToken(t *testing.T) {
	s := New(&MemoryPersist{})
	if s.Check("") {
		t.Fatal("empty token")
	}
}

func TestCatalogContainsMediaPlay(t *testing.T) {
	var play, page, setting bool
	for _, c := range LiveCatalog() {
		if c.StableKey == "feature.office.media.play" {
			play = true
		}
		if c.StableKey == "feature.office.page.media" {
			page = true
		}
		if c.StableKey == "feature.foundation.settings.voice" {
			setting = true
		}
	}
	if !play || !page || !setting {
		t.Fatalf("live catalog missing anchors play=%v page=%v setting=%v", play, page, setting)
	}
}

func TestGenerateKeepsSeedProseAndAddsLive(t *testing.T) {
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	if ed.CardCount < 40 {
		t.Fatalf("too few cards: %d", ed.CardCount)
	}
	var play Card
	for _, c := range ed.Features {
		if c.StableKey == "feature.dialog.music.play" {
			play = c
		}
	}
	if play.Description == "" || play.Provenance != "seed+live" {
		t.Fatalf("play card %#v", play)
	}
	if play.Name != "放歌" || play.Principle == "" || play.Logic == "" || play.Tech == "" || play.Analysis == "" {
		t.Fatalf("play anatomy incomplete %#v", play)
	}
	if ed.ReportMarkdown == "" || ed.ReportHTML == "" {
		t.Fatal("report must be generated in-app")
	}
	if len(ed.Graph.Nodes) < 20 {
		t.Fatalf("graph too small: %d", len(ed.Graph.Nodes))
	}
	seen := map[string]bool{}
	for _, n := range ed.Graph.Nodes {
		seen[n.Type] = true
	}
	for _, typ := range []string{"Product", "Domain", "Module", "Feature", "Expert", "Skill", "Plugin", "Mcp", "McpTool", "Chain", "Step", "Capability", "Scenario"} {
		if !seen[typ] {
			t.Fatalf("missing ontology node type %s", typ)
		}
	}
	var competitor, frontier bool
	for _, c := range ed.Features {
		if c.StableKey == "landscape.competitor.desktop-assistants" {
			competitor = true
		}
		if strings.HasPrefix(c.StableKey, "landscape.frontier.") {
			frontier = true
		}
	}
	if !competitor || !frontier {
		t.Fatalf("landscape slots missing competitor=%v frontier=%v", competitor, frontier)
	}
}

type stubCollab struct{}

func (stubCollab) Consult(context.Context, string) (ConsultResult, error) {
	return ConsultResult{SkillID: "skill-debug", SkillName: "debugger", Output: "已绑定 debugger"}, nil
}

func TestApplyAndTagPersist(t *testing.T) {
	s := New(&MemoryPersist{})
	s.SetCollaborator(stubCollab{})
	ctx := context.Background()
	ed, err := s.Generate(ctx, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(ed.Findings) == 0 {
		t.Fatal("generate must produce findings")
	}
	res, err := s.Apply(ctx, ed.Findings[0].ErrorCode, ed.Findings[0].StableKey)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.SkillName != "debugger" || res.Plan == "" {
		t.Fatalf("apply %#v", res)
	}
	if err := s.TagSet(ctx, "feature.dialog.music.play", "alias", "放歌"); err != nil {
		t.Fatal(err)
	}
	card, ok, err := s.FeatureCard(ctx, "feature.dialog.music.play")
	if err != nil || !ok {
		t.Fatalf("card %v %v", ok, err)
	}
	found := false
	for _, tag := range card.Tags {
		if tag == "alias:放歌" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manual tag missing: %v", card.Tags)
	}
	ed2, err := s.Generate(ctx, "boot")
	if err != nil {
		t.Fatal(err)
	}
	if ed2.EditionID == ed.EditionID {
		t.Fatal("second generate should persist a new edition")
	}
	ch, err := s.Changelog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = ch
}
