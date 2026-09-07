//go:build windows

package engineclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/oklog/ulid/v2"
)

func TestGuidanceAndEquipSurviveRPCAndNextRequest(t *testing.T) {
	client, server := newPipedClient(t)
	id := ulid.Make().String()
	frames := []bridge.Event{
		{Type: bridge.EventEquip, Equip: &bridge.EquipEvent{Experts: []string{"技能专家"}, Skills: []string{"skill-creator"}}},
		{Type: bridge.EventGuidance, Guidance: &bridge.GuidanceEvent{Labels: []string{"工作流", "技能"}, Digest: strings.Repeat("a", 16)}},
		{Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "技能已创建"}},
		{Type: bridge.EventCompleted},
	}
	for i, event := range frames {
		event.Version, event.Kind, event.ID, event.StreamID, event.Sequence = bridge.Version, "event", ulid.Make().String(), id, uint64(i+1)
		writeJSONFrame(t, server, event)
		select {
		case got := <-client.Events():
			if got.Type != event.Type || got.Sequence != event.Sequence {
				t.Fatalf("lost/reordered event: %#v", got)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s event", event.Type)
		}
	}
	request := bridge.Request{ID: ulid.Make().String(), DeadlineMS: 3000}
	done := make(chan error, 1)
	go func() { _, err := client.Call(context.Background(), request); done <- err }()
	raw, err := ipc.ReadFrame(server)
	if err != nil {
		t.Fatal(err)
	}
	var read bridge.Request
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	writeJSONFrame(t, server, bridge.Success(read.ID, map[string]bool{"ready": true}))
	if err := <-done; err != nil {
		t.Fatalf("next request failed: %v", err)
	}
}

func TestGuidanceAndEquipRejectInvalidOrMixedPayloads(t *testing.T) {
	base := bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: ulid.Make().String(), Sequence: 1}
	for _, mutation := range []func(*bridge.Event){
		func(e *bridge.Event) {
			e.Type = bridge.EventGuidance
			e.Guidance = &bridge.GuidanceEvent{Digest: strings.Repeat("a", 16)}
		},
		func(e *bridge.Event) {
			e.Type = bridge.EventGuidance
			e.Guidance = &bridge.GuidanceEvent{Labels: []string{"技能"}, Digest: "not-a-digest"}
		},
		func(e *bridge.Event) { e.Type = bridge.EventEquip; e.Equip = &bridge.EquipEvent{} },
		func(e *bridge.Event) {
			e.Type = bridge.EventEquip
			e.Equip = &bridge.EquipEvent{Experts: []string{strings.Repeat("界", 33)}}
		},
		func(e *bridge.Event) {
			e.Type = bridge.EventEquip
			e.Equip = &bridge.EquipEvent{Experts: []string{"专家"}}
			e.Delta = &bridge.DeltaEvent{Text: "unexpected"}
		},
		func(e *bridge.Event) {
			e.Type = bridge.EventCompleted
			e.Guidance = &bridge.GuidanceEvent{Labels: []string{"技能"}, Digest: strings.Repeat("a", 16)}
		},
	} {
		event := base
		mutation(&event)
		if validateEvent(event) == nil {
			t.Fatalf("malformed event accepted: %#v", event)
		}
	}
}
