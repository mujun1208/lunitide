package agenthub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKimiSkillDirsFindsSlides(t *testing.T) {
	root := t.TempDir()
	slides := filepath.Join(root, "kimi-slides")
	if err := os.MkdirAll(slides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slides, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := filterKimiSkillRoots([]string{root, filepath.Join(root, "missing")})
	if len(got) != 1 || got[0] != root {
		t.Fatalf("%v", got)
	}
}
