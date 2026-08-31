package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestLooksLikeFilePickerToolResult(t *testing.T) {
	if !looksLikeFilePickerToolResult(`{"needs_user":"x","handoff":"file_dialog"}`) {
		t.Fatal("json file_dialog")
	}
	if !looksLikeFilePickerToolResult("needs_user: 请你点「保存」「打开」或「取消」，我不能代你选文件") {
		t.Fatal("spoken needs_user")
	}
	if looksLikeFilePickerToolResult("captured desktop 1920x1080") {
		t.Fatal("screenshot is not a file picker")
	}
}

func TestLooksLikeUACToolResult(t *testing.T) {
	if !looksLikeUACToolResult(ccappUACPrompt()) {
		t.Fatal("spoken UAC needs_user")
	}
	if !looksLikeUACToolResult("ccapp: operation blocked by risk policy: uac dialog") {
		t.Fatal("uac dialog")
	}
	if looksLikeUACToolResult("captured desktop 1920x1080") {
		t.Fatal("screenshot is not UAC")
	}
}

func ccappUACPrompt() string {
	return "needs_user: 这是系统提权对话框，我不能代点「是」。请你自己确认或取消。"
}

func TestParkUACAskEmitsUserAsk(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	e := NewEngine(nil, "test")
	e.SetToolRuntime(runtime)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	var events []bridge.Event
	if err := e.parkUACAsk(context.Background(), session, session, executionModeApproval, func(ev bridge.Event) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != bridge.EventApprovalRequired || events[0].Tool == nil {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Tool.Name != "user.ask" {
		t.Fatalf("name = %s", events[0].Tool.Name)
	}
	if !strings.Contains(events[0].Tool.Summary, "系统提权") {
		t.Fatalf("summary = %q", events[0].Tool.Summary)
	}
}

func TestParkFilePickerAskEmitsUserAsk(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	e := NewEngine(nil, "test")
	e.SetToolRuntime(runtime)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	var events []bridge.Event
	if err := e.parkFilePickerAsk(context.Background(), session, session, executionModeApproval, func(ev bridge.Event) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != bridge.EventApprovalRequired || events[0].Tool == nil {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Tool.Name != "user.ask" {
		t.Fatalf("name = %s", events[0].Tool.Name)
	}
	if events[0].Tool.ArgsDigest == "" {
		t.Fatal("missing args digest")
	}
	if !strings.Contains(events[0].Tool.Summary, "文件对话框") {
		t.Fatalf("summary = %q", events[0].Tool.Summary)
	}
}

func TestInvokeExpertCreateInvalidJSONRetry(t *testing.T) {
	e := NewEngine(nil, "test")
	_, err := e.invokeExpertCreateTool(context.Background(), "s", json.RawMessage(`not-json`))
	if err == nil || !strings.Contains(err.Error(), "retry:") || !strings.Contains(err.Error(), "expert.create") {
		t.Fatalf("err = %v", err)
	}
}

func TestInjectedGuidanceDigestHashesSystem(t *testing.T) {
	req := gateway.Request{
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: "alpha"},
			{Role: gateway.RoleUser, Content: "ignored"},
			{Role: gateway.RoleSystem, Content: "beta"},
		},
		Tools: []gateway.ToolDefinition{{Name: "workspace.read"}},
	}
	n, digest, tools := injectedGuidanceDigest(req)
	if n != len("alpha")+len("beta") {
		t.Fatalf("bytes = %d", n)
	}
	if len(digest) != 16 {
		t.Fatalf("digest = %q", digest)
	}
	if tools != 1 {
		t.Fatalf("tools = %d", tools)
	}
	n2, digest2, _ := injectedGuidanceDigest(req)
	if n2 != n || digest2 != digest {
		t.Fatal("digest must be stable")
	}
	req.Messages[0].Content = "ALPHA"
	_, digest3, _ := injectedGuidanceDigest(req)
	if digest3 == digest {
		t.Fatal("digest must change when system text changes")
	}
}
