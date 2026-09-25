package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevCommandsStayOffTheGlobalAllowlist(t *testing.T) {
	rules := builtinCommandRules()
	for _, argv := range [][]string{
		{"go", "test", "."},
		{"go", "fmt", "."},
		{"npm", "test"},
	} {
		if _, ok := matchCommandRule(rules, argv); ok {
			t.Fatalf("global allowlist permitted %v", argv)
		}
		if _, ok := matchCommandRule(devCommandRules(), argv); !ok {
			t.Fatalf("bound rules rejected %v", argv)
		}
	}
	if _, ok := matchCommandRule(devCommandRules(), []string{"go", "test", "-exec", "cmd", "."}); !ok {
		t.Fatal("prefix match is separate from the -exec block")
	}
}

func TestDevCommandsRunOnlyInsideABoundRoot(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	proj := t.TempDir()
	writeBoundModule(t, proj, "right")

	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false); err == nil || !strings.Contains(err.Error(), "command denied") {
		t.Fatalf("unbound go test err = %v", err)
	}

	r.SetProjectRootResolver(func(string) (string, error) { return proj, nil })
	out, err := r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err != nil {
		t.Fatalf("bound go test: %v", err)
	}
	if !strings.Contains(out.Output, "ok:true") {
		t.Fatalf("output = %q", out.Output)
	}

	if _, err = r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","-exec","cmd","."]}`), false); err == nil || !strings.Contains(err.Error(), "command denied") {
		t.Fatalf("go test -exec err = %v", err)
	}
}

func TestBoundGoTestFailureNamesTheLine(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	proj := t.TempDir()
	writeBoundModule(t, proj, "wrong")
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetFullAccessRootResolver(func() (string, error) { return proj, nil })
	_, err = r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err == nil {
		t.Fatal("expected a failing test")
	}
	text := err.Error()
	if !strings.Contains(text, "answer_test.go") || !strings.Contains(text, "got wrong") {
		t.Fatalf("failure text missing the test line: %s", text)
	}
}

func TestBoundGoTestReportsAMissingImportLine(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "go.mod"), []byte("module example.com/bound\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	src := "package bound\n\nfunc Speak() {\n\tfmt.Println(\"hi\")\n}\n"
	if err := os.WriteFile(filepath.Join(proj, "speak.go"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetProjectRootResolver(func(string) (string, error) { return proj, nil })
	_, err = r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err == nil {
		t.Fatal("expected a compile error")
	}
	text := err.Error()
	if !strings.Contains(text, "speak.go") || !strings.Contains(text, "undefined: fmt") {
		t.Fatalf("diagnostic missing the import line: %s", text)
	}
}

func TestSessionCodeRootConfinesTheEditAndTheTest(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	proj := t.TempDir()
	writeBoundModule(t, proj, "right")
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetSessionCodeRoot(session, proj); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err != nil || !strings.Contains(out.Output, "ok:true") {
		t.Fatalf("code root go test: %v %s", err, out.Output)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.write", json.RawMessage(`{"path":"note.txt","content":"inside"}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(proj, "note.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestEditCheckpointRestoresBothFiles(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	write := func(path, content string) {
		t.Helper()
		raw, e := json.Marshal(map[string]string{"path": path, "content": content})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = r.Execute(ctx, FullAccess, session, "workspace.write", raw, false); e != nil {
			t.Fatal(e)
		}
	}
	write("a.txt", "alpha")
	write("b.txt", "beta")
	edit := json.RawMessage(`{"files":[{"path":"a.txt","oldText":"alpha","newText":"ALPHA"},{"path":"b.txt","oldText":"beta","newText":"BETA"}]}`)
	edited, err := r.Execute(ctx, FullAccess, session, "workspace.edit", edit, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []string{"--- a.txt", "+++ a.txt", "-alpha", "+ALPHA", "--- b.txt", "-beta", "+BETA"} {
		if !strings.Contains(edited.Output, piece) {
			t.Fatalf("diff missing %q in %s", piece, edited.Output)
		}
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.restore", json.RawMessage(`{}`), false); err != nil {
		t.Fatal(err)
	}
	root, err := r.SessionFolder(session)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, want string }{{"a.txt", "alpha"}, {"b.txt", "beta"}} {
		got, e := os.ReadFile(filepath.Join(root, c.path))
		if e != nil {
			t.Fatal(e)
		}
		if string(got) != c.want {
			t.Fatalf("%s = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestCodeRootRejectsAbsoluteWritesOutsideTheFolder(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	proj := t.TempDir()
	outside := t.TempDir()
	writeBoundModule(t, proj, "right")
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	if err = r.SetSessionCodeRoot(session, proj); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "nope.txt")
	raw, err := json.Marshal(map[string]string{"path": target, "content": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ExecuteUnconfined(ctx, session, "workspace.write", raw, true); err == nil || !strings.Contains(err.Error(), "代码目录") {
		t.Fatalf("outside write err = %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("outside file stat = %v", statErr)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.write", json.RawMessage(`{"path":"note.txt","content":"inside"}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(proj, "note.txt")); err != nil {
		t.Fatal(err)
	}
	for _, argv := range []string{
		`{"argv":["git","--no-pager","push"]}`,
		`{"argv":["git","--no-pager","reset","--hard"]}`,
	} {
		if _, err = r.Execute(ctx, FullAccess, session, "command.run", json.RawMessage(argv), false); err == nil || !strings.Contains(err.Error(), "command denied") {
			t.Fatalf("argv %s err = %v", argv, err)
		}
	}
}

func TestAcceptKeepsTheBytesAndDropsTheCheckpoint(t *testing.T) {
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	raw, err := json.Marshal(map[string]string{"path": "a.txt", "content": "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.write", raw, false); err != nil {
		t.Fatal(err)
	}
	edited, err := r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"a.txt","oldText":"alpha","newText":"ALPHA"}`), false)
	if err != nil || !strings.Contains(edited.Output, "+ALPHA") {
		t.Fatalf("edit = %v %s", err, edited.Output)
	}
	accepted, err := r.Execute(ctx, FullAccess, session, "workspace.accept", json.RawMessage(`{}`), false)
	if err != nil || !strings.Contains(accepted.Output, "accepted 1") {
		t.Fatalf("accept = %v %s", err, accepted.Output)
	}
	root, err := r.SessionFolder(session)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(got) != "ALPHA" {
		t.Fatalf("file = %q err=%v", got, err)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.restore", json.RawMessage(`{}`), false); err == nil || !strings.Contains(err.Error(), "nothing to restore") {
		t.Fatalf("restore after accept = %v", err)
	}
	got, err = os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(got) != "ALPHA" {
		t.Fatalf("file after empty restore = %q err=%v", got, err)
	}
}

func TestAcceptCompletionWritesTheLine(t *testing.T) {
	src := "package p\n\nfunc Total(count int) int {\n\treturn cou\n}\n"
	line, ok := CompleteSourceLine(src, 3)
	if !ok || line != "\treturn count" {
		t.Fatalf("suggestion = %q ok=%v", line, ok)
	}
	if _, ok = CompleteSourceLine("package p\nfunc F(count, cost int) int {\n\treturn c\n}\n", 2); ok {
		t.Fatal("ambiguous prefix must not guess")
	}
	if _, ok = CompleteSourceLine(src, 0); ok {
		t.Fatal("a finished name must not be rewritten")
	}
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	updated := applySourceLine(src, 3, line)
	raw, err := json.Marshal(map[string]string{"path": "p.go", "content": updated})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.write", raw, false); err != nil {
		t.Fatal(err)
	}
	root, err := r.SessionFolder(session)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "p.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\treturn count\n") {
		t.Fatalf("file = %q", got)
	}
}

func writeBoundModule(t *testing.T, dir, answer string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/bound\n\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	body := "package bound\n\nfunc Answer() string { return " + jsonString(answer) + " }\n"
	if err := os.WriteFile(filepath.Join(dir, "answer.go"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	test := "package bound\n\nimport \"testing\"\n\nfunc TestAnswer(t *testing.T) {\n\tif got := Answer(); got != \"right\" {\n\t\tt.Fatalf(\"got %s\", got)\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "answer_test.go"), []byte(test), 0600); err != nil {
		t.Fatal(err)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
