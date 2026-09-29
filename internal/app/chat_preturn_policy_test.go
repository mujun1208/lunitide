package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
)

func TestPreturnHardStopOnlyForCancel(t *testing.T) {
	if preturnHardStop(nil) {
		t.Fatal("nil error aborted the turn")
	}
	if !preturnHardStop(context.Canceled) {
		t.Fatal("cancel continued")
	}
	if !preturnHardStop(context.DeadlineExceeded) {
		t.Fatal("deadline continued")
	}
	if preturnHardStop(errors.New("summary db down")) {
		t.Fatal("optional context error aborted the turn")
	}
}

func TestApprovalWaitKeepsTheTurnUnfinished(t *testing.T) {
	status := settleTurnStatus(turnStatusRunning, bridge.EventCompleted, true, true, nil)
	if status != turnStatusInterrupted {
		t.Fatalf("approval wait status=%q", status)
	}
	if !liveTaskSkipsSessionSummary("继续", chatTurnCheckpoint{Status: status}) {
		t.Fatal("继续 after approval still pays the preturn flush")
	}
	if liveTaskSkipsSessionSummary("帮我写一个关于12星座的长篇分析报告论文", chatTurnCheckpoint{Status: status}) {
		t.Fatal("a new topic skipped preturn after an approval wait")
	}
}

func TestSettleTurnStatusMatchesTheStreamTerminal(t *testing.T) {
	if got := settleTurnStatus(turnStatusRunning, bridge.EventCompleted, false, false, nil); got != turnStatusCompleted {
		t.Fatalf("completed=%q", got)
	}
	if got := settleTurnStatus(turnStatusRunning, bridge.EventCancelled, false, true, nil); got != turnStatusCancelled {
		t.Fatalf("cancelled=%q", got)
	}
	if got := settleTurnStatus(turnStatusRunning, bridge.EventFailed, false, false, errors.New("upstream")); got != turnStatusInterrupted {
		t.Fatalf("failed=%q", got)
	}
	if got := settleTurnStatus(turnStatusRunning, bridge.EventFailed, false, false, nil); got != turnStatusRunning {
		t.Fatalf("empty failure changed status to %q", got)
	}
}
