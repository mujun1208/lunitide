package projecttree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRejectsEscapeAndAcceptsDefault(t *testing.T) {
	if _, err := Parse([]byte(`{"version":1,"dirs":["../x"],"phaseMap":{"1":"../x"},"codeRoot":"../x"}`)); err == nil {
		t.Fatal("accepted ..")
	}
	if _, err := Parse([]byte(`{"version":1,"dirs":["C:/abs"],"phaseMap":{"1":"C:/abs"},"codeRoot":"C:/abs"}`)); err == nil {
		t.Fatal("accepted drive")
	}
	tree, err := Default(false)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(tree.Dirs, "src") || tree.CodeRoot != "src" || tree.PhaseMap["5"] != "src" {
		t.Fatalf("impl default: %+v", tree)
	}
	ops, err := Default(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ops.Dirs {
		if strings.Contains(d, "方案和UI") || strings.Contains(d, "07-集成") {
			t.Fatalf("ops tree still has skipped dir %q", d)
		}
	}
	if ops.PhaseMap["4"] != "src" {
		t.Fatalf("ops phaseMap: %+v", ops.PhaseMap)
	}
}

func TestMaterializeCreatesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	tree, err := Default(false)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Materialize(root, tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Failed) > 0 {
		t.Fatalf("failed: %+v", receipt.Failed)
	}
	if _, err := os.Stat(filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".lunitide", "project-tree.json")); err != nil {
		t.Fatal(err)
	}
	again, err := Materialize(root, tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Created) != 0 {
		t.Fatalf("second pass created %v", again.Created)
	}
}

func TestExportCopyVersionsInsteadOfOverwrite(t *testing.T) {
	root := t.TempDir()
	first, err := ExportCopy(root, "docs/03-数据库", "db_design-设计.md", []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportCopy(root, "docs/03-数据库", "db_design-设计.md", []byte("v2"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("overwrote existing file")
	}
	got, err := os.ReadFile(first)
	if err != nil || string(got) != "v1" {
		t.Fatalf("original changed: %s %v", got, err)
	}
	got, err = os.ReadFile(second)
	if err != nil || string(got) != "v2" {
		t.Fatalf("versioned write: %s %v", got, err)
	}
}

func contains(dirs []string, want string) bool {
	for _, d := range dirs {
		if d == want {
			return true
		}
	}
	return false
}
