package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestRepairStripsFenceAndProse(t *testing.T) {
	raw := []byte("sure:\n```json\n{\"q\":\"北京天气\",}\n```\nthanks")
	got := Repair(raw)
	if !json.Valid(got) {
		t.Fatalf("repaired not JSON: %s", got)
	}
	var v map[string]any
	if err := json.Unmarshal(got, &v); err != nil || v["q"] != "北京天气" {
		t.Fatalf("got %s err %v", got, err)
	}
}

func TestRepairLeavesValidJSON(t *testing.T) {
	in := []byte(`{"a":1}`)
	if string(Repair(in)) != string(in) {
		t.Fatal(string(Repair(in)))
	}
}

func TestValidateRequiredAndEnum(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"op":{"type":"string","enum":["navigate","read"]},"url":{"type":"string"}},"required":["op"],"additionalProperties":false}`)
	if err := Validate(schema, []byte(`{"op":"navigate","url":"https://example.com"}`)); err != nil {
		t.Fatal(err)
	}
	if err := Validate(schema, []byte(`{}`)); err == nil || err.Error() == "" {
		t.Fatal("expected missing op")
	}
	if err := Validate(schema, []byte(`{"op":"click"}`)); err == nil {
		t.Fatal("expected enum miss")
	}
	if err := Validate(schema, []byte("```json\n{\"op\":\"read\"}\n```")); err != nil {
		t.Fatal(err)
	}
}

func TestRetryMessage(t *testing.T) {
	got := RetryMessage("web.search", "missing required property \"query\"")
	if got[:8] != "ok:false" {
		t.Fatal(got)
	}
}

func TestValidateNumberBounds(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"max":{"type":"integer","minimum":1,"maximum":10}},"required":["max"]}`)
	if err := Validate(schema, []byte(`{"max":5}`)); err != nil {
		t.Fatal(err)
	}
	if err := Validate(schema, []byte(`{"max":0}`)); err == nil {
		t.Fatal("expected minimum miss")
	}
	if err := Validate(schema, []byte(`{"max":11}`)); err == nil {
		t.Fatal("expected maximum miss")
	}
}
