package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// enableFullDisk persists and hot-applies the full-disk opt-in document.
func enableFullDisk(t *testing.T, r *Runtime) {
	t.Helper()
	if err := r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	if !r.FullDiskEnabled() {
		t.Fatal("fullAccess flag not applied")
	}
}

func TestOpenWithoutPolicyFileKeepsFullDiskOff(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.FullDiskEnabled() {
		t.Fatal("missing command-policy.json must not enable fullAccess")
	}
}

func TestFullDiskPolicyLoadAndHotApply(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "command-policy.json"), []byte(`{"commands":[],"fullAccess":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if !r.FullDiskEnabled() {
		t.Fatal("startup load missed fullAccess")
	}
	if err := r.SetCommandPolicyJSON([]byte(`{"commands":[]}`)); err != nil {
		t.Fatal(err)
	}
	if r.FullDiskEnabled() {
		t.Fatal("hot-apply did not clear fullAccess")
	}
	if err := r.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	if !r.FullDiskEnabled() {
		t.Fatal("hot-apply did not restore fullAccess")
	}
}

func TestFullDiskUnconfinedWriteAndCommand(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	enableFullDisk(t, r)

	outside := filepath.Join(t.TempDir(), "可可", "note.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(outside), "content": "hello"})
	if _, err := r.ExecuteUnconfined(context.Background(), s, "workspace.write", args, false); err != nil {
		t.Fatalf("unconfined absolute write: %v", err)
	}
	b, err := os.ReadFile(outside)
	if err != nil || string(b) != "hello" {
		t.Fatalf("absolute write content = %q err=%v", string(b), err)
	}

	// "go env" is not in the builtin allowlist; unconfined must still run it.
	res, err := r.ExecuteUnconfined(context.Background(), s, "command.run", json.RawMessage(`{"argv":["go","env"]}`), false)
	if err != nil {
		t.Fatalf("unconfined go env: %v", err)
	}
	if !strings.Contains(res.Output, "GOPATH") && !strings.Contains(res.Output, "GO") {
		t.Fatalf("go env output missing GO vars: %q", res.Output)
	}
}

func TestFullDiskConfinementHoldsOnConfinedEntry(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	enableFullDisk(t, r)

	// The confined entry point (subagents, delegation, approvals) must keep
	// both gates even with the opt-in persisted: FullAccess mode alone is
	// approval-skipping, not an unconfined grant.
	if _, err := r.Execute(context.Background(), FullAccess, s, "command.run", json.RawMessage(`{"argv":["go","env"]}`), false); err == nil {
		t.Fatal("confined Execute allowed unlisted command under fullDisk")
	}
	outside := filepath.Join(t.TempDir(), "escape.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(outside), "content": "x"})
	if _, err := r.Execute(context.Background(), FullAccess, s, "workspace.write", args, false); err == nil || !strings.Contains(err.Error(), "relative path required") {
		t.Fatalf("confined Execute accepted absolute path: %v", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("confined Execute wrote outside the sandbox")
	}
}

func TestFullDiskOffKeepsRelativeOnly(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	outside := filepath.Join(t.TempDir(), "escape.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(outside), "content": "x"})
	if _, err := r.ExecuteUnconfined(context.Background(), s, "workspace.write", args, false); err == nil || !strings.Contains(err.Error(), "relative path required") {
		t.Fatalf("unconfined write without opt-in escaped: %v", err)
	}
	if _, err := r.ExecuteUnconfined(context.Background(), s, "command.run", json.RawMessage(`{"argv":["go","env"]}`), false); err == nil {
		t.Fatal("unconfined command without opt-in bypassed the allowlist")
	}
}
