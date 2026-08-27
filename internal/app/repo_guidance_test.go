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

func TestRepoGuidanceInjectionEmptyWhenNoFiles(t *testing.T) {
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

func TestRepoGuidanceDoesNotWalkPastMissingGit(t *testing.T) {
	if got := repoGuidanceInjection(t.TempDir()); got != "" {
		t.Fatalf("want empty without git/AGENTS, got %q", got)
	}
}
