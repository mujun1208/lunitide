package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/terminalruntime"
	"github.com/oklog/ulid/v2"
)

func handleTerminalStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		SessionID string `json:"sessionId"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || !validCanonicalULID(p.SessionID) || p.Cols < 1 || p.Cols > 500 || p.Rows < 1 || p.Rows > 500 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "terminal.start 参数无效", false)
	}
	if e.terminals == nil || !sessionServiceAvailable(e.sessions) {
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_UNAVAILABLE", "终端暂时不可用", true)
	}
	items, err := e.sessions.List(ctx, session.Filter{ProjectID: p.ProjectID})
	if err != nil {
		return sessionFailure(r, err)
	}
	found := false
	for _, item := range items {
		if item.ID == p.SessionID && item.ProjectID == p.ProjectID {
			found = true
			break
		}
	}
	if !found {
		return bridge.Failure(r.ID, r.TraceID, "SESSION_NOT_FOUND", "会话不存在或不属于当前项目", false)
	}
	emit, ok := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if !ok || emit == nil {
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_EVENT_TRANSPORT_UNAVAILABLE", "终端事件通道不可用", true)
	}
	id := ulid.Make().String()
	owner := &terminalOwner{emit: emit}
	e.terminalsMu.Lock()
	e.terminalOwners[id] = owner
	e.terminalsMu.Unlock()
	// Start must not inherit the short request deadline: lifetime is explicitly
	// owned by terminal.close, renderer generation invalidation, or engine shutdown.
	if err = e.terminals.Start(context.Background(), id, p.Cols, p.Rows); err != nil {
		e.terminalsMu.Lock()
		delete(e.terminalOwners, id)
		e.terminalsMu.Unlock()
		return terminalFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]string{"terminalId": id})
}

func handleTerminalInput(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TerminalID string `json:"terminalId"`
		Data       string `json:"data"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TerminalID) || len(p.Data) < 1 || len([]byte(p.Data)) > 65536 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "terminal.input 参数无效", false)
	}
	if !e.ownsTerminal(p.TerminalID) {
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_NOT_OWNED", "终端不属于当前会话", false)
	}
	if err := e.terminals.Write(p.TerminalID, []byte(p.Data)); err != nil {
		return terminalFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]bool{"accepted": true})
}
func handleTerminalResize(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TerminalID string `json:"terminalId"`
		Cols       int    `json:"cols"`
		Rows       int    `json:"rows"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TerminalID) || p.Cols < 1 || p.Cols > 500 || p.Rows < 1 || p.Rows > 500 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "terminal.resize 参数无效", false)
	}
	if !e.ownsTerminal(p.TerminalID) {
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_NOT_OWNED", "终端不属于当前会话", false)
	}
	if err := e.terminals.Resize(p.TerminalID, p.Cols, p.Rows); err != nil {
		return terminalFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]bool{"resized": true})
}
func handleTerminalClose(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TerminalID string `json:"terminalId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TerminalID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "terminal.close 参数无效", false)
	}
	if !e.ownsTerminal(p.TerminalID) {
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_NOT_OWNED", "终端不属于当前会话", false)
	}
	err := e.terminals.Close(p.TerminalID)
	e.terminalsMu.Lock()
	delete(e.terminalOwners, p.TerminalID)
	e.terminalsMu.Unlock()
	if err != nil && !errors.Is(err, terminalruntime.ErrNotFound) {
		return terminalFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]bool{"closed": true})
}
func (e *Engine) ownsTerminal(id string) bool {
	e.terminalsMu.Lock()
	defer e.terminalsMu.Unlock()
	_, ok := e.terminalOwners[id]
	return ok
}
func (e *Engine) forwardTerminalEvents(events <-chan terminalruntime.Event) {
	for ev := range events {
		if ev.Type == terminalruntime.EventStarted {
			continue
		}
		e.terminalsMu.Lock()
		o := e.terminalOwners[ev.SessionID]
		if o == nil {
			e.terminalsMu.Unlock()
			continue
		}
		if ev.Type == terminalruntime.EventOutput {
			for len(ev.Data) > 0 {
				n := len(ev.Data)
				if n > 16*1024 {
					n = 16 * 1024
				}
				o.sequence++
				_ = o.emit(bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: ev.SessionID, Sequence: o.sequence, Type: bridge.EventTerminalOutput, Terminal: &bridge.TerminalEvent{Data: string(ev.Data[:n])}})
				ev.Data = ev.Data[n:]
			}
			e.terminalsMu.Unlock()
			continue
		}
		o.sequence++
		code := ev.ExitCode
		if ev.Type == terminalruntime.EventError {
			code = 1
		}
		_ = o.emit(bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: ev.SessionID, Sequence: o.sequence, Type: bridge.EventTerminalExit, Terminal: &bridge.TerminalEvent{ExitCode: code}})
		delete(e.terminalOwners, ev.SessionID)
		e.terminalsMu.Unlock()
	}
}
func terminalFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, terminalruntime.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "终端参数无效", false)
	case errors.Is(err, terminalruntime.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_NOT_FOUND", "终端不存在", false)
	case errors.Is(err, terminalruntime.ErrLimit):
		return bridge.Failure(r.ID, r.TraceID, "TERMINAL_LIMIT", "终端数量已达上限", false)
	default:
		return internalBridgeFailure(r, "TERMINAL_OPERATION_FAILED", "终端操作失败", true, err)
	}
}

var _ = json.RawMessage{}
