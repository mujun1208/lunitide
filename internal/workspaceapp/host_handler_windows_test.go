//go:build windows

package workspaceapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"golang.org/x/sys/windows"
)

func TestSafePathRejectsTraversalBinaryAndLargeBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := safePath(root, "ok.txt", false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "C:\\Windows\\win.ini"} {
		if _, err := safePath(root, path, false); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
func TestSafePathRejectsSymlinkOrReparse(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := safePath(root, "escape", true); err == nil {
		t.Fatal("accepted symlink escape")
	}
}

func workspaceRequest(method bridge.Method, payload string) bridge.Request {
	return bridge.Request{ID: "request", TraceID: "trace", Method: string(method), Payload: json.RawMessage(payload)}
}

func TestReadRejectsHardLinksAndOversizedFiles(t *testing.T) {
	root := t.TempDir()
	h := New(filepath.Join(t.TempDir(), "root.json"))
	if err := atomicWrite(h.configPath, []byte(`{"path":`+mustJSON(root)+`}`)); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(root, "large.txt")
	if err := os.WriteFile(large, make([]byte, maxPreviewBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRead, `{"path":"large.txt"}`)); response.OK || response.Error.Code != "WORKSPACE_PREVIEW_TOO_LARGE" {
		t.Fatalf("oversized response: %#v", response)
	}
	original := filepath.Join(root, "original.txt")
	if err := os.WriteFile(original, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.txt")
	from, _ := windows.UTF16PtrFromString(original)
	to, _ := windows.UTF16PtrFromString(link)
	if err := windows.CreateHardLink(to, from, 0); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRead, `{"path":"linked.txt"}`)); response.OK || response.Error.Code != "WORKSPACE_FILE_UNSUPPORTED" {
		t.Fatalf("hard-link response: %#v", response)
	}
}

func TestClearUnbindsSavedWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(t.TempDir(), "root.json")
	h := New(config)
	if err := atomicWrite(h.configPath, []byte(`{"path":`+mustJSON(root)+`}`)); err != nil {
		t.Fatal(err)
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRootGet, `{}`)); !response.OK {
		t.Fatalf("get before clear: %#v", response)
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRootClear, `{}`)); !response.OK {
		t.Fatalf("clear: %#v", response)
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRootGet, `{}`)); !response.OK {
		t.Fatalf("get after clear: %#v", response)
	} else {
		body, _ := json.Marshal(response.Payload)
		var parsed struct {
			Bound bool `json:"bound"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil || parsed.Bound {
			t.Fatalf("expected unbound after clear: %#v", response)
		}
	}
	if response := h.HandleHost(t.Context(), workspaceRequest(bridge.MethodWorkspaceRootClear, `{}`)); !response.OK {
		t.Fatalf("clear twice: %#v", response)
	}
}

func TestStrictConfigRejectsUnknownFields(t *testing.T) {
	h := New(filepath.Join(t.TempDir(), "root.json"))
	if err := os.WriteFile(h.configPath, []byte(`{"path":"C:\\","extra":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.root(); err == nil {
		t.Fatal("accepted unknown config field")
	}
}

func mustJSON(value string) string { data, _ := json.Marshal(value); return string(data) }
