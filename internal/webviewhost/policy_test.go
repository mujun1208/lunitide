package webviewhost

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/oklog/ulid/v2"
)

type testCaller struct{ calls atomic.Int32 }

func (c *testCaller) Call(_ context.Context, request bridge.Request) (bridge.Response, error) {
	c.calls.Add(1)
	return bridge.Success(request.ID, map[string]string{"status": "ok"}), nil
}

func TestNavigationPolicyRejectsUntrustedDocuments(t *testing.T) {
	for _, raw := range []string{"http://app.lunitide.local/", "https://evil.example/", "https://app.lunitide.local.evil/", "data:text/html,x", "https://app.lunitide.local:444/"} {
		if NavigationAllowed(raw) {
			t.Fatalf("allowed untrusted navigation %q", raw)
		}
	}
	for _, raw := range []string{TrustedOrigin + "/index.html", TrustedOrigin + "/index.html?route=settings", TrustedOrigin + "/index.html#settings"} {
		if !NavigationAllowed(raw) {
			t.Fatalf("trusted navigation rejected: %q", raw)
		}
	}
}

func TestBlockedNavigationDoesNotInvalidateGeneration(t *testing.T) {
	for _, raw := range []string{"https://evil.example/", "data:text/html,x", "http://app.lunitide.local/"} {
		if NavigationInvalidatesGeneration(raw) {
			t.Fatalf("blocked navigation invalidated generation: %q", raw)
		}
	}
	if !NavigationInvalidatesGeneration(TrustedOrigin + "/next") {
		t.Fatal("trusted document navigation did not invalidate generation")
	}
}

func TestExternalActionsDefaultDeny(t *testing.T) {
	for _, action := range []string{"new-window", "permission", "download"} {
		if ExternalActionAllowed(action) {
			t.Fatalf("external action %q was allowed", action)
		}
	}
}

func TestGenerationCurrentDropsPreNavigationWork(t *testing.T) {
	if !GenerationCurrent(7, 7, false) {
		t.Fatal("current generation was dropped")
	}
	if GenerationCurrent(7, 8, false) {
		t.Fatal("stale generation was dispatched")
	}
	if GenerationCurrent(7, 7, true) {
		t.Fatal("closed host dispatched work")
	}
}

func TestDispatchChildFrameAndUnhandledNeverReply(t *testing.T) {
	caller := &testCaller{}
	gateway, _ := hostbridge.New(TrustedOrigin, caller)
	replied := make(chan struct{}, 1)
	Dispatch(context.Background(), gateway, gateway.Generation(), hostbridge.Message{SourceURL: TrustedOrigin, TopFrame: false, JSON: requestJSON(t)}, func(fn func()) bool { fn(); return true }, func([]byte) bool { replied <- struct{}{}; return true })
	select {
	case <-replied:
		t.Fatal("child frame received a reply")
	case <-time.After(100 * time.Millisecond):
	}
	if caller.calls.Load() != 0 {
		t.Fatalf("child frame escaped gate: calls=%d", caller.calls.Load())
	}
}

func TestDispatchHandledReplyPreservesRequestID(t *testing.T) {
	caller := &testCaller{}
	gateway, _ := hostbridge.New(TrustedOrigin, caller)
	raw := requestJSON(t)
	var request bridge.Request
	_ = json.Unmarshal(raw, &request)
	done := make(chan []byte, 1)
	Dispatch(context.Background(), gateway, gateway.Generation(), hostbridge.Message{SourceURL: TrustedOrigin + "/", TopFrame: true, JSON: raw}, func(fn func()) bool { fn(); return true }, func(reply []byte) bool { done <- reply; return true })
	select {
	case reply := <-done:
		var response bridge.Response
		if err := json.Unmarshal(reply, &response); err != nil || response.RequestID != request.ID {
			t.Fatalf("request correlation lost: err=%v response=%#v", err, response)
		}
	case <-time.After(time.Second):
		t.Fatal("handled message did not reply")
	}
}

type immediateStreamCaller struct {
	events   chan bridge.Event
	streamID string
}

func (c *immediateStreamCaller) Events() <-chan bridge.Event { return c.events }
func (c *immediateStreamCaller) Call(_ context.Context, request bridge.Request) (bridge.Response, error) {
	c.events <- bridge.Event{StreamID: c.streamID, Sequence: 1, Type: bridge.EventCompleted}
	return bridge.Success(request.ID, map[string]string{"streamId": c.streamID}), nil
}

func TestDispatchActivatesStreamAfterResponseUIExecution(t *testing.T) {
	caller := &immediateStreamCaller{events: make(chan bridge.Event, 1), streamID: ulid.Make().String()}
	gateway, _ := hostbridge.New(TrustedOrigin, caller)
	ui := make(chan func(), 1)
	posted := false
	Dispatch(context.Background(), gateway, gateway.Generation(), hostbridge.Message{SourceURL: TrustedOrigin, TopFrame: true, JSON: requestJSONForMethod(t, string(bridge.MethodChatStart))}, func(fn func()) bool { ui <- fn; return true }, func([]byte) bool {
		posted = true
		return true
	})
	select {
	case event := <-gateway.Events():
		event.Acknowledge(false)
		t.Fatal("event escaped before chat.start response UI execution")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case fn := <-ui:
		fn()
	case <-time.After(time.Second):
		t.Fatal("response UI work was not scheduled")
	}
	select {
	case event := <-gateway.Events():
		event.Acknowledge(true)
		if !posted || event.Event.StreamID != caller.streamID || event.Event.Type != bridge.EventCompleted {
			t.Fatalf("event preceded response or was corrupted: posted=%v event=%#v", posted, event)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal did not pass activated barrier")
	}
}

func requestJSON(t *testing.T) []byte {
	return requestJSONForMethod(t, "system.health")
}

func requestJSONForMethod(t *testing.T, method string) []byte {
	t.Helper()
	raw, err := json.Marshal(bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: method, SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestBoundedQueueCoalescesAndRejectsOverflow(t *testing.T) {
	q := NewBoundedQueue[int](2)
	if ok, notify := q.Push(1); !ok || !notify {
		t.Fatal("first push must schedule drain")
	}
	if ok, notify := q.Push(2); !ok || notify {
		t.Fatal("second push must be coalesced")
	}
	if ok, _ := q.Push(3); ok {
		t.Fatal("overflow accepted")
	}
	got := q.Drain()
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("drain=%v", got)
	}
	if ok, notify := q.Push(4); !ok || !notify {
		t.Fatal("post-drain push did not reschedule")
	}
}

func TestDeliverRoutedEventAcknowledgesMarshalFailure(t *testing.T) {
	caller := &immediateStreamCaller{events: make(chan bridge.Event, 2), streamID: ulid.Make().String()}
	gateway, _ := hostbridge.New(TrustedOrigin, caller)
	response, handled := gateway.Handle(context.Background(), hostbridge.Message{SourceURL: TrustedOrigin, TopFrame: true, JSON: requestJSONForMethod(t, string(bridge.MethodChatStart))})
	if !handled || !response.OK || !gateway.ActivateStream(gateway.Generation(), caller.streamID) {
		t.Fatal("stream setup failed")
	}
	select {
	case routed := <-gateway.Events():
		if DeliverRoutedEvent(routed, func(any) ([]byte, error) { return nil, errors.New("marshal failed") }, func([]byte) bool {
			t.Fatal("delivery called after marshal failure")
			return true
		}) {
			t.Fatal("marshal failure reported delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("event was not routed")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !gateway.ActivateStream(gateway.Generation(), caller.streamID) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("marshal-failed terminal was not acknowledged and cleaned up")
}
