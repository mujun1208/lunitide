package audit

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func chainOf(n int) []Event {
	var out []Event
	var prev *Event
	for i := 0; i < n; i++ {
		e := Link(prev, Event{
			ID:           fmt.Sprintf("01ARZ3NDEKTSV4RRFFQ69G5FA%04d", i),
			Action:       "app.update.installed",
			ResourceType: "update_installation",
			ResourceID:   "01ARZ3NDEKTSV4RRFFQ69G5FB1",
			Actor:        "device-1",
			AfterDigest:  strings.Repeat("ab", 32),
			CreatedAt:    "2026-08-15T00:00:00Z",
		})
		out = append(out, e)
		prev = &out[len(out)-1]
	}
	return out
}

func TestChainLinkProducesSealedSequence(t *testing.T) {
	events := chainOf(3)
	if err := VerifyChain(events); err != nil {
		t.Fatalf("fresh chain should verify: %v", err)
	}
	if events[0].Seq != 1 || events[0].PrevHash != GenesisPrev {
		t.Fatalf("bad genesis link: %+v", events[0])
	}
	if events[2].Seq != 3 || events[2].PrevHash != events[1].EventHash {
		t.Fatalf("bad chaining: %+v", events[2])
	}
	if len(events[0].EventHash) != 64 {
		t.Fatalf("event hash not sha256 hex: %q", events[0].EventHash)
	}
}

func TestChainEmptyVerifies(t *testing.T) {
	if err := VerifyChain(nil); err != nil {
		t.Fatalf("empty chain should verify: %v", err)
	}
}

func TestChainDetectsFieldTamper(t *testing.T) {
	events := chainOf(4)
	events[2].Actor = "mallory"
	err := VerifyChain(events)
	if !errors.Is(err, ErrChainBroken) {
		t.Fatalf("tampered actor must break chain, got %v", err)
	}
	if !strings.Contains(err.Error(), "seq 3") {
		t.Fatalf("error should point at tampered row: %v", err)
	}
}

func TestChainDetectsDeletionGap(t *testing.T) {
	events := chainOf(4)
	gapped := append(events[:1], events[2:]...) // drop seq 2
	if err := VerifyChain(gapped); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("gap must break chain, got %v", err)
	}
}

func TestChainDetectsReordering(t *testing.T) {
	events := chainOf(3)
	events[0], events[1] = events[1], events[0]
	if err := VerifyChain(events); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("swap must break chain, got %v", err)
	}
}

func TestChainDetectsForgedPrevHash(t *testing.T) {
	events := chainOf(3)
	events[1].PrevHash = GenesisPrev // pretend it is first
	if err := VerifyChain(events); !errors.Is(err, ErrChainBroken) {
		t.Fatalf("forged prev_hash must break chain, got %v", err)
	}
}

func TestChainSeqBindsHash(t *testing.T) {
	// Identical content at different seq positions must hash differently,
	// so an attacker cannot move an event to another position.
	a := Link(nil, Event{ID: "x1", Action: "a", ResourceType: "r", ResourceID: "ri", Actor: "u", CreatedAt: "2026-08-15T00:00:00Z"})
	b := a
	b.Seq, b.PrevHash = 2, GenesisPrev
	b = Seal(b)
	if a.EventHash == b.EventHash {
		t.Fatal("seq/prev must participate in the event hash")
	}
}