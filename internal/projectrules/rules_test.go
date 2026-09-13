package projectrules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeWritesRulesAndPreservesOuterAgents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("USER KEEP\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	man, err := Materialize(root, Input{
		ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		DevStandard: "开发必须先写测试。",
		TechStandard: "只用 SQLite。",
		At: "2026-09-13T00:00:00Z",
	})
	if err != nil || man.Digest == "" {
		t.Fatalf("materialize: %+v %v", man, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "USER KEEP") || !strings.Contains(text, "开发必须先写测试") || !strings.HasPrefix(text, "USER KEEP") {
		t.Fatalf("agents: %s", text)
	}
	if g := Guidance(root); !strings.Contains(g, "只用 SQLite") {
		t.Fatalf("guidance: %s", g)
	}
	man2, err := Materialize(root, Input{ProjectID: man.ProjectID, DevStandard: "改过的规范", TechStandard: "只用 SQLite。", At: "2026-09-13T01:00:00Z"})
	if err != nil || man2.Digest == man.Digest {
		t.Fatalf("digest should change: %v %s %s", err, man.Digest, man2.Digest)
	}
	body, _ = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(string(body), "USER KEEP") || !strings.Contains(string(body), "改过的规范") {
		t.Fatalf("rewrite: %s", body)
	}
}

func TestMaterializeRequiresRoot(t *testing.T) {
	if _, err := Materialize("", Input{}); err != ErrRulesFailed {
		t.Fatalf("empty root: %v", err)
	}
}
