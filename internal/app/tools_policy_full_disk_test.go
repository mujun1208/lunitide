// E2E pin for the settings-plane toggle: tools.commandPolicy.set must
// accept the fullAccess flag through the bridge schema layer (the
// generated schema embeds it), persist it and hot-apply it so a
// full-access conversation immediately reaches the unconfined runtime.
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestCommandPolicySetFullAccessE2E(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)

	resp := e.Handle(context.Background(), validRequest("tools.commandPolicy.set", `{"commands":[],"fullAccess":true}`))
	if !resp.OK {
		t.Fatalf("set with fullAccess rejected: %+v", resp.Error)
	}
	if !tools.FullDiskEnabled() {
		t.Fatal("fullAccess flag not hot-applied")
	}

	get := e.Handle(context.Background(), validRequest("tools.commandPolicy.get", `{}`))
	if !get.OK {
		t.Fatalf("get failed: %+v", get.Error)
	}

	off := e.Handle(context.Background(), validRequest("tools.commandPolicy.set", `{"commands":[],"fullAccess":false}`))
	if !off.OK {
		t.Fatalf("set fullAccess=false rejected: %+v", off.Error)
	}
	if tools.FullDiskEnabled() {
		t.Fatal("fullAccess=false did not clear the flag")
	}
}

// TestFullDiskChatWritesAbsolutePaths pins the user-visible acceptance: the
// settings toggle on + a full-access conversation must let workspace.write
// create a file at any absolute path (the desktop case) through the very
// routing chat.start uses.
func TestFullDiskChatWritesAbsolutePaths(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)

	// By default (no policy file), full-access mode allows absolute paths.
	desktop := filepath.Join(t.TempDir(), "Desktop", "note.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(desktop), "content": "hi"})
	if _, err = e.executeUserTool(context.Background(), executionModeFullAccess, "s01", "workspace.write", args); err != nil {
		t.Fatalf("full-access default absolute write: %v", err)
	}
	if b, rerr := os.ReadFile(desktop); rerr != nil || string(b) != "hi" {
		t.Fatalf("desktop write content = %q err=%v", string(b), rerr)
	}

	// Explicitly opting out of full-disk should confine again.
	resp := e.Handle(context.Background(), validRequest("tools.commandPolicy.set", `{"commands":[],"fullAccess":false}`))
	if !resp.OK {
		t.Fatalf("set fullAccess=false rejected: %+v", resp.Error)
	}
	desktop2 := filepath.Join(t.TempDir(), "Desktop", "note2.txt")
	args2, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(desktop2), "content": "hi"})
	if _, err = e.executeUserTool(context.Background(), executionModeFullAccess, "s01", "workspace.write", args2); err == nil {
		t.Fatal("full-access with explicit opt-out escaped confinement")
	}
}
