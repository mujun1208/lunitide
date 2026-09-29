package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
)

// preturnHardStop reports whether a pre-turn helper must abort chat.start.
// Cancel and deadline abort. A failed compaction check, summary read, or
// handoff read leaves the turn on the context it already has.
func preturnHardStop(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// settleTurnStatus records how the stream ended. An approval wait still emits
// a completed event so the card can resolve, but the checkpoint stays
// interrupted. The next "继续" then uses the existing live-task skip.
func settleTurnStatus(current string, terminal bridge.EventType, waitingForApproval, hadTools bool, err error) string {
	if waitingForApproval {
		return turnStatusInterrupted
	}
	switch terminal {
	case bridge.EventCompleted:
		return turnStatusCompleted
	case bridge.EventCancelled:
		return turnStatusCancelled
	default:
		if hadTools || err != nil {
			return turnStatusInterrupted
		}
		return current
	}
}
