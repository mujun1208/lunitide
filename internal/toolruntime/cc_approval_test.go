package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
)

func TestCcInputToolsGateInApprovalMode(t *testing.T) {
	// ccapp only pauses for its own high/critical risks, and it files clicking
	// and typing as medium. The runtime gate never listed cc.* either, so in
	// approval mode a model could click, type and paste on the operator's
	// desktop without a single prompt.
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ran := ""
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		ran = tool
		return ccapp.Outcome{Tool: tool, Summary: tool + " ok"}, nil
	})

	for _, tc := range []struct{ tool, args string }{
		{"cc.mouse_click", `{"button":"left"}`},
		{"cc.mouse_drag", `{"x1":1,"y1":1,"x2":2,"y2":2}`},
		{"cc.keyboard_type", `{"text":"hello"}`},
		{"cc.paste", `{"text":"hi"}`},
		{"cc.press", `{"key":"enter"}`},
		{"cc.menu_click", `{"path":"File > Save"}`},
		{"cc.set_value", `{"target":"Name","value":"Ada"}`},
		{"cc.confirm_dialog", `{"choice":"ok"}`},
		{"cc.clipboard", `{"op":"get"}`},
		{"cc.window_action", `{"op":"close"}`},
		{"cc.app_quit", `{"name":"notepad.exe"}`},
		{"computer.act", `{"action":"click","x":10,"y":10}`},
		{"computer.act", `{"action":"type","text":"hello"}`},
		{"computer.act", `{"action":"paste","text":"hi"}`},
	} {
		ran = ""
		_, err := r.Execute(context.Background(), Approval, "s01", tc.tool, json.RawMessage(tc.args), false)
		if !errors.Is(err, ErrApprovalRequired) {
			t.Fatalf("%s %s needs approval; got err=%v", tc.tool, tc.args, err)
		}
		if ran != "" {
			t.Fatalf("%s reached the executor as %q before approval", tc.tool, ran)
		}
	}
}

func TestCcObservationStaysUngated(t *testing.T) {
	// see→act→verify screenshots before every click and again after it.
	// Gating observation would put an approval prompt in front of looking,
	// which is the one thing the loop has to be able to do freely.
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{Tool: tool, Summary: tool + " ok"}, nil
	})

	for _, tc := range []struct{ tool, args string }{
		{"cc.screen_capture", `{}`},
		{"cc.get_active_window", `{}`},
		{"cc.window_list", `{}`},
		{"cc.observe_ui", `{}`},
		{"cc.observe_dialog", `{}`},
		{"cc.app_list", `{}`},
		{"cc.wait", `{"ms":1}`},
		{"cc.mouse_move", `{"x":5,"y":5}`},
		{"computer.act", `{"action":"screenshot"}`},
		{"computer.act", `{"action":"observe"}`},
		{"computer.act", `{"action":"list"}`},
	} {
		if _, err := r.Execute(context.Background(), Approval, "s01", tc.tool, json.RawMessage(tc.args), false); err != nil {
			t.Fatalf("%s %s must not need approval: %v", tc.tool, tc.args, err)
		}
	}
}

func TestComputerActUnparseableActionFailsClosed(t *testing.T) {
	// An action ccapp cannot map is treated as mutating: ccapp would refuse it
	// anyway, and guessing "harmless" is the wrong way to be wrong.
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetCcExecutor(func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error) {
		return ccapp.Outcome{Tool: tool, Summary: "ok"}, nil
	})
	_, err = r.Execute(context.Background(), Approval, "s01", "computer.act", json.RawMessage(`{"action":"not-a-real-action"}`), false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("unmappable computer.act should gate; got %v", err)
	}
}
