package mcp

import (
	"encoding/json"
	"testing"
)

func TestJSONRPCIDMatchesNumericAndString(t *testing.T) {
	if !jsonRPCIDMatches(json.RawMessage(`1`), 1) || !jsonRPCIDMatches(json.RawMessage(`"1"`), 1) {
		t.Fatal("numeric and string ids must match")
	}
	if jsonRPCIDMatches(json.RawMessage(`"2"`), 1) || jsonRPCIDMatches(json.RawMessage(`null`), 1) || jsonRPCIDMatches(nil, 1) {
		t.Fatal("mismatched or empty ids must not match")
	}
}

func TestRemoteRPCResultAcceptsStringID(t *testing.T) {
	result, matched, err := remoteRPCResult([]byte(`{"jsonrpc":"2.0","id":"3","result":{"ok":true}}`), 3)
	if err != nil || !matched || string(result) != `{"ok":true}` {
		t.Fatalf("string id: %s matched=%v err=%v", result, matched, err)
	}
}

func TestCoerceJSONTextAcceptsNumericVersion(t *testing.T) {
	if got := coerceJSONText(json.RawMessage(`"1.4.0"`)); got != "1.4.0" {
		t.Fatalf("string version = %q", got)
	}
	if got := coerceJSONText(json.RawMessage(`1.2`)); got != "1.2" {
		t.Fatalf("numeric version = %q", got)
	}
	if got := coerceJSONText(json.RawMessage(`12`)); got != "12" {
		t.Fatalf("integer version = %q", got)
	}
}
