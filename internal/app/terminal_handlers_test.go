package app

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
	"testing"
	"time"
)

func terminalRequest(method bridge.Method, payload any) bridge.Request {
	raw, _ := json.Marshal(payload)
	return bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: string(method), SentAt: time.Now().UTC(), Payload: raw, DeadlineMS: 1000}
}
func TestTerminalStartFailsClosedWithoutRuntime(t *testing.T) {
	e := NewEngine(nil, "test")
	r := e.Handle(context.Background(), terminalRequest(bridge.MethodTerminalStart, map[string]any{"projectId": ulid.Make().String(), "sessionId": ulid.Make().String(), "cols": 80, "rows": 24}))
	if r.OK || r.Error == nil || r.Error.Code != "TERMINAL_UNAVAILABLE" {
		t.Fatalf("response=%#v", r)
	}
}
func TestTerminalMethodsRejectInvalidOrUnownedIDs(t *testing.T) {
	e := NewEngine(nil, "test")
	for _, tc := range []struct {
		m bridge.Method
		p any
	}{{bridge.MethodTerminalInput, map[string]any{"terminalId": "bad", "data": "x"}}, {bridge.MethodTerminalResize, map[string]any{"terminalId": ulid.Make().String(), "cols": 0, "rows": 24}}, {bridge.MethodTerminalClose, map[string]any{"terminalId": ulid.Make().String()}}} {
		r := e.Handle(context.Background(), terminalRequest(tc.m, tc.p))
		if r.OK || r.Error == nil {
			t.Fatalf("%s accepted: %#v", tc.m, r)
		}
	}
}
