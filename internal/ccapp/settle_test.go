package ccapp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// slowHost repaints only after `changeAt` captures: the click "lands" late,
// like an Electron view that redraws a few hundred ms after the input.
type slowHost struct {
	chainHost
	changeAt int
}

func (h *slowHost) ScreenCapture() ([]byte, error) {
	h.captures++
	if h.captures < h.changeAt {
		return tintPNG(h.png, 1), nil
	}
	return tintPNG(h.png, h.captures), nil
}

func newSlowService(t *testing.T, changeAt int, budget time.Duration) (*Service, *slowHost) {
	t.Helper()
	host := &slowHost{
		chainHost: chainHost{
			nativeStubHost: nativeStubHost{
				ladderStubHost: ladderStubHost{
					title: "Untitled - Notepad", process: "notepad.exe",
					nodes: []UINode{{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24}},
				},
				hit: "保存",
			},
			png: tinyPNG(t, 64, 36),
		},
		changeAt: changeAt,
	}
	svc := New(nil)
	svc.SetHost(host)
	svc.SetMutateSettleForTest(budget)
	return svc, host
}

func TestVerifyAfterSettlesAdaptively(t *testing.T) {
	// Capture 1 = observe baseline, capture 2 = immediate verify (same),
	// capture 3 = first poll (same), capture 4 = second poll (changed).
	svc, host := newSlowService(t, 4, 2*time.Second)
	if _, _, err := computerAct(svc, `{"action":"observe"}`); err != nil {
		t.Fatalf("observe: %v", err)
	}
	start := time.Now()
	summary, _, err := computerAct(svc, `{"action":"click","name":"保存"}`)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("slow repaint inside the budget must verify: %v (%s)", err, summary)
	}
	if !strings.Contains(summary, "screen updated") {
		t.Fatalf("summary: %s", summary)
	}
	// Two polls of 120ms, not the whole 2s budget.
	if took > 1200*time.Millisecond {
		t.Fatalf("adaptive settle should return as soon as the frame changes; took %s", took)
	}
	if host.captures < 4 {
		t.Fatalf("expected at least 4 captures, got %d", host.captures)
	}
}

func TestVerifyAfterStillReportsUnchangedAfterBudget(t *testing.T) {
	svc, _ := newSlowService(t, 1000, 250*time.Millisecond)
	if _, _, err := computerAct(svc, `{"action":"observe"}`); err != nil {
		t.Fatalf("observe: %v", err)
	}
	start := time.Now()
	summary, _, err := computerAct(svc, `{"action":"click","name":"保存"}`)
	took := time.Since(start)
	if !errors.Is(err, ErrCcExecFailed) || !strings.Contains(summary, "screen unchanged") {
		t.Fatalf("frozen screen must stay unverified: err=%v summary=%s", err, summary)
	}
	if took < 200*time.Millisecond || took > 1500*time.Millisecond {
		t.Fatalf("should spend roughly the budget and no more: %s", took)
	}
}
