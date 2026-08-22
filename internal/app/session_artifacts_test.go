package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestAppendAndLoadSessionArtifacts(t *testing.T) {
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{tools: tools}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	msg := "01ARZ3NDEKTSV4RRFFQ69G5FBV"
	e.appendMessageArtifacts(session, msg, []SessionArtifact{{
		Kind: "docx", Path: "report.docx", CallID: "call-1", ToolName: "docx.gen",
	}})
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
