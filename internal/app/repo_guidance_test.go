package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoGuidanceInjectionReadsAgentsAndLocalSkills(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Always run tests. Keep 月汐 identity."), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(root, ".agents", "skills", "review-pr")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# review-pr\nDo not dump this body."), 0o644); err != nil {
		t.Fatal(err)
	}
	got := repoGuidanceInjection(root)
	if !strings.Contains(got, "不替换身份") {
		t.Fatalf("missing identity overlay: %s", got)
	}
	if !strings.Contains(got, "Always run tests") {
		t.Fatalf("missing AGENTS.md: %s", got)
	}
	if !strings.Contains(got, "review-pr") {
		t.Fatalf("missing skill name: %s", got)
	}
	if strings.Contains(got, "Do not dump this body") {
		t.Fatalf("injected skill body: %s", got)
	}
}

func isolateHomeAgentSkills(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	testHomeAgentSkillsRoot = &empty
	t.Cleanup(func() { testHomeAgentSkillsRoot = nil })
}

func TestRepoGuidanceInjectionEmptyWhenNoFiles(t *testing.T) {
	isolateHomeAgentSkills(t)
	if got := repoGuidanceInjection(t.TempDir()); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestRepoGuidanceWalksUpToRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Prefer small diffs."), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got := repoGuidanceInjection(nested)
	if !strings.Contains(got, "Prefer small diffs") {
		t.Fatalf("did not walk up to AGENTS.md: %s", got)
	}
}

func TestRepoGuidanceChainsNestedAgentsMarkdown(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("ROOT-RULE keep tests."), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "AGENTS.md"), []byte("NEAR-RULE prefer CSS modules."), 0o644); err != nil {
		t.Fatal(err)
	}
	got := repoGuidanceInjection(nested)
	rootAt := strings.Index(got, "ROOT-RULE")
	nearAt := strings.Index(got, "NEAR-RULE")
	if rootAt < 0 || nearAt < 0 || nearAt < rootAt {
		t.Fatalf("want git-root then nearer last, got %s", got)
	}
	if !strings.Contains(got, "apps/web") && !strings.Contains(got, `apps\web`) {
		t.Fatalf("nested label missing: %s", got)
	}
}

func TestRepoGuidanceDoesNotWalkPastMissingGit(t *testing.T) {
	isolateHomeAgentSkills(t)
	if got := repoGuidanceInjection(t.TempDir()); got != "" {
		t.Fatalf("want empty without git/AGENTS, got %q", got)
	}
}

func TestRepoGuidanceIncludesHomeAgentSkills(t *testing.T) {
	home := t.TempDir()
	skillDir := filepath.Join(home, ".agents", "skills", "my-home-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# home"), 0o644); err != nil {
		t.Fatal(err)
	}
	testHomeAgentSkillsRoot = &home
	t.Cleanup(func() { testHomeAgentSkillsRoot = nil })
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Keep going."), 0o644); err != nil {
		t.Fatal(err)
	}
	got := repoGuidanceInjection(root)
	if !strings.Contains(got, "my-home-skill") {
		t.Fatalf("missing home skill: %s", got)
	}
}
