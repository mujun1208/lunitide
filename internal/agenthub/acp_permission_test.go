package agenthub

import (
	"encoding/json"
	"testing"
)

func TestACPAutoAllowPermission(t *testing.T) {
	edit := json.RawMessage(`{"toolCall":{"title":"Edit file","kind":"edit"}}`)
	shell := json.RawMessage(`{"toolCall":{"title":"Run shell","kind":"execute"}}`)
	ask := json.RawMessage(`{"questions":[{"prompt":"选哪个?"}]}`)
	cases := []struct {
		access, method string
		params         json.RawMessage
		want           bool
	}{
		{"full-access", "session/request_permission", edit, true},
		{"full-access", "session/request_permission", shell, true},
		{"full-access", "cursor/ask_question", ask, false},
		{"auto-edit", "session/request_permission", edit, true},
		{"auto-edit", "session/request_permission", shell, false},
		{"approval", "session/request_permission", edit, false},
	}
	for _, tc := range cases {
		if got := acpAutoAllowPermission(tc.access, tc.method, tc.params); got != tc.want {
			t.Fatalf("%s %s = %v, want %v", tc.access, tc.method, got, tc.want)
		}
	}
}
