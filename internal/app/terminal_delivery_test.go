package app

import (
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/terminalruntime"
)

func TestTerminalBlockedBridgeCannotHoldOwnershipLock(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	entered := make(chan struct{})
	e := &Engine{terminalOwners: map[string]*terminalOwner{"terminal": {emit: func(bridge.Event) error { close(entered); <-block; return nil }}}}
	events := make(chan terminalruntime.Event, 1)
	events <- terminalruntime.Event{Type: terminalruntime.EventOutput, SessionID: "terminal", Data: []byte("hello")}
	close(events)
	done := make(chan struct{})
	go func() { defer close(done); e.forwardTerminalEvents(events) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("bridge callback did not start")
	}
	read := make(chan bool, 1)
	go func() { read <- e.ownsTerminal("terminal") }()
	select {
	case <-read:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("blocked bridge owns terminal mutex")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked bridge stopped event pump shutdown")
	}
	if e.ownsTerminal("terminal") {
		t.Fatal("failed delivery retained terminal ownership")
	}
}
