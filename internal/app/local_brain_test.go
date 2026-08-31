package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundBrainFromKeys(t *testing.T) {
	if got := BoundBrainFromKeys([]string{"slide-builder", "mcp:playwright"}); got != BrainLunitide {
		t.Fatalf("default = %q", got)
	}
	if got := BoundBrainFromKeys([]string{"brain:codex", "slide-builder"}); got != BrainCodex {
		t.Fatalf("codex = %q", got)
	}
	if got := BoundBrainFromKeys([]string{"brain:claude"}); got != BrainClaude {
		t.Fatalf("claude = %q", got)
	}
	if _, ok := lookPathBrain(BrainLunitide); ok {
		t.Fatal("lunitide must not resolve a CLI binary")
	}
	if localBrainPromptCap < 20000 {
		t.Fatalf("persona+history must not use the old 6000 cap: %d", localBrainPromptCap)
	}
}

func TestLocalBrainResumeKeepsUserAndAssistant(t *testing.T) {
	dir := t.TempDir()
	writeLocalBrainResume(dir, "做三页封面", "封面标题是月汐")
	raw, err := os.ReadFile(filepath.Join(dir, localBrainLastFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "做三页封面") || !strings.Contains(string(raw), "封面标题是月汐") {
		t.Fatalf("transcript = %s", raw)
	}
	got := readLocalBrainResume(dir)
	if !strings.Contains(got, "上一轮本机大脑") || !strings.Contains(got, "做三页封面") {
		t.Fatalf("resume = %s", got)
	}
}
