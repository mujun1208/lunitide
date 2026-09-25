package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestCodeDiagnosticInjectionKeepsTheFailureLine(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	e.rememberCodeDiagnostic(session, "speak.go:4: undefined: fmt")
	got := e.codeDiagnosticInjection(session)
	if !strings.Contains(got, "speak.go:4") || !strings.Contains(got, "undefined: fmt") || !strings.Contains(got, "不要整文件重写") {
		t.Fatalf("injection = %q", got)
	}
}

func TestCodeWorkspaceDefinitionDiagnosticCompletionAndRestore(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	defer e.closeCodeHosts()
	dir := t.TempDir()
	writeCodeFile(t, filepath.Join(dir, "go.mod"), "module example.com/bound\n\ngo 1.22\n")
	writeCodeFile(t, filepath.Join(dir, "answer.go"), "package bound\n\nfunc Answer() string { return \"right\" }\n")
	writeCodeFile(t, filepath.Join(dir, "use.go"), "package bound\n\nfunc Use() string {\n\treturn Answer()\n}\n")
	writeCodeFile(t, filepath.Join(dir, "speak.go"), "package bound\n\nfunc Speak() {\n\tfmt.Println(\"hi\")\n}\n")
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ctx := context.Background()

	def := e.Handle(ctx, codeRequest(`{"action":"definition","root":`+codeJSON(dir)+`,"path":"use.go","line":4,"column":9}`))
	if !def.OK {
		t.Fatalf("definition: %#v", def.Error)
	}
	var loc struct {
		Path string `json:"path"`
		Line int    `json:"line"`
	}
	raw, _ := json.Marshal(def.Payload)
	if err := json.Unmarshal(raw, &loc); err != nil || filepath.Base(loc.Path) != "answer.go" || loc.Line < 1 {
		t.Fatalf("definition result %s err=%v", raw, err)
	}

	refs := e.Handle(ctx, codeRequest(`{"action":"references","root":`+codeJSON(dir)+`,"path":"answer.go","line":3,"column":6}`))
	refsRaw, _ := json.Marshal(refs.Payload)
	if !refs.OK || !strings.Contains(string(refsRaw), "use.go") {
		t.Fatalf("references %#v %s", refs.Error, refsRaw)
	}

	diag := e.Handle(ctx, codeRequest(`{"action":"diagnostics","root":`+codeJSON(dir)+`,"path":"speak.go","sessionId":"`+session+`"}`))
	if !diag.OK {
		t.Fatalf("diagnostics: %#v", diag.Error)
	}
	diagRaw, _ := json.Marshal(diag.Payload)
	if !strings.Contains(string(diagRaw), "fmt") {
		t.Fatalf("diagnostics = %s", diagRaw)
	}
	if !strings.Contains(e.codeDiagnosticInjection(session), "fmt") {
		t.Fatal("next turn did not receive the diagnostic")
	}

	e.codeRuntime().model = func(context.Context, string, int) (string, error) {
		return "\treturn count", nil
	}
	src := "package p\n\nfunc Total(count int) int {\n\treturn cou\n}\n"
	writeCodeFile(t, filepath.Join(dir, "p.go"), src)
	done := e.Handle(ctx, codeRequest(`{"action":"complete","root":`+codeJSON(dir)+`,"path":"p.go","line":4,"accept":true,"content":`+codeJSON(src)+`}`))
	doneRaw, _ := json.Marshal(done.Payload)
	if !done.OK || !strings.Contains(string(doneRaw), `"source":"model"`) {
		t.Fatalf("complete: %#v %s", done.Error, doneRaw)
	}
	body, err := os.ReadFile(filepath.Join(dir, "p.go"))
	if err != nil || !strings.Contains(string(body), "\treturn count") {
		t.Fatalf("file = %q err=%v", body, err)
	}

	writeCodeFile(t, filepath.Join(dir, "a.txt"), "alpha")
	writeCodeFile(t, filepath.Join(dir, "b.txt"), "beta")
	first := e.Handle(ctx, codeRequest(`{"action":"edit","root":`+codeJSON(dir)+`,"path":"a.txt","content":"ALPHA"}`))
	second := e.Handle(ctx, codeRequest(`{"action":"edit","root":`+codeJSON(dir)+`,"path":"b.txt","content":"BETA"}`))
	if !first.OK || !second.OK {
		t.Fatalf("edit %#v %#v", first.Error, second.Error)
	}
	diffRaw, _ := json.Marshal(second.Payload)
	diff := string(diffRaw)
	if !strings.Contains(diff, "a.txt") || !strings.Contains(diff, "b.txt") || !strings.Contains(diff, "+BETA") {
		t.Fatalf("diff = %s", diff)
	}
	restored := e.Handle(ctx, codeRequest(`{"action":"restore","root":`+codeJSON(dir)+`}`))
	if !restored.OK {
		t.Fatalf("restore %#v", restored.Error)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(a) != "alpha" || string(b) != "beta" {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestCodeWorkspaceDebugStopsOnTheTestLine(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	dir := t.TempDir()
	writeCodeFile(t, filepath.Join(dir, "go.mod"), "module example.com/bound\n\ngo 1.22\n")
	writeCodeFile(t, filepath.Join(dir, "answer_test.go"), "package bound\n\nimport \"testing\"\n\nfunc Answer() int { return 1 }\n\nfunc TestStop(t *testing.T) {\n\tn := Answer()\n\tif n != 1 {\n\t\tt.Fatal(n)\n\t}\n}\n")
	resp := e.Handle(context.Background(), codeRequest(`{"action":"debug","root":`+codeJSON(dir)+`,"path":"answer_test.go","line":8,"test":"TestStop"}`))
	debugRaw, _ := json.Marshal(resp.Payload)
	if !resp.OK || !strings.Contains(string(debugRaw), `"stopped":8`) {
		t.Fatalf("debug %#v %s", resp.Error, debugRaw)
	}
}

func TestCodeAndGitWorkflowNameTheDiagnosticLoop(t *testing.T) {
	got := bundledWorkflowInjection("修这个测试，然后 git commit")
	for _, piece := range []string{"引用结果里的文件名和行号", "不要整文件重写", "接受或还原", "禁止 push", "reset"} {
		if !strings.Contains(got, piece) {
			t.Fatalf("workflow missing %q in %s", piece, got)
		}
	}
}

func TestQuotedFailureBecomesAPassingTest(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	dir := t.TempDir()
	writeCodeFile(t, filepath.Join(dir, "go.mod"), "module example.com/bound\n\ngo 1.22\n")
	writeCodeFile(t, filepath.Join(dir, "answer.go"), "package bound\n\nfunc Answer() string { return \"wrong\" }\n")
	writeCodeFile(t, filepath.Join(dir, "answer_test.go"), "package bound\n\nimport \"testing\"\n\nfunc TestAnswer(t *testing.T) {\n\tif got := Answer(); got != \"right\" {\n\t\tt.Fatalf(\"got %s\", got)\n\t}\n}\n")
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	if err = tools.SetSessionCodeRoot(session, dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = tools.Execute(ctx, toolruntime.FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err == nil {
		t.Fatal("expected the planted failure")
	}
	e.rememberCodeDiagnostic(session, err.Error())
	prompt := "修这个测试" + e.codeDiagnosticInjection(session) + bundledWorkflowInjection("修这个测试")
	reply := quoteFailureLine(prompt)
	if !strings.Contains(reply, "answer_test.go") || !strings.Contains(reply, "got wrong") {
		t.Fatalf("reply = %q", reply)
	}
	edit, err := json.Marshal(map[string]string{"path": "answer.go", "oldText": `return "wrong"`, "newText": `return "right"`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tools.Execute(ctx, toolruntime.FullAccess, session, "workspace.edit", edit, false); err != nil {
		t.Fatal(err)
	}
	out, err := tools.Execute(ctx, toolruntime.FullAccess, session, "command.run", json.RawMessage(`{"argv":["go","test","."]}`), false)
	if err != nil || !strings.Contains(out.Output, "ok:true") {
		t.Fatalf("retest: %v %s", err, out.Output)
	}
}

func quoteFailureLine(prompt string) string {
	if !strings.Contains(prompt, "引用诊断里的文件名和行号") || !strings.Contains(prompt, "不要整文件重写") {
		return ""
	}
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, "answer_test.go") && strings.Contains(line, "got wrong") {
			return line
		}
	}
	return ""
}

func TestCodeWorkspaceAcceptKeepsTheNewBytes(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	dir := t.TempDir()
	writeCodeFile(t, filepath.Join(dir, "a.txt"), "alpha")
	ctx := context.Background()
	edited := e.Handle(ctx, codeRequest(`{"action":"edit","root":`+codeJSON(dir)+`,"path":"a.txt","content":"ALPHA"}`))
	if !edited.OK {
		t.Fatalf("edit %#v", edited.Error)
	}
	accepted := e.Handle(ctx, codeRequest(`{"action":"accept","root":`+codeJSON(dir)+`}`))
	raw, _ := json.Marshal(accepted.Payload)
	if !accepted.OK || !strings.Contains(string(raw), `"accepted":1`) {
		t.Fatalf("accept %#v %s", accepted.Error, raw)
	}
	body, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(body) != "ALPHA" {
		t.Fatalf("file = %q err=%v", body, err)
	}
	restored := e.Handle(ctx, codeRequest(`{"action":"restore","root":`+codeJSON(dir)+`}`))
	body, err = os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || !restored.OK || string(body) != "ALPHA" {
		t.Fatalf("after restore file=%q ok=%v err=%v", body, restored.OK, err)
	}
}

func TestEditorRestoreUndoesTheAgentEdit(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e.tools = tools
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	dir := t.TempDir()
	writeCodeFile(t, filepath.Join(dir, "a.txt"), "alpha")
	ctx := context.Background()
	bound := e.Handle(ctx, codeRequest(`{"action":"diff","root":`+codeJSON(dir)+`,"sessionId":"`+session+`"}`))
	if !bound.OK {
		t.Fatalf("bind %#v", bound.Error)
	}
	edit, err := json.Marshal(map[string]string{"path": "a.txt", "oldText": "alpha", "newText": "ALPHA"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tools.Execute(ctx, toolruntime.FullAccess, session, "workspace.edit", edit, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(body) != "ALPHA" {
		t.Fatalf("after edit = %q err=%v", body, err)
	}
	restored := e.Handle(ctx, codeRequest(`{"action":"restore","root":`+codeJSON(dir)+`,"sessionId":"`+session+`"}`))
	body, err = os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || !restored.OK || string(body) != "alpha" {
		raw, _ := json.Marshal(restored.Payload)
		t.Fatalf("editor restore file=%q ok=%v payload=%s err=%v", body, restored.OK, raw, err)
	}
	edit, err = json.Marshal(map[string]string{"path": "a.txt", "oldText": "alpha", "newText": "BETA"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tools.Execute(ctx, toolruntime.FullAccess, session, "workspace.edit", edit, false); err != nil {
		t.Fatal(err)
	}
	accepted := e.Handle(ctx, codeRequest(`{"action":"accept","root":`+codeJSON(dir)+`,"sessionId":"`+session+`"}`))
	if !accepted.OK {
		t.Fatalf("accept %#v", accepted.Error)
	}
	kept := e.Handle(ctx, codeRequest(`{"action":"restore","root":`+codeJSON(dir)+`,"sessionId":"`+session+`"}`))
	body, err = os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || !kept.OK || string(body) != "BETA" {
		t.Fatalf("after accept file=%q ok=%v err=%v", body, kept.OK, err)
	}
}

func codeRequest(payload string) bridge.Request {
	req := validRequest("code.workspace", payload)
	req.DeadlineMS = 120000
	return req
}

func writeCodeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func codeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
