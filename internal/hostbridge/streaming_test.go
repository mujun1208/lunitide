package hostbridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type streamingCaller struct {
	events    chan bridge.Event
	started   chan struct{}
	release   chan struct{}
	mu        sync.Mutex
	cancelled []string
}

type immediateTerminalCaller struct {
	events   chan bridge.Event
	streamID string
}

type fastTerminalCaller struct {
	events chan bridge.Event
	mu     sync.Mutex
	starts int
}

func (c *fastTerminalCaller) Events() <-chan bridge.Event { return c.events }
func (c *fastTerminalCaller) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	if bridge.Method(r.Method) != bridge.MethodChatStart {
		return bridge.Success(r.ID, map[string]bool{"cancelled": true}), nil
	}
	id := ulid.Make().String()
	c.mu.Lock()
	c.starts++
	c.mu.Unlock()
	c.events <- bridge.Event{StreamID: id, Sequence: 1, Type: bridge.EventCompleted}
	return bridge.Success(r.ID, map[string]string{"streamId": id}), nil
}

func (c *immediateTerminalCaller) Events() <-chan bridge.Event { return c.events }
func (c *immediateTerminalCaller) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	if bridge.Method(r.Method) == bridge.MethodChatStart {
		c.events <- bridge.Event{StreamID: c.streamID, Sequence: 1, Type: bridge.EventCompleted}
		return bridge.Success(r.ID, map[string]string{"streamId": c.streamID}), nil
	}
	return bridge.Success(r.ID, map[string]bool{"cancelled": true}), nil
}

func (c *streamingCaller) Events() <-chan bridge.Event { return c.events }
func (c *streamingCaller) Call(ctx context.Context, r bridge.Request) (bridge.Response, error) {
	if bridge.Method(r.Method) == bridge.MethodChatStart {
		if c.started != nil {
			close(c.started)
		}
		select {
		case <-c.release:
		case <-ctx.Done():
		}
		return bridge.Success(r.ID, map[string]string{"streamId": ulid.Make().String()}), nil
	}
	if bridge.Method(r.Method) == bridge.MethodStreamCancel {
		var p struct {
			StreamID string `json:"streamId"`
		}
		_ = json.Unmarshal(r.Payload, &p)
		c.mu.Lock()
		c.cancelled = append(c.cancelled, p.StreamID)
		c.mu.Unlock()
	}
	return bridge.Success(r.ID, map[string]bool{"cancelled": true}), nil
}

func TestGenerationRaceNeverTransfersLateChatOwnership(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event, 4), started: make(chan struct{}), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	request := validRequest(t, string(bridge.MethodChatStart))
	done := make(chan bool, 1)
	go func() {
		_, handled := g.HandleGeneration(context.Background(), g.Generation(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: request})
		done <- handled
	}()
	<-caller.started
	g.InvalidateGeneration(context.Background())
	close(caller.release)
	select {
	case handled := <-done:
		if handled {
			t.Fatal("stale response was posted")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not return")
	}
	time.Sleep(20 * time.Millisecond)
	g.streamsMu.Lock()
	owned := len(g.streams)
	g.streamsMu.Unlock()
	if owned != 0 {
		t.Fatalf("late stream became owned by new generation: %d", owned)
	}
	caller.mu.Lock()
	cancels := len(caller.cancelled)
	caller.mu.Unlock()
	if cancels != 1 {
		t.Fatalf("late stream cancellations=%d want 1", cancels)
	}
}

func TestStartOwnershipHappensBeforeImmediateTerminalPublish(t *testing.T) {
	caller := &immediateTerminalCaller{events: make(chan bridge.Event, 1), streamID: ulid.Make().String()}
	g, _ := New("https://app.lunitide.local", caller)
	response, handled := g.HandleGeneration(context.Background(), g.Generation(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, string(bridge.MethodChatStart))})
	if !handled || !response.OK {
		t.Fatalf("start failed: handled=%v response=%#v", handled, response)
	}
	if !g.ActivateStream(g.Generation(), caller.streamID) {
		t.Fatal("immediate stream activation failed")
	}
	select {
	case event := <-g.Events():
		event.Acknowledge(true)
		if event.Event.StreamID != caller.streamID || event.Event.Type != bridge.EventCompleted {
			t.Fatalf("immediate terminal lost or corrupted: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("immediate terminal was dropped before ownership registration")
	}
}

func TestSourceCloseRacingStartRejectsAndCancelsReturnedStream(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event), started: make(chan struct{}), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	done := make(chan bridge.Response, 1)
	go func() {
		response, _ := g.HandleGeneration(context.Background(), g.Generation(), Message{SourceURL: "https://app.lunitide.local/", TopFrame: true, JSON: validRequest(t, string(bridge.MethodChatStart))})
		done <- response
	}()
	<-caller.started
	close(caller.events)
	deadline := time.Now().Add(time.Second)
	for {
		g.streamsMu.Lock()
		closed := g.eventSourceClosed
		g.streamsMu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source closure was not persisted")
		}
		time.Sleep(time.Millisecond)
	}
	close(caller.release)
	select {
	case response := <-done:
		if response.OK || response.Error == nil || response.Error.Code != "ENGINE_EVENT_SOURCE_CLOSED" {
			t.Fatalf("racing start was not explicitly rejected: %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("racing start did not return")
	}
	deadline = time.Now().Add(time.Second)
	for {
		caller.mu.Lock()
		cancelled := len(caller.cancelled)
		caller.mu.Unlock()
		if cancelled == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("racing stream cancellations=%d want=1", cancelled)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestForwardEventsRequiresOwnershipAndCleansTerminal(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event, 4), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	owned := ulid.Make().String()
	foreign := ulid.Make().String()
	generation := g.Generation()
	g.streamsMu.Lock()
	g.streams[owned] = activeOwner(generation)
	g.streamsMu.Unlock()
	caller.events <- bridge.Event{StreamID: foreign, Type: bridge.EventDelta}
	caller.events <- bridge.Event{StreamID: owned, Type: bridge.EventCompleted}
	select {
	case e := <-g.Events():
		e.Acknowledge(true)
		if e.Event.StreamID != owned {
			t.Fatal("foreign event forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("owned event not forwarded")
	}
	deadline := time.Now().Add(time.Second)
	for {
		g.streamsMu.Lock()
		_, still := g.streams[owned]
		g.streamsMu.Unlock()
		if !still {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal event retained ownership")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBufferedEventRetainsOwningGenerationAcrossInvalidation(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event, 1), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	streamID := ulid.Make().String()
	oldGeneration := g.Generation()
	g.streamsMu.Lock()
	g.streams[streamID] = activeOwner(oldGeneration)
	g.streamsMu.Unlock()
	caller.events <- bridge.Event{StreamID: streamID, Sequence: 1, Type: bridge.EventDelta}
	var buffered RoutedEvent
	select {
	case buffered = <-g.Events():
		buffered.Acknowledge(true)
	case <-time.After(time.Second):
		t.Fatal("event was not buffered")
	}
	newGeneration := g.InvalidateGeneration(context.Background())
	if buffered.Generation != oldGeneration || buffered.Generation == newGeneration {
		t.Fatalf("routed generation=%d old=%d new=%d", buffered.Generation, oldGeneration, newGeneration)
	}
	if buffered.Generation == newGeneration {
		t.Fatal("old buffered event would be dispatched to the new document")
	}
}

func TestSourceClosureSynthesizesTerminalForEveryOwnedStream(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	generation := g.Generation()
	ids := []string{ulid.Make().String(), ulid.Make().String()}
	g.streamsMu.Lock()
	for _, id := range ids {
		owner := activeOwner(generation)
		owner.sequence = 7
		g.streams[id] = owner
	}
	g.streamsMu.Unlock()
	close(caller.events)
	got := make(map[string]RoutedEvent)
	for len(got) != len(ids) {
		select {
		case event := <-g.Events():
			event.Acknowledge(true)
			got[event.Event.StreamID] = event
		case <-time.After(time.Second):
			t.Fatalf("terminals=%d want=%d", len(got), len(ids))
		}
	}
	for _, id := range ids {
		event := got[id]
		if event.Generation != generation || event.Event.Type != bridge.EventFailed || event.Event.Error == nil || event.Event.Error.Code != "ENGINE_EVENT_SOURCE_CLOSED" || event.Event.Sequence != 8 {
			t.Fatalf("invalid synthetic terminal for %s: %#v", id, event)
		}
	}
}

func TestQueueSaturationStillDeliversExactlyOneTerminal(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event, eventQueueCapacity*2), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	streamID := ulid.Make().String()
	g.streamsMu.Lock()
	g.streams[streamID] = activeOwner(g.generation)
	g.streamsMu.Unlock()
	for i := 0; i < cap(g.events); i++ {
		g.events <- RoutedEvent{}
	}
	for i := 1; i <= eventQueueCapacity+2; i++ {
		caller.events <- bridge.Event{StreamID: streamID, Sequence: uint64(i), Type: bridge.EventDelta}
	}
	terminals := 0
	deadline := time.Now().Add(2 * time.Second)
	for terminals == 0 && time.Now().Before(deadline) {
		select {
		case routed := <-g.Events():
			routed.Acknowledge(true)
			if routed.Event.StreamID == streamID && routed.Event.Type == bridge.EventFailed {
				terminals++
				if routed.Event.Error == nil || routed.Event.Error.Code != "HOST_EVENT_OVERFLOW" {
					t.Fatalf("unexpected overflow terminal: %#v", routed)
				}
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if terminals != 1 {
		t.Fatalf("overflow terminals=%d want=1", terminals)
	}
}

func TestPausedConsumerBoundsRepeatedFastTerminalStreams(t *testing.T) {
	caller := &fastTerminalCaller{events: make(chan bridge.Event, eventQueueCapacity+1)}
	g, _ := New("https://app.lunitide.local", caller)
	accepted := make(map[string]bool, eventQueueCapacity)
	for i := 0; i < eventQueueCapacity; i++ {
		response, handled := g.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local", TopFrame: true, JSON: validRequest(t, string(bridge.MethodChatStart))})
		if !handled || !response.OK {
			t.Fatalf("start %d rejected before capacity: %#v", i, response)
		}
		b, _ := json.Marshal(response.Payload)
		var p struct {
			StreamID string `json:"streamId"`
		}
		_ = json.Unmarshal(b, &p)
		accepted[p.StreamID] = true
		if !g.ActivateStream(g.Generation(), p.StreamID) {
			t.Fatalf("start %d activation failed", i)
		}
	}
	response, handled := g.Handle(context.Background(), Message{SourceURL: "https://app.lunitide.local", TopFrame: true, JSON: validRequest(t, string(bridge.MethodChatStart))})
	if !handled || response.OK || response.Error == nil || response.Error.Code != "HOST_EVENT_CAPACITY" {
		t.Fatalf("over-capacity start not explicitly rejected: %#v", response)
	}
	caller.mu.Lock()
	starts := caller.starts
	caller.mu.Unlock()
	if starts != eventQueueCapacity {
		t.Fatalf("engine starts=%d want=%d", starts, eventQueueCapacity)
	}
	seen := make(map[string]int, eventQueueCapacity)
	for len(seen) < eventQueueCapacity {
		select {
		case routed := <-g.Events():
			routed.Acknowledge(true)
			if !accepted[routed.Event.StreamID] || routed.Event.Type != bridge.EventCompleted {
				t.Fatalf("unexpected terminal: %#v", routed)
			}
			seen[routed.Event.StreamID]++
		case <-time.After(2 * time.Second):
			t.Fatalf("delivered terminals=%d want=%d", len(seen), eventQueueCapacity)
		}
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("terminal %s delivered %d times", id, count)
		}
	}
}

func TestUnifiedQueuePreservesDeltaBeforeTerminal(t *testing.T) {
	caller := &streamingCaller{events: make(chan bridge.Event, 2), release: make(chan struct{})}
	g, _ := New("https://app.lunitide.local", caller)
	streamID := ulid.Make().String()
	g.streamsMu.Lock()
	g.streams[streamID] = activeOwner(g.generation)
	g.streamsMu.Unlock()
	caller.events <- bridge.Event{StreamID: streamID, Sequence: 1, Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "last"}}
	caller.events <- bridge.Event{StreamID: streamID, Sequence: 2, Type: bridge.EventCompleted}
	for sequence := uint64(1); sequence <= 2; sequence++ {
		select {
		case routed := <-g.Events():
			routed.Acknowledge(true)
			if routed.Event.Sequence != sequence {
				t.Fatalf("event sequence=%d want=%d type=%s", routed.Event.Sequence, sequence, routed.Event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d not delivered", sequence)
		}
	}
}

func activeOwner(generation uint64) *streamOwner {
	active := make(chan struct{})
	owner := &streamOwner{generation: generation, active: active}
	owner.activateOnce.Do(func() { close(active) })
	return owner
}

func TestStopEventConsumerReleasesEveryDispatchWait(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Gateway, *streamOwner)
	}{
		{name: "activation", setup: func(*Gateway, *streamOwner) {}},
		{name: "event send", setup: func(_ *Gateway, owner *streamOwner) { owner.activateOnce.Do(func() { close(owner.active) }) }},
		{name: "acknowledgement", setup: func(g *Gateway, owner *streamOwner) {
			owner.activateOnce.Do(func() { close(owner.active) })
			select {
			case <-g.events:
			case <-time.After(time.Second):
				t.Fatal("event was not sent")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caller := &streamingCaller{events: make(chan bridge.Event), release: make(chan struct{})}
			g, _ := New("https://app.lunitide.local", caller)
			owner := &streamOwner{generation: g.generation, active: make(chan struct{}), admitted: true}
			g.streamsMu.Lock()
			g.streams["stream"] = owner
			g.outstanding = 1
			g.reservations = 1
			g.eventQueue = append(g.eventQueue, RoutedEvent{Generation: g.generation, owner: owner, Event: bridge.Event{StreamID: "stream", Type: bridge.EventDelta}})
			g.eventChanged.Broadcast()
			g.streamsMu.Unlock()
			tc.setup(g, owner)
			g.StopEventConsumer()
			select {
			case <-g.dispatchDone:
			case <-time.After(time.Second):
				t.Fatal("dispatcher remained blocked after consumer stop")
			}
			g.streamsMu.Lock()
			defer g.streamsMu.Unlock()
			if len(g.streams) != 0 || len(g.eventQueue) != 0 || g.outstanding != 0 || g.reservations != 0 {
				t.Fatalf("shutdown leaked ownership/capacity: streams=%d queue=%d outstanding=%d reservations=%d", len(g.streams), len(g.eventQueue), g.outstanding, g.reservations)
			}
		})
	}
}
