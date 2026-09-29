package toolruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteArtifactForPathSurfacesEveryUserFacingFile(t *testing.T) {
	md := writeArtifactForPath("周报/周报_2026-W37.md", "# 周报")
	if md == nil || md.Kind != "md" || md.Path != "周报/周报_2026-W37.md" || md.Content != "" {
		t.Fatalf("markdown = %+v", md)
	}
	html := writeArtifactForPath("site/index.html", "<h1>ok</h1>")
	if html == nil || html.Kind != "html" || html.Content != "<h1>ok</h1>" {
		t.Fatalf("html = %+v", html)
	}
	// A POC is more than its entry page: the supporting code files the model
	// writes are user deliverables too, so they must surface as cards.
	for path, want := range map[string]string{
		"poc/app.js":        "code",
		"poc/style.css":     "code",
		"tools/analyze.py":  "code",
		"db/schema.sql":     "code",
		"poc/config.json":   "code",
		"assets/hero.jpg":   "image",
		"assets/anim.gif":   "image",
		"assets/photo.jpeg": "image",
	} {
		if got := writeArtifactForPath(path, "x"); got == nil || got.Kind != want || got.Content != "" {
			t.Fatalf("%s = %+v, want kind %q", path, got, want)
		}
	}
	if got := writeArtifactForPath(".message-artifacts.json", "{}"); got != nil {
		t.Fatalf("hidden file = %+v", got)
	}
	if got := writeArtifactForPath("binary.zip", "x"); got != nil {
		t.Fatalf("non-previewable binary = %+v", got)
	}
}

func TestArtifactReadUsesAuthorizedActualPath(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	dir, err := r.SessionFolder(session)
	if err != nil {
		t.Fatal(err)
	}
	project, outside := t.TempDir(), t.TempDir()
	r.SetFullAccessRootResolver(func() (string, error) { return project, nil })
	for root, text := range map[string]string{dir: "session", project: "project", outside: "outside"} {
		if err := os.WriteFile(filepath.Join(root, "报告.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range map[string]string{"报告.txt": "session", filepath.Join(project, "报告.txt"): "project", filepath.Join(dir, "报告.txt"): "session"} {
		data, err := r.ReadWorkspaceFile(session, path, 64)
		if err != nil || string(data) != want {
			t.Fatalf("read %q = %q, %v", path, data, err)
		}
	}
	for _, path := range []string{filepath.Join(outside, "报告.txt"), "../报告.txt"} {
		if _, err := r.ReadWorkspaceFile(session, path, 64); err == nil {
			t.Fatalf("outside file accepted: %q", path)
		}
	}
	if _, err := r.ReadWorkspaceFile(session, "报告.txt", 2); err == nil {
		t.Fatal("oversized file silently truncated")
	}
}
