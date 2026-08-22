package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestResolveSessionArtifactTarget(t *testing.T) {
	root := t.TempDir()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sessionDir := filepath.Join(root, session)
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sessionDir, "report.html")
	if err := os.WriteFile(file, []byte("<html></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	e := &Engine{}
	rt, err := toolruntime.New(filepath.Join(root, "legacy"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	e.tools = rt
	e.tools.SetSessionStorageRoot(func() (string, error) { return root, nil })

	got, err := e.resolveSessionArtifactTarget(session, "report.html")
	if err != nil {
		t.Fatal(err)
	}
	if got != file {
		t.Fatalf("expected %q, got %q", file, got)
	}

	if _, err := e.resolveSessionArtifactTarget(session, `..\..\..\windows\system.ini`); err == nil {
		t.Fatal("expected traversal to fail")
	}
	if _, err := e.resolveSessionArtifactTarget(session, `C:\Windows\System.ini`); err == nil {
		t.Fatal("expected absolute path to fail")
	}
}
