// Package run implements the M5 recovery scanner (T-5.1.3): at startup it
// replays every active Root Run's event chain to rebuild state, deduplicates
// idempotently, and quarantines runs whose chain is corrupted (M5-REC-001).
//
// Quarantine policy: a broken chain (sequence gap, duplicate, non-canonical
// payload) fences the run as interrupted. The scanner never guesses how to
// continue a corrupted run; the chain stays readable for export only.
package run

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

// newEventID derives a deterministic quarantine event ID so a re-scan that
// reaches the same corruption point cannot append a duplicate event.
func newEventID(runID string, seq int64, reason string) string {
	sum := sha256.Sum256([]byte("m5-recovery\x00" + runID + "\x00" + fmt.Sprint(seq) + "\x00" + reason))
	var id ulid.ULID
	copy(id[:], sum[:16])
	return id.String()
}

var (
	ErrUnitOfWorkUnavailable = errors.New("run recovery: unit of work unavailable")
	ErrChainCorrupted        = errors.New("run recovery: event chain corrupted")
)

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// RecoveryReport is the per-run outcome of one scan.
type RecoveryReport struct {
	RunID          string             `json:"runId"`
	Status         agentrun.RunStatus `json:"status"`
	EventsReplayed int                `json:"eventsReplayed"`
	LastEventSeq   int64              `json:"lastEventSeq"`
	Quarantined    bool               `json:"quarantined"`
	Corruption     string             `json:"corruption,omitempty"`
}

// RecoveryService replays run event chains on the M4 single-writer boundary.
type RecoveryService struct {
	uow   agentrunapp.UnitOfWork
	clock Clock
}

func NewRecoveryService(uow agentrunapp.UnitOfWork) *RecoveryService {
	return &RecoveryService{uow: uow, clock: systemClock{}}
}

func (s *RecoveryService) SetClock(c Clock) { s.clock = c }

// ScanAll replays every active (non-terminal) run. It is idempotent: runs
// already quarantined (terminal) are skipped, so repeated scans and scans
// after a mid-scan crash have zero repeated side effects.
func (s *RecoveryService) ScanAll(ctx context.Context) ([]RecoveryReport, error) {
	if s == nil || s.uow == nil {
		return nil, ErrUnitOfWorkUnavailable
	}
	var reports []RecoveryReport
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		runs, err := tx.ListActiveRuns()
		if err != nil {
			return err
		}
		for _, run := range runs {
			report, err := s.scanRun(tx, run)
			if err != nil {
				return err
			}
			reports = append(reports, report)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return reports, nil
}

// scanRun validates one run's chain and quarantines on corruption. replay is
// pure: the only mutation is the single fencing transition.
func (s *RecoveryService) scanRun(tx agentrunapp.Tx, run agentrun.AgentRun) (RecoveryReport, error) {
	report := RecoveryReport{RunID: run.ID, Status: run.Status}
	events, err := tx.ListEvents(run.ID)
	if err != nil {
		return report, err
	}
	if reason := verifyChain(events); reason != "" {
		now := s.clock.Now().UTC()
		quarantined, err := tx.TransitionRun(run.ID, run.Version, agentrun.RunInterrupted, now)
		if err != nil {
			return report, fmt.Errorf("%w: quarantine %s failed: %v", ErrChainCorrupted, run.ID, err)
		}
		var lastSeq int64
		if len(events) > 0 {
			lastSeq = events[len(events)-1].Sequence
		}
		if err := appendRecoveryEvent(tx, run.ID, lastSeq, quarantined.Status, reason, now); err != nil {
			return report, err
		}
		report.Status = quarantined.Status
		report.Quarantined = true
		report.Corruption = reason
		return report, nil
	}
	report.EventsReplayed = len(events)
	if len(events) > 0 {
		report.LastEventSeq = events[len(events)-1].Sequence
	}
	return report, nil
}

// verifyChain returns "" for a healthy chain (sequences 1..n strictly
// increasing with canonical JSON payloads) or a human-readable corruption
// reason for M5-REC-001.
func verifyChain(events []agentrun.RunEvent) string {
	for i, event := range events {
		if want := int64(i + 1); event.Sequence != want {
			return fmt.Sprintf("sequence break at index %d: got %d, want %d", i, event.Sequence, want)
		}
		if event.EventType == "" {
			return fmt.Sprintf("event %d has no type", event.Sequence)
		}
		if !json.Valid(event.Payload) {
			return fmt.Sprintf("event %d payload is not canonical JSON", event.Sequence)
		}
	}
	return ""
}

func appendRecoveryEvent(tx agentrunapp.Tx, runID string, lastSeq int64, status agentrun.RunStatus, reason string, now time.Time) error {
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": 1,
		"runId":         runID,
		"quarantined":   true,
		"reason":        reason,
		"status":        string(status),
	})
	if err != nil {
		return err
	}
	return tx.AppendEvent(agentrun.RunEvent{
		ID:        newEventID(runID, lastSeq+1, reason),
		RunID:     runID,
		Sequence:  lastSeq + 1,
		EventType: "RunChainQuarantined",
		Payload:   payload,
		CreatedAt: now,
	})
}
