package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestArtifactImagePreviewAndOpenResolveSameFile(t *testing.T) {
	e := newArtifactEngine(t)
	dir, err := e.tools.SessionFolder(artifactSession)
	if err != nil {
		t.Fatal(err)
	}
	var pngBytes bytes.Buffer
	if err = png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "截图 & 报告.png")
	if err = os.WriteFile(path, pngBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"sessionId": artifactSession, "path": filepath.ToSlash(path)})
	preview := handleWorkspaceArtifactPreview(e, context.Background(), artifactRequest(string(payload)))
	if !preview.OK {
		t.Fatalf("preview: %+v", preview)
	}
	raw, _ := json.Marshal(preview.Payload)
	var value struct {
		Kind, Content, AbsolutePath string
		Size                        int
	}
	if json.Unmarshal(raw, &value) != nil || value.Kind != "image" || !strings.HasPrefix(value.Content, "data:image/png;base64,") || value.Size != pngBytes.Len() {
		t.Fatalf("preview metadata invalid: %s", raw)
	}
	original := openArtifactTarget
	t.Cleanup(func() { openArtifactTarget = original })
	calls := 0
	openArtifactTarget = func(target string, file, reveal bool) error {
		calls++
		if !file || filepath.Clean(target) != filepath.Clean(value.AbsolutePath) || reveal != (calls == 2) {
			t.Fatalf("wrong open target: %q, file=%v, reveal=%v", target, file, reveal)
		}
		return nil
	}
	for _, reveal := range []bool{false, true} {
		payload, _ = json.Marshal(map[string]any{"sessionId": artifactSession, "relativePath": filepath.ToSlash(path), "reveal": reveal})
		result := handleSessionFolderOpen(e, context.Background(), artifactRequest(string(payload)))
		if !result.OK {
			t.Fatalf("native opening: %+v", result)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestConcurrentArtifactPersistenceKeepsAllMessageCards(t *testing.T) {
	e := newArtifactEngine(t)
	var workers sync.WaitGroup
	for i := 0; i < 24; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			messageID := fmt.Sprintf("01ARZ3NDEKTSV4RRFFQ69G5F%02d", i)
			e.appendMessageArtifacts(artifactSession, messageID, []SessionArtifact{{Kind: "docx", Path: fmt.Sprintf("v1..%d.docx", i), CallID: fmt.Sprintf("call-%d", i), ToolName: "workspace.write"}})
		}(i)
	}
	workers.Wait()
	// Reconstructing the engine proves cards come from committed disk metadata.
	restarted := &Engine{tools: e.tools}
	if got := len(restarted.loadSessionArtifactsByMessage(artifactSession)); got != 24 {
		t.Fatalf("persisted cards=%d, want 24", got)
	}
}

func TestArtifactAppendPreservesUnreadableExistingIndex(t *testing.T) {
	e := newArtifactEngine(t)
	path := e.sessionArtifactsPath(artifactSession)
	for _, original := range []string{`{"messages":{"old":[`, `{"unexpected":"retain this data"}`} {
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		e.appendMessageArtifacts(artifactSession, "01ARZ3NDEKTSV4RRFFQ69G5FAA", []SessionArtifact{{Kind: "docx", Path: "new.docx", CallID: "new-call", ToolName: "docx.gen"}})
		got, err := os.ReadFile(path)
		if err != nil || string(got) != original {
			t.Fatalf("existing metadata was replaced: %q, error=%v", got, err)
		}
	}
}
