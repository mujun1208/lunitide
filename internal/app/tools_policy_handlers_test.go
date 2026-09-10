package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func observedNamedPolicyRequest(t *testing.T, e *Engine, getMethod, setMethod, raw string) bridge.Request {
	t.Helper()
	status := e.Handle(context.Background(), validRequest(getMethod, `{}`))
	if !status.OK {
		t.Fatalf("%s: %+v", getMethod, status.Error)
	}
	encoded, _ := json.Marshal(status.Payload)
	var current struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(encoded, &current); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	payload["expectedRevision"] = current.Revision
	body, _ := json.Marshal(payload)
	return validRequest(setMethod, string(body))
}

func TestCommandPolicySetInvalidPrefixIsChinese(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)

	resp := e.Handle(context.Background(), observedNamedPolicyRequest(t, e, "tools.commandPolicy.get", "tools.commandPolicy.set", `{"commands":[{"prefix":["C:evil"]}]}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "COMMAND_POLICY_INVALID" {
		t.Fatalf("expected COMMAND_POLICY_INVALID, got %+v", resp)
	}
	if strings.Contains(resp.Error.Message, "invalid prefix") || strings.Contains(resp.Error.Message, "command-policy.json") || !officeUserMessageHasHan(resp.Error.Message) {
		t.Fatalf("settings-facing command policy leaked English: %q", resp.Error.Message)
	}

	empty := e.Handle(context.Background(), observedNamedPolicyRequest(t, e, "tools.commandPolicy.get", "tools.commandPolicy.set", `{"commands":[{"prefix":[]}]}`))
	if empty.OK || empty.Error == nil || empty.Error.Code != "COMMAND_POLICY_INVALID" {
		t.Fatalf("expected empty-prefix COMMAND_POLICY_INVALID, got %+v", empty)
	}
	if !strings.Contains(empty.Error.Message, "命令前缀必须是") || strings.Contains(empty.Error.Message, "prefix must have") {
		t.Fatalf("empty prefix leaked English: %q", empty.Error.Message)
	}
}

func TestHooksPolicySetBlockWithoutMessageIsChinese(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)

	resp := e.Handle(context.Background(), observedNamedPolicyRequest(t, e, "tools.hooksPolicy.get", "tools.hooksPolicy.set", `{"hooks":[{"id":"no-docx","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"block"}]}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "HOOKS_POLICY_INVALID" {
		t.Fatalf("expected HOOKS_POLICY_INVALID, got %+v", resp)
	}
	if strings.Contains(resp.Error.Message, "requires a message") || strings.Contains(resp.Error.Message, "hooks-policy.json") || !officeUserMessageHasHan(resp.Error.Message) {
		t.Fatalf("settings-facing hooks policy leaked English: %q", resp.Error.Message)
	}
}
