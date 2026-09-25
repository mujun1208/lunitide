package codehost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGoplsDefinitionAndMissingImport(t *testing.T) {
	if _, err := toolBin("gopls"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/bound\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "answer.go"), "package bound\n\nfunc Answer() string { return \"right\" }\n")
	mustWrite(t, filepath.Join(dir, "use.go"), "package bound\n\nfunc Use() string {\n\treturn Answer()\n}\n")
	mustWrite(t, filepath.Join(dir, "speak.go"), "package bound\n\nfunc Speak() {\n\tfmt.Println(\"hi\")\n}\n")

	s, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	use := filepath.Join(dir, "use.go")
	text, err := os.ReadFile(use)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Open(use, string(text)); err != nil {
		t.Fatal(err)
	}
	col := strings.Index("\treturn Answer()", "Answer") + 1
	loc, err := s.Definition(use, 4, col)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(loc.Path) != "answer.go" || loc.Line < 1 {
		t.Fatalf("definition = %+v", loc)
	}

	answer := filepath.Join(dir, "answer.go")
	answerText, err := os.ReadFile(answer)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Open(answer, string(answerText)); err != nil {
		t.Fatal(err)
	}
	answerCol := strings.Index("func Answer() string { return \"right\" }", "Answer") + 1
	var refs []Location
	refDeadline := time.Now().Add(20 * time.Second)
	for {
		refs, err = s.References(answer, 3, answerCol)
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) > 0 {
			break
		}
		if time.Now().After(refDeadline) {
			t.Fatal("no references")
		}
		time.Sleep(100 * time.Millisecond)
	}
	foundUse := false
	for _, ref := range refs {
		if filepath.Base(ref.Path) == "use.go" && ref.Line > 0 {
			foundUse = true
		}
	}
	if !foundUse {
		t.Fatalf("references = %+v", refs)
	}

	speak := filepath.Join(dir, "speak.go")
	body, err := os.ReadFile(speak)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Open(speak, string(body)); err != nil {
		t.Fatal(err)
	}
	var diag Diagnostic
	deadline := time.Now().Add(20 * time.Second)
	for {
		items := s.Diagnostics(speak)
		if len(items) > 0 {
			diag = items[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no diagnostics")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if diag.Line < 1 || !strings.Contains(diag.Message, "fmt") {
		t.Fatalf("diagnostic = %+v", diag)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
