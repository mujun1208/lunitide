package toolruntime

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCanvasPresentWritesADocumentTheWorkspaceCanShow(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := []byte(`{"title":"能力对照","intro":"落地后的文档","sections":[{"heading":"结论","body":"画布可以看"}],"bars":[{"label":"编辑器","value":8,"max":10}],"html":"<p>补充</p><script>alert(1)</script>"}`)
	out, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", args, true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Kind != "html" || out.Artifact.Path != "canvas.html" {
		t.Fatalf("%+v", out.Artifact)
	}
	page := out.Artifact.Content
	if !strings.Contains(page, "能力对照") || !strings.Contains(page, "画布可以看") || !strings.Contains(page, "width:80.0%") {
		t.Fatalf("document missing the report: %s", page)
	}
	if strings.Contains(strings.ToLower(page), "<script") {
		t.Fatal("canvas kept a script")
	}
	root, err := r.sessionArtifactFile(session, "canvas.html")
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), "能力对照") {
		t.Fatal("canvas.html was not written")
	}
}
