// Package m5 implements the M5 durable chat Root Run adapter (run.send /
// run.cancel, PRD M5 T-5.1.1/T-5.1.2) on top of the M4 execution core.
//
// Write-ahead contract: run.send commits the Root Run row and the first
// user-message event (seq=1) in one transaction before the caller sees a
// result. A failure at any point inside the transaction (M5-RUN-001) leaves
// no durable message, so the renderer never marks a send as delivered unless
// the event is on disk. run.cancel is an idempotent, CAS-guarded state
// transition; illegal transitions answer M5-RUN-002.
package m5

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// Errors map 1:1 onto the M5 wire error registry: RUN-001 (send not durable),
// RUN-002 (cancel request invalid for the run state), RUN-003 (lease/ownership
// conflict). Callers translate with errors.Is.
var (
	ErrIdempotencyKeyRequired = errors.New("m5: idempotency key required")
	ErrIdempotencyConflict    = errors.New("m5: idempotency key replayed with a different request")
	ErrSessionRequired        = errors.New("m5: sessionId is required")
	ErrTextRequired           = errors.New("m5: message text is required")
	ErrTextTooLarge           = errors.New("m5: message text exceeds 131072 bytes")
	ErrRunNotFound            = errors.New("m5: run not found")
	ErrRunTerminal            = errors.New("m5: run is terminal and cannot accept messages")
	ErrSessionMismatch        = errors.New("m5: run belongs to a different session")
	ErrCancelReasonInvalid    = errors.New("m5: cancel reason must be user, policy or timeout")
	ErrCancelStateInvalid     = errors.New("m5: run state does not allow this cancel transition")
)

// TextLimitBytes is the frozen M5 wire limit (run.send schema maxLength).
const TextLimitBytes = 131072

// ChatBudget is the default envelope for a durable chat Root Run. M5 chat has
// no child runs or fan-out, so the envelope only bounds one Root Run.
func ChatBudget() agentrun.Budget {
	return agentrun.Budget{
		MaxModelTurns:       512,
		MaxToolCalls:        2048,
		MaxTokens:           8_000_000,
		MaxCostMicros:       2_000_000_000,
		MaxWallClockSeconds: 86_400,
		MaxOutputBytes:      1_073_741_824,
		MaxRetries:          16,
		MaxNoProgress:       32,
		HardCeiling:         false,
	}
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// RunService is the M5 ChatRootRunAdapter. It shares the M4 single-writer
// transaction boundary, so every mutation is atomic with its audit and
// idempotency records.
type RunService struct {
	uow   agentrunapp.UnitOfWork
	clock Clock
}

func NewRunService(uow agentrunapp.UnitOfWork) *RunService {
	return &RunService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *RunService) SetClock(c Clock) { s.clock = c }

func (s *RunService) available() error {
	if s == nil || s.uow == nil {
		return errors.New("m5: run unit of work unavailable")
	}
	return nil
}

// RunSendAttachment mirrors the run.send schema attachment item.
type RunSendAttachment struct {
	Kind       string `json:"kind"`
	ArtifactID string `json:"artifactId"`
}

// RunSendInput is the canonical run.send request.
type RunSendInput struct {
	RunID       string              `json:"runId"`
	SessionID   string              `json:"sessionId"`
	Text        string              `json:"text"`
	Attachments []RunSendAttachment `json:"attachments"`
}

// RunSendResult is the durable receipt: the event is on disk before Send
// returns, so eventSeq is the authoritative message order.
type RunSendResult struct {
	RunID    string `json:"runId"`
	EventSeq int64  `json:"eventSeq"`
	Status   string `json:"status"`
}

func requestDigest(request any) (string, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Send persists a user message and returns only after the run row and the
// UserMessageAccepted event are committed (write-ahead, M5-RUN-001). With an
// empty RunID it reuses the newest non-terminal run of the session or creates
// one (get-or-create); one conversation maps to exactly one Root Run.
func (s *RunService) Send(ctx context.Context, key, actor string, in RunSendInput) (RunSendResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return RunSendResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return RunSendResult{}, err
	}
	if in.SessionID == "" {
		return RunSendResult{}, ErrSessionRequired
	}
	if in.Text == "" {
		return RunSendResult{}, ErrTextRequired
	}
	if len(in.Text) > TextLimitBytes {
		return RunSendResult{}, ErrTextTooLarge
	}
	for _, a := range in.Attachments {
		if a.Kind != "file" || a.ArtifactID == "" {
			return RunSendResult{}, fmt.Errorf("%w: attachment must be kind=file with artifactId", ErrTextRequired)
		}
	}
	digest, err := requestDigest(in)
	if err != nil {
		return RunSendResult{}, err
	}
	var result RunSendResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("run.send", key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		run, err := resolveChatRun(tx, in, now)
		if err != nil {
			return err
		}
		events, err := tx.ListEvents(run.ID)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"schemaVersion": 1,
			"runId":         run.ID,
			"sessionId":     run.SessionID,
			"text":          in.Text,
			"attachments":   in.Attachments,
			"status":        string(run.Status),
		})
		if err != nil {
			return err
		}
		event := agentrun.RunEvent{
			ID:        ulid.Make().String(),
			RunID:     run.ID,
			Sequence:  int64(len(events)) + 1,
			EventType: "UserMessageAccepted",
			Payload:   payload,
			CreatedAt: now,
		}
		if err := tx.AppendEvent(event); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"runId": run.ID, "eventSeq": event.Sequence, "sessionId": run.SessionID})
		if err := putAudit(tx, "run.message.sent", run.ID, actor, digest, key, meta, now); err != nil {
			return err
		}
		result = RunSendResult{RunID: run.ID, EventSeq: event.Sequence, Status: string(run.Status)}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "run.send", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	if err != nil {
		return RunSendResult{}, err
	}
	return result, nil
}

// resolveChatRun implements get-or-create: an explicit RunID must reference a
// live run of the session; otherwise the newest non-terminal run of the
// session is reused, creating a fresh running Root Run when none exists.
func resolveChatRun(tx agentrunapp.Tx, in RunSendInput, now time.Time) (agentrun.AgentRun, error) {
	if in.RunID != "" {
		run, err := tx.GetRun(in.RunID)
		if err != nil {
			return agentrun.AgentRun{}, fmt.Errorf("%w: %s", ErrRunNotFound, in.RunID)
		}
		if run.SessionID != in.SessionID {
			return agentrun.AgentRun{}, ErrSessionMismatch
		}
		if run.Status.Terminal() {
			return agentrun.AgentRun{}, ErrRunTerminal
		}
		return run, nil
	}
	runs, err := tx.ListRunsBySession(in.SessionID)
	if err != nil {
		return agentrun.AgentRun{}, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if !runs[i].Status.Terminal() {
			return runs[i], nil
		}
	}
	run := agentrun.AgentRun{
		ID:        ulid.Make().String(),
		SessionID: in.SessionID,
		Status:    agentrun.RunRunning,
		Budget:    ChatBudget(),
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := tx.PutRun(run); err != nil {
		return agentrun.AgentRun{}, err
	}
	return run, nil
}

func putAudit(tx agentrunapp.Tx, action, aggregateID, actor, digest, key string, meta []byte, now time.Time) error {
	eventSum := sha256.Sum256([]byte("m5-run-audit\x00" + digest + "\x00" + key + "\x00" + aggregateID + "\x00" + action))
	var id ulid.ULID
	copy(id[:], eventSum[:16])
	return tx.PutAudit(providerapp.Audit{ID: id.String(), Action: action, AggregateID: aggregateID, Actor: actor, Metadata: meta, CreatedAt: now})
}

// RunCancelReason enumerates the frozen cancel reasons.
type RunCancelReason string

const (
	CancelUser   RunCancelReason = "user"
	CancelPolicy RunCancelReason = "policy"
	CancelTimeout RunCancelReason = "timeout"
)

// RunCancelInput is the canonical run.cancel request.
type RunCancelInput struct {
	RunID  string          `json:"runId"`
	Reason RunCancelReason `json:"reason"`
}

// RunCancelResult reports the terminal status observed after cancellation.
// outcome_unknown means cancellation could not prove termination within its
// deadline; the run is fenced and surfaced for manual reconciliation.
type RunCancelResult struct {
	RunID      string    `json:"runId"`
	Status     string    `json:"status"`
	ReleasedAt time.Time `json:"releasedAt"`
}

// Cancel moves a live run to cancelled (user/policy) or outcome_unknown
// (timeout, M5 frozen semantics). Repeated cancels are idempotent: the second
// caller receives the already-terminal snapshot. Cancelling a run in any other
// terminal state is M5-RUN-002.
func (s *RunService) Cancel(ctx context.Context, key, actor string, in RunCancelInput) (RunCancelResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return RunCancelResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return RunCancelResult{}, err
	}
	switch in.Reason {
	case CancelUser, CancelPolicy, CancelTimeout:
	default:
		return RunCancelResult{}, ErrCancelReasonInvalid
	}
	if in.RunID == "" {
		return RunCancelResult{}, ErrRunNotFound
	}
	digest, err := requestDigest(in)
	if err != nil {
		return RunCancelResult{}, err
	}
	var result RunCancelResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("run.cancel", key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		run, err := tx.GetRun(in.RunID)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrRunNotFound, in.RunID)
		}
		switch {
		case run.Status == agentrun.RunCancelled || run.Status == agentrun.RunOutcomeUnknown:
			// Idempotent replay of an earlier cancel: return the snapshot.
		case run.Status.Terminal():
			return fmt.Errorf("%w: %s is %s", ErrCancelStateInvalid, run.ID, run.Status)
		default:
			target := agentrun.RunCancelled
			if in.Reason == CancelTimeout {
				target = agentrun.RunOutcomeUnknown
			}
			run, err = tx.TransitionRun(run.ID, run.Version, target, now)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{
				"schemaVersion": 1,
				"runId":         run.ID,
				"reason":        string(in.Reason),
				"status":        string(run.Status),
				"version":       run.Version,
			})
			if err != nil {
				return err
			}
			events, err := tx.ListEvents(run.ID)
			if err != nil {
				return err
			}
			if err := tx.AppendEvent(agentrun.RunEvent{
				ID:        ulid.Make().String(),
				RunID:     run.ID,
				Sequence:  int64(len(events)) + 1,
				EventType: "RunCancelCompleted",
				Payload:   payload,
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		meta, _ := json.Marshal(map[string]any{"status": string(run.Status), "reason": string(in.Reason), "version": run.Version})
		if err := putAudit(tx, "agent.run.cancelled", run.ID, actor, digest, key, meta, now); err != nil {
			return err
		}
		result = RunCancelResult{RunID: run.ID, Status: string(run.Status), ReleasedAt: now}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "run.cancel", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	if err != nil {
		return RunCancelResult{}, err
	}
	return result, nil
}
