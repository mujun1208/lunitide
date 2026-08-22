package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLGenWritesPlayablePreview(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	args := json.RawMessage(`{"path":"games/worldcup.html","title":"世界杯点球大战","template":"penalty-shootout"}`)
	if _, err := r.Execute(context.Background(), Approval, officeSession, "html.gen", args, false); err == nil {
		t.Fatal("html.gen must gate in approval mode")
	}
	out, err := r.Execute(context.Background(), AutoEdit, officeSession, "html.gen", args, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Kind != "html" || out.Artifact.Path != "worldcup.html" {
		t.Fatalf("artifact = %+v", out.Artifact)
	}
	if strings.Contains(out.Artifact.Path, "file:") || strings.Contains(out.Artifact.Content, "{{TITLE}}") {
		t.Fatalf("unsafe artifact path/content: %+v", out.Artifact)
	}
	if !strings.Contains(out.Artifact.Content, "<canvas") {
		t.Fatal("generated HTML is not playable")
	}
	b, err := os.ReadFile(filepath.Join(r.root, officeSession, "games", "worldcup.html"))
	if err != nil || !strings.Contains(string(b), "WORLD CUP") {
		t.Fatalf("file missing: %v", err)
	}
}

func TestHTMLGenDesktopNeedsUnconfined(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	args := json.RawMessage(`{"template":"penalty-shootout","desktop":true}`)
	if _, err := r.Execute(context.Background(), AutoEdit, officeSession, "html.gen", args, false); err == nil {
		t.Fatal("desktop write escaped confined runtime")
	}
}

func TestHTMLGenDesktopArtifactPath(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)
	desktop, err := userDesktopDir()
	if err != nil {
		t.Skip("desktop folder not found:", err)
	}
	target := filepath.Join(desktop, "点球大战.html")
	t.Cleanup(func() { _ = os.Remove(target) })
	args, _ := json.Marshal(map[string]any{
		"path":     "点球大战.html",
		"title":    "世界杯点球大战",
		"template": "penalty-shootout",
		"desktop":  true,
	})
	out, err := r.ExecuteUnconfined(context.Background(), officeSession, "html.gen", args, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Path != "desktop/点球大战.html" {
		t.Fatalf("preview path = %+v", out.Artifact)
	}
	resolved, err := r.ResolveSessionArtifact(officeSession, out.Artifact.Path)
	if err != nil || resolved != target {
		t.Fatalf("ResolveSessionArtifact = %q err=%v want %q", resolved, err, target)
	}
	b, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(b), "canvas") {
		t.Fatalf("desktop write failed: %v", err)
	}
}

func TestWorkspaceWriteHTMLArtifactIsBasename(t *testing.T) {
	r, _ := New(t.TempDir())
	html, err := r.Execute(context.Background(), AutoEdit, officeSession, "workspace.write", json.RawMessage(`{"path":"site/index.html","content":"<h1>preview</h1>"}`), false)
	if err != nil || html.Artifact == nil || html.Artifact.Path != "index.html" || strings.Contains(html.Artifact.Path, "file:") {
		t.Fatalf("html result = %+v err=%v", html, err)
	}
}
