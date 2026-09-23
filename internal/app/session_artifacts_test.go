package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestSessionArtifactFromToolBindsOfficeTask(t *testing.T) {
	task := "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	got := sessionArtifactFromTool("call-1", "pptx.gen", "pptx", "deck.pptx", task)
	if got.OfficeTaskID != task || got.Kind != "pptx" || got.Path != "deck.pptx" || got.CallID != "call-1" || got.ToolName != "pptx.gen" {
		t.Fatalf("chat card lost the bound office task: %+v", got)
	}
	unbound := sessionArtifactFromTool("call-2", "docx.gen", "docx", "note.docx", "")
	if unbound.OfficeTaskID != "" {
		t.Fatalf("empty task leaked onto an unbound card: %+v", unbound)
	}
}

func TestChatDeliverableArtifact(t *testing.T) {
	if chatDeliverableArtifact("web.search", "html", "search.html") {
		t.Fatal("web.search html must not be a chat deliverable")
	}
	if chatDeliverableArtifact("web.fetch", "html", "fetch.html") {
		t.Fatal("web.fetch html must not be a chat deliverable")
	}
	if !chatDeliverableArtifact("pptx.gen", "pptx", "deck.pptx") {
		t.Fatal("pptx.gen must be a chat deliverable")
	}
	if !chatDeliverableArtifact("office.generate", "docx", "office/周报-abc123.docx") {
		t.Fatal("office.generate Word must be a chat deliverable")
	}
	if !chatDeliverableArtifact("workspace.write", "html", "index.html") {
		t.Fatal("user html pages must be deliverables")
	}
	if !chatDeliverableArtifact("workspace.write", "md", "周报/周报_2026-W37.md") {
		t.Fatal("skill markdown writes must be chat deliverables")
	}
	if !chatDeliverableArtifact("workspace.edit", "txt", "notes.txt") {
		t.Fatal("edited text files must be chat deliverables")
	}
	if !chatDeliverableArtifact("audio.generate", "audio", "song.wav") {
		t.Fatal("generated speech must be a chat deliverable")
	}
}

func TestLoadSessionArtifactsDoesNotCreateAnEmptyFolder(t *testing.T) {
	root := t.TempDir()
	tools, err := toolruntime.New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if got := e.loadSessionArtifactsByMessage(session); len(got) != 0 {
		t.Fatalf("artifacts = %#v", got)
	}
	if _, err := os.Stat(filepath.Join(root, session)); !os.IsNotExist(err) {
		t.Fatal("reading artifact cards created the conversation folder")
	}
}

func TestAppendAndLoadSessionArtifacts(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	msg := "01ARZ3NDEKTSV4RRFFQ69G5FBV"
	e.appendMessageArtifacts(session, msg, []SessionArtifact{
		{Kind: "docx", Path: "report.docx", CallID: "call-1", ToolName: "docx.gen"},
		{Kind: "html", Path: "search.html", CallID: "call-2", ToolName: "web.search"},
	})
	byMsg := e.loadSessionArtifactsByMessage(session)
	if len(byMsg[msg]) != 1 || byMsg[msg][0].Path != "report.docx" {
		t.Fatalf("artifacts = %#v", byMsg)
	}
	path := e.sessionArtifactsPath(session)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	if filepath.Base(path) != ".message-artifacts.json" {
		t.Fatalf("path = %q", path)
	}
}
