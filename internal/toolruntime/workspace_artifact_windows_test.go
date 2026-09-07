//go:build windows

package toolruntime

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestWorkspaceHTMLArtifactKeepsAuthorizedAbsoluteFile(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)
	if err := r.ConfirmFullDiskSession(context.Background(), workspaceDocumentSession); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "deliverable.html")
	args, _ := json.Marshal(map[string]string{"path": target, "content": "<h1>Actual document</h1>"})
	result, err := r.ExecuteUnconfined(context.Background(), workspaceDocumentSession, "workspace.write", args, true)
	if err != nil || result.Artifact == nil || result.Artifact.Path != filepath.ToSlash(target) {
		t.Fatalf("absolute artifact path lost: %+v %v", result, err)
	}
	data, err := r.ReadWorkspaceFile(workspaceDocumentSession, result.Artifact.Path, 4096)
	if err != nil || string(data) != "<h1>Actual document</h1>" {
		t.Fatalf("authorized file could not open: %q %v", data, err)
	}
	// The artifact does not itself grant full-disk access to another session.
	if _, err := r.ReadWorkspaceFile("01ARZ3NDEKTSV4RRFFQ69G5FAA", result.Artifact.Path, 4096); err == nil {
		t.Fatal("artifact path bypassed session approval")
	}
}
