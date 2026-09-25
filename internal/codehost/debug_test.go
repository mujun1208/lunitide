package codehost

import (
	"path/filepath"
	"testing"
)

func TestDelveStopsOnTheTestLine(t *testing.T) {
	if _, err := toolBin("dlv"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/bound\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "answer_test.go"), "package bound\n\nimport \"testing\"\n\nfunc Answer() int { return 1 }\n\nfunc TestStop(t *testing.T) {\n\tn := Answer()\n\tif n != 1 {\n\t\tt.Fatal(n)\n\t}\n}\n")
	file := filepath.Join(dir, "answer_test.go")
	got, err := StopOnLine(dir, file, 8, "TestStop")
	if err != nil {
		t.Fatal(err)
	}
	if got != 8 {
		t.Fatalf("stopped on line %d, want 8", got)
	}
}
