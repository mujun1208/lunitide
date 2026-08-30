package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestCcToolsUnavailableWithoutExecutor(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, err = r.Execute(context.Background(), FullAccess, "s01", "cc.mouse_move", json.RawMessage(`{"x":10,"y":10}`), true)
	if err == nil || !strings.Contains(err.Error(), "M10-CC-010") {
		t.Fatalf("expected M10-CC-010 unavailable error, got %v", err)
	}
}

func TestCcToolsPlanModeExcluded(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		t.Fatal("cc executor must not run in plan mode")
		return ccapp.Outcome{}, nil
	})
	_, err = r.Execute(context.Background(), Plan, "s01", "cc.screen_capture", json.RawMessage(`{}`), true)
	if err == nil || !strings.Contains(err.Error(), "plan mode") {
		t.Fatalf("expected plan-mode refusal, got %v", err)
	}
}

func TestCcToolApprovalGate(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{}, ccapp.ErrCcConfirmRequired
	})
	_, err = r.Execute(context.Background(), Approval, "s01", "cc.keyboard_shortcut", json.RawMessage(`{"keys":["ctrl","s"]}`), false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}
}

func TestCcToolErrorCarriesWireCode(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{}, ccapp.ErrCcDisabled
	})
	_, err = r.Execute(context.Background(), FullAccess, "s01", "cc.mouse_click", json.RawMessage(`{"button":"left"}`), true)
	if err == nil || !strings.Contains(err.Error(), "M10-CC-012") {
		t.Fatalf("expected M10-CC-012 disabled error, got %v", err)
	}
}

func TestCcScreenCapturePersistsArtifact(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		if tool != "cc.screen_capture" {
			return ccapp.Outcome{}, ccapp.ErrCcSchema
		}
		return ccapp.Outcome{Tool: tool, Summary: "captured screen (8 bytes png)", CapturePNG: png}, nil
	})
	out, err := r.Execute(context.Background(), Approval, session, "cc.screen_capture", json.RawMessage(`{}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Kind != "image" || !strings.HasSuffix(out.Artifact.Path, ".png") {
		t.Fatalf("expected png artifact, got %+v", out.Artifact)
	}
	if out.VisionMIME != "image/png" || len(out.VisionData) != len(png) {
		t.Fatalf("expected vision passthrough, mime=%s n=%d", out.VisionMIME, len(out.VisionData))
	}
	data, err := os.ReadFile(filepath.Join(root, session, out.Artifact.Path))
	if err != nil || len(data) != len(png) {
		t.Fatalf("artifact bytes mismatch: %v %d", err, len(data))
	}
}

func TestCcToolPlainOutcome(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{Tool: tool, Summary: "active window: Notes (process: notepad.exe)"}, nil
	})
	out, err := r.Execute(context.Background(), FullAccess, "s01", "cc.get_active_window", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact != nil || !strings.Contains(out.Output, "notepad.exe") {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

func TestCcNewToolsRouteThroughExecutor(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	seen := map[string]bool{}
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		seen[tool] = true
		return ccapp.Outcome{Tool: tool, Summary: tool + " ok"}, nil
	})
	for _, tool := range []string{"cc.mouse_drag", "cc.window_list", "cc.window_focus", "cc.observe_ui", "cc.wait", "cc.clipboard", "cc.window_action", "cc.app_list", "cc.app_quit", "cc.paste", "cc.press", "cc.menu_click", "cc.set_value", "computer.act"} {
		args := json.RawMessage(`{}`)
		switch tool {
		case "cc.mouse_drag":
			args = json.RawMessage(`{"x1":1,"y1":1,"x2":2,"y2":2}`)
		case "cc.window_focus":
			args = json.RawMessage(`{"title":"Notepad"}`)
		case "cc.clipboard":
			args = json.RawMessage(`{"op":"get"}`)
		case "cc.wait":
			args = json.RawMessage(`{"ms":1}`)
		case "cc.window_action":
			args = json.RawMessage(`{"op":"restore"}`)
		case "cc.app_quit":
			args = json.RawMessage(`{"name":"notepad.exe"}`)
		case "cc.paste":
			args = json.RawMessage(`{"text":"hi"}`)
		case "cc.press":
			args = json.RawMessage(`{"key":"enter"}`)
		case "cc.menu_click":
			args = json.RawMessage(`{"path":"File > Save"}`)
		case "cc.set_value":
			args = json.RawMessage(`{"target":"Name","value":"Ada"}`)
		case "computer.act":
			args = json.RawMessage(`{"action":"list"}`)
		}
		if _, err := r.Execute(context.Background(), FullAccess, "s01", tool, args, true); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	if len(seen) != 14 {
		t.Fatalf("routed %v", seen)
	}
}

func TestCcClickVerifyPersistsScreenshotArtifact(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{Tool: tool, Summary: "clicked left mouse 1 time(s); screen updated 100x100 (use image 100x100)", CapturePNG: png}, nil
	})
	out, err := r.Execute(context.Background(), Approval, session, "cc.mouse_click", json.RawMessage(`{"button":"left"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Kind != "image" {
		t.Fatalf("verify screenshot should appear as image artifact, got %+v", out.Artifact)
	}
	if len(out.VisionData) == 0 {
		t.Fatal("verify screenshot should be passed to the model")
	}
}

func TestCcObserveDialogRoutesThroughExecutor(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		if tool != "cc.observe_dialog" {
			t.Fatalf("unexpected tool %s", tool)
		}
		return ccapp.Outcome{Tool: tool, Summary: `{"count":0,"dialogs":[]}`}, nil
	})
	out, err := r.Execute(context.Background(), FullAccess, "s01", "cc.observe_dialog", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, `"count":0`) {
		t.Fatalf("unexpected observe output: %s", out.Output)
	}
}
