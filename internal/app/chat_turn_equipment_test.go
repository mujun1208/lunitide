package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/people"
)

type priorUserReader struct {
	msgs []contextapp.Message
}

func (r priorUserReader) ListMessages(context.Context, string, string, int) ([]contextapp.Message, error) {
	return r.msgs, nil
}
func (priorUserReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func TestTurnEquipmentForResumePeeksPriorExpertChip(t *testing.T) {
	const ppt = "01ARZ3NDEKTSV4RRFFQ69G5FAC"
	const resume = "继续上次未完成的工作。结合任务清单、已完成步骤和我补充过的说明，接着做到完成。"
	e := NewEngine(nil, "test")
	e.messageReader = priorUserReader{msgs: []contextapp.Message{
		{Role: "user", Sequence: 1, Content: "[引用专家 PPT专家|" + ppt + "] 做封面"},
		{Role: "user", Sequence: 2, Content: resume},
	}}
	eq := e.turnEquipmentFor(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", resume, false)
	if len(eq.ExpertIDs) != 1 || eq.ExpertIDs[0] != ppt {
		t.Fatalf("ids=%v names=%v", eq.ExpertIDs, eq.Names)
	}
	if len(eq.Names) != 1 || eq.Names[0] != "PPT专家" {
		t.Fatalf("names=%v", eq.Names)
	}
}

func TestResolvePublishedSkillIDAcceptsTemplateID(t *testing.T) {
	e := NewEngine(nil, "test")
	e.skills = &skillCatalogStub{items: []skill.Skill{{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "tpl-slide-builder",
		Status: skill.SkillStatusPublished, EntryPoint: "builtin://slide-builder",
	}}}
	got, err := e.resolvePublishedSkillID(context.Background(), "slide-builder")
	if err != nil || got != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestMcpNameAllowedRestrictsUnbound(t *testing.T) {
	e := NewEngine(nil, "test")
	if e.mcpNameAllowed("mcp_01ARZ3NDEKTSV4RRFFQ69G5FAV_fetch", "01ARZ3NDEKTSV4RRFFQ69G5FAV", []string{"playwright"}, true) {
		t.Fatal("unknown endpoint must be denied when restricted")
	}
	if !e.mcpNameAllowed("mcp.search", "", []string{"playwright"}, true) {
		t.Fatal("search is allowed when the expert has any MCP")
	}
	if e.mcpNameAllowed("mcp.search", "", nil, true) {
		t.Fatal("search must be denied with empty ACL")
	}
}

func TestPeopleAgentReplyStaleAndCollision(t *testing.T) {
	user := people.Message{MessageID: "m1", SenderID: "human", Kind: "text", Body: "第一句"}
	later := people.Message{MessageID: "m2", SenderID: "human", Kind: "text", Body: "第二句"}
	if !peopleAgentReplyStale([]people.Message{user, later}, user) {
		t.Fatal("newer user text must stale the first turn")
	}
	other := people.Message{MessageID: "m3", SenderID: "agent-b", Kind: "text", Body: "我先回"}
	if !peopleAgentCollision([]people.Message{user, other}, "m1", "agent-a", map[string]bool{"agent-b": true}) {
		t.Fatal("other agent reply must count as collision")
	}
}

func TestLocalBrainUserErrorAndPrefix(t *testing.T) {
	if !strings.Contains(localBrainUserError(BrainCodex, errLocalBrainMissing), "PATH") {
		t.Fatal("PATH miss must be visible")
	}
	if localBrainPrefix(BrainCodex) != "【本机 Codex】\n" {
		t.Fatal(localBrainPrefix(BrainCodex))
	}
	if packEntrypointOrDefault("") != "pack://manifest" || packEntrypointOrDefault("plugin/main.ts") != "pack://manifest" {
		t.Fatal("chat and form must not default to plugin/main.ts")
	}
}

var errLocalBrainMissing = errLocalBrainPATH()

func errLocalBrainPATH() error {
	return &simpleErr{"local brain not on PATH"}
}

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func TestSkillViewHonestNoAttachment(t *testing.T) {
	e := NewEngine(nil, "test")
	e.skills = &skillCatalogStub{}
	out, err := e.invokeSkillViewTool(context.Background(), []byte(`{"skillId":"computer-control","path":"docs/missing.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "没有附件") {
		t.Fatalf("missing path must be honest: %s", out.Output)
	}
}

func TestSkillViewReadsWorkspaceAttachment(t *testing.T) {
	root := t.TempDir()
	prev := testHomeAgentSkillsRoot
	testHomeAgentSkillsRoot = &root
	t.Cleanup(func() { testHomeAgentSkillsRoot = prev })
	dir := filepath.Join(root, ".agents", "skills", "computer-control", "docs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api.md"), []byte("attachment body for L2"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.skills = &skillCatalogStub{}
	out, err := e.invokeSkillViewTool(context.Background(), []byte(`{"skillId":"computer-control","path":"docs/api.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "attachment body for L2") {
		t.Fatalf("must read workspace attachment: %s", out.Output)
	}
}

func TestPresetIDFromCommandArgs(t *testing.T) {
	if got := presetIDFromCommandArgs("npx", []string{"-y", "@playwright/mcp"}); got != "playwright" {
		t.Fatalf("playwright = %q", got)
	}
	if got := presetIDFromCommandArgs("npx", []string{"-y", "@modelcontextprotocol/server-filesystem", "C:/data"}); got != "filesystem" {
		t.Fatalf("filesystem = %q", got)
	}
	if got := presetIDFromCommandArgs("npx", []string{"-y", "@not-a-preset/mcp"}); got != "" {
		t.Fatalf("unknown package = %q", got)
	}
}
