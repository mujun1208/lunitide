package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredOutputEventTemplate(t *testing.T) {
	args := json.RawMessage(`{"template":"event","data":{"title":"站会","start":"2026-08-27T09:30:00+08:00"}}`)
	r, err := emitStructuredOutput(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Output, `"title"`) || !strings.Contains(r.Output, "站会") {
		t.Fatalf("output = %s", r.Output)
	}
}

func TestStructuredOutputRejectsMissingStart(t *testing.T) {
	_, err := emitStructuredOutput(json.RawMessage(`{"template":"event","data":{"title":"站会"}}`))
	if err == nil || !strings.Contains(err.Error(), "retry:") {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredOutputRepairsFence(t *testing.T) {
	raw := []byte("```json\n{\"template\":\"kv\",\"data\":{\"pairs\":[{\"key\":\"a\",\"value\":\"1\"}]},}\n```")
	r, err := emitStructuredOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Output, `"key"`) {
		t.Fatalf("output = %s", r.Output)
	}
}

func TestPrepareToolArgumentsMissingRequired(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`)
	_, hint := prepareToolArguments("web.search", json.RawMessage(`{}`), schema)
	if hint == "" || !strings.Contains(hint, "query") {
		t.Fatalf("hint = %q", hint)
	}
	prepared, hint := prepareToolArguments("web.search", json.RawMessage("sure\n```json\n{\"query\":\"北京天气\",}\n```"), schema)
	if hint != "" {
		t.Fatalf("repaired should pass, hint=%q prepared=%s", hint, prepared)
	}
}

func TestReplyStyleKeepsLunitideIdentity(t *testing.T) {
	got := replyStyleInstruction("teacher", false)
	if !strings.Contains(got, "保持月汐身份") || !strings.Contains(got, "老师") {
		t.Fatalf("got %q", got)
	}
	if replyStyleInstruction("teacher", true) != "" {
		t.Fatal("companion must not overlay reply style")
	}
}

func TestIdentityAndFewShotNamesBoundaries(t *testing.T) {
	got := identityAndFewShotInstruction()
	if !strings.Contains(got, "你是月汐") || !strings.Contains(got, "禁止") || !strings.Contains(got, "web.search") {
		t.Fatalf("got %q", got)
	}
}
