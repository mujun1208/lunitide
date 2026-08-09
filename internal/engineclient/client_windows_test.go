//go:build windows

package engineclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/oklog/ulid/v2"
)

func newPipedClient(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	client := &Client{conn: clientConn, pending: make(map[string]chan callResult), events: make(chan bridge.Event, 1024), tombstones: make(map[string]time.Time), streams: make(map[string]streamProgress), streamTerminals: make(map[string]time.Time)}
	go client.readPump()
	t.Cleanup(func() { _ = client.Close(); _ = serverConn.Close() })
	return client, serverConn
}

func writeJSONFrame(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = ipc.WriteFrame(conn, raw); err != nil {
		t.Fatal(err)
	}
}

func TestValidateResponseRejectsMismatchedRequest(t *testing.T) {
	requestID := ulid.Make().String()
	response := bridge.Success(ulid.Make().String(), map[string]bool{"ok": true})
	if err := validateResponse(response, requestID); err == nil {
		t.Fatal("mismatched request ID was accepted")
	}
}

func TestValidateResponseAcceptsSuccessAndFailureShapes(t *testing.T) {
	requestID := ulid.Make().String()
	for _, response := range []bridge.Response{
		bridge.Success(requestID, map[string]bool{"ok": true}),
		bridge.Failure(requestID, ulid.Make().String(), "ENGINE_UNAVAILABLE", "unavailable", true),
	} {
		if err := validateResponse(response, requestID); err != nil {
			t.Fatalf("valid response rejected: %v", err)
		}
	}
}

func TestDecodeStrictRejectsUnknownAndTrailingFields(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"accepted":true,"rpcMajor":1,"rpcMinor":0,"extra":true}`),
		[]byte(`{"accepted":true,"rpcMajor":1,"rpcMinor":0} {}`),
	} {
		var ack handshakeAck
		if err := decodeStrict(raw, &ack); err == nil {
			t.Fatalf("invalid ACK accepted: %s", raw)
		}
	}
}

func TestPoisonClosesConnectionAndPreventsReuse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	client := &Client{conn: clientConn}
	first := client.poison(errors.New("read timeout"))
	if first == nil || client.brokenError() == nil {
		t.Fatal("connection was not poisoned")
	}
	if _, err := serverConn.Write([]byte("x")); err == nil {
		t.Fatal("poison did not close the connection")
	}
	request := bridge.Request{ID: ulid.Make().String(), DeadlineMS: 3000}
	if _, err := client.Call(context.Background(), request); err == nil || err.Error() != first.Error() {
		t.Fatalf("poisoned client was reused: %v", err)
	}
}

func TestCloseInterruptsBlockedCallAndPreventsReuse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	client := &Client{conn: clientConn}
	callDone := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), bridge.Request{ID: ulid.Make().String(), DeadlineMS: 30000})
		callDone <- err
	}()
	requestRead := make(chan struct{})
	go func() {
		var buf [4096]byte
		_, _ = serverConn.Read(buf[:])
		close(requestRead)
	}()
	select {
	case <-requestRead:
	case <-time.After(time.Second):
		t.Fatal("Call did not reach the blocked response read")
	}
	closed := make(chan error, 1)
	go func() { closed <- client.Close() }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for the serialized Call")
	}
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("blocked Call succeeded after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not interrupt blocked Call")
	}
	if _, err := client.Call(context.Background(), bridge.Request{ID: ulid.Make().String()}); err == nil {
		t.Fatal("closed client was reused")
	}
}

func TestDuplicatePendingRequestIDRejectedWithoutDisturbingOriginal(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	first := make(chan error, 1)
	go func() { _, err := client.Call(context.Background(), bridge.Request{ID: id}); first <- err }()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(context.Background(), bridge.Request{ID: id}); err == nil || err.Error() != "duplicate pending Engine request ID" {
		t.Fatalf("duplicate error = %v", err)
	}
	writeJSONFrame(t, server, bridge.Success(id, map[string]bool{"ok": true}))
	if err := <-first; err != nil {
		t.Fatalf("original call failed: %v", err)
	}
}

func TestCancelledCallLateResponseTombstonePreservesConnection(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Call(ctx, bridge.Request{ID: id}); done <- err }()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("call error = %v", err)
	}
	writeJSONFrame(t, server, bridge.Success(id, map[string]bool{"late": true}))

	nextID := ulid.Make().String()
	next := make(chan error, 1)
	go func() { _, err := client.Call(context.Background(), bridge.Request{ID: nextID}); next <- err }()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	writeJSONFrame(t, server, bridge.Success(nextID, map[string]bool{"ok": true}))
	if err := <-next; err != nil {
		t.Fatalf("connection lost after late response: %v", err)
	}
}

func TestCancelledRequestIDCannotBeReusedWhileLateResponseIsPossible(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Call(ctx, bridge.Request{ID: id}); done <- err }()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("call error = %v", err)
	}
	if _, err := client.Call(context.Background(), bridge.Request{ID: id}); err == nil {
		t.Fatal("cancelled request ID was reused")
	}
}

func TestMoreThan256CancelledRequestsRetainLateResponseProtection(t *testing.T) {
	clientConn, server := net.Pipe()
	client := &Client{conn: clientConn, pending: make(map[string]chan callResult), events: make(chan bridge.Event, 1024), tombstones: make(map[string]time.Time), streams: make(map[string]streamProgress), streamTerminals: make(map[string]time.Time)}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	ids := make([]string, 257)
	client.stateMu.Lock()
	for i := range ids {
		ids[i] = ulid.Make().String()
		client.addTombstoneLocked(ids[i])
	}
	client.stateMu.Unlock()
	go client.readPump()
	writeJSONFrame(t, server, bridge.Success(ids[0], map[string]bool{"late": true}))
	time.Sleep(20 * time.Millisecond)
	if err := client.brokenError(); err != nil {
		t.Fatalf("valid late response poisoned connection: %v", err)
	}
}

func TestEventQueueAcceptsBurstAndPreservesOrder(t *testing.T) {
	client, server := newPipedClient(t)
	streamID := ulid.Make().String()
	for i := 1; i <= cap(client.events); i++ {
		writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: uint64(i), Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "x"}})
	}
	for i := 1; i <= cap(client.events); i++ {
		select {
		case event := <-client.Events():
			if event.Sequence != uint64(i) {
				t.Fatalf("sequence=%d want=%d", event.Sequence, i)
			}
		case <-time.After(time.Second):
			t.Fatalf("burst stalled at %d", i)
		}
	}
	if err := client.brokenError(); err != nil {
		t.Fatalf("burst poisoned connection: %v", err)
	}
}

func TestEventSequenceGapPoisonsConnection(t *testing.T) {
	client, server := newPipedClient(t)
	streamID := ulid.Make().String()
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: 2, Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "x"}})
	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatal("invalid sequence was forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("sequence violation did not terminate event source")
	}
	if client.brokenError() == nil {
		t.Fatal("sequence violation did not poison connection")
	}
}

func TestEventAfterTerminalPoisonsWithoutAffectingFirstTerminal(t *testing.T) {
	client, server := newPipedClient(t)
	streamID := ulid.Make().String()
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: 1, Type: bridge.EventCompleted})
	if event := <-client.Events(); event.Type != bridge.EventCompleted {
		t.Fatalf("first terminal = %s", event.Type)
	}
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: streamID, Sequence: 2, Type: bridge.EventCancelled})
	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatal("second terminal was forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("second terminal did not terminate event source")
	}
}

func TestEventArbitrationIsIndependentPerStream(t *testing.T) {
	client, server := newPipedClient(t)
	first, second := ulid.Make().String(), ulid.Make().String()
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: first, Sequence: 1, Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "a"}})
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: second, Sequence: 1, Type: bridge.EventCompleted})
	writeJSONFrame(t, server, bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: first, Sequence: 2, Type: bridge.EventCompleted})
	for i, want := range []string{first, second, first} {
		select {
		case event := <-client.Events():
			if event.StreamID != want {
				t.Fatalf("event %d stream=%s want=%s", i, event.StreamID, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d stalled", i)
		}
	}
	if err := client.brokenError(); err != nil {
		t.Fatalf("independent streams poisoned connection: %v", err)
	}
}

func TestValidateEventDiscriminatedUnion(t *testing.T) {
	base := bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: ulid.Make().String(), Sequence: 1}
	tests := []struct {
		name   string
		mutate func(*bridge.Event)
		valid  bool
	}{
		{"delta", func(e *bridge.Event) { e.Type = bridge.EventDelta; e.Delta = &bridge.DeltaEvent{Text: "x"} }, true},
		{"empty delta", func(e *bridge.Event) { e.Type = bridge.EventDelta; e.Delta = &bridge.DeltaEvent{} }, false},
		{"delta extra body", func(e *bridge.Event) {
			e.Type = bridge.EventDelta
			e.Delta = &bridge.DeltaEvent{Text: "x"}
			e.Usage = &bridge.UsageEvent{}
		}, false},
		{"usage", func(e *bridge.Event) {
			e.Type = bridge.EventUsage
			e.Usage = &bridge.UsageEvent{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
		}, true},
		{"usage sum", func(e *bridge.Event) {
			e.Type = bridge.EventUsage
			e.Usage = &bridge.UsageEvent{InputTokens: 2, OutputTokens: 3, TotalTokens: 4}
		}, false},
		{"terminal body", func(e *bridge.Event) {
			e.Type = bridge.EventCompleted
			e.Error = &bridge.StreamError{Code: "x", Message: "x"}
		}, false},
		{"failed", func(e *bridge.Event) {
			e.Type = bridge.EventFailed
			e.Error = &bridge.StreamError{Code: "UPSTREAM", Message: "failed"}
		}, true},
		{"failed empty", func(e *bridge.Event) { e.Type = bridge.EventFailed; e.Error = &bridge.StreamError{} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := base
			tt.mutate(&e)
			if (validateEvent(e) == nil) != tt.valid {
				t.Fatalf("validity mismatch: %#v", e)
			}
		})
	}
}

func TestPoisonAndCancellationAlwaysCompleteCallWithError(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan callResult, 1)
	go func() {
		response, err := client.Call(ctx, bridge.Request{ID: id})
		done <- callResult{response: response, err: err}
	}()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	client.stateMu.Lock()
	poisoned := make(chan struct{})
	go func() {
		client.poison(errors.New("forced poison"))
		close(poisoned)
	}()
	cancel()
	client.stateMu.Unlock()
	select {
	case result := <-done:
		if result.err == nil || result.response.OK {
			t.Fatalf("zero/success result after poison+cancellation: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("poison+cancellation deadlocked Call")
	}
	<-poisoned
}

func TestInvalidResponseDoesNotOrphanPendingCall(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	done := make(chan callResult, 1)
	go func() {
		response, err := client.Call(context.Background(), bridge.Request{ID: id})
		done <- callResult{response: response, err: err}
	}()
	if _, err := ipc.ReadFrame(server); err != nil {
		t.Fatal(err)
	}
	invalid := bridge.Success(id, map[string]bool{"ok": true})
	invalid.ID = "not-a-ulid"
	writeJSONFrame(t, server, invalid)
	select {
	case result := <-done:
		if result.err == nil || result.response.OK {
			t.Fatalf("invalid response produced zero/success result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("invalid response orphaned pending Call")
	}
}

func TestReadPumpTerminationClosesEventsExactlyOnce(t *testing.T) {
	client, server := newPipedClient(t)
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-client.Events():
		if ok {
			t.Fatal("event channel yielded value after source termination")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel was not closed")
	}
	client.poison(errors.New("second poison"))
	_ = client.Close()
}
