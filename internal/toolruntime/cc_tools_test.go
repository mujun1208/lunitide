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
