package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

const (
	TaskWriteFaultACKLost           = "write.ack_lost"
	TaskWriteFaultBeforeSettle      = "write.before_settle"
	TaskWriteFaultCompleteEventLost = "write.complete_event_lost"
	EventExecutionTaskCompleted     = "ExecutionTaskCompleted"
	taskOutcomeUnknown              = "outcome_unknown"
	taskOutcomeSucceeded            = "succeeded"
)

var taskWriteFault func(string) error

func InstallTaskWriteFault(fn func(string) error) func() {
	prev := taskWriteFault
	taskWriteFault = fn
	return func() { taskWriteFault = prev }
}

func injectTaskWriteFault(point string) error {
	if taskWriteFault != nil {
		return taskWriteFault(point)
	}
	return nil
}

type WriteEffectInput struct {
	Scope           agentrun.ExecutionScope
	Permit          agentrun.CallPermit
	StepID          string
	AttemptID       string
	EffectKey       string
	Path            string
	Content         []byte
	NativeSessionID string
	Usage           agentrun.UsageTotals
	ReceiptID       string
	PayloadDigest   string
}

type WriteEffectResult struct {
	EffectStatus agentrun.EffectStatus
	OutcomeState string
}

type ResumeUnknownInput struct {
	TaskID          string
	NativeSessionID string
}

type LateEffectReceipt struct {
	TaskID, EffectKey, ReceiptID, PayloadDigest string
	SettlementRevision                          int64
	Usage                                       agentrun.UsageTotals
}

type TaskRecoveryResult struct {
	EffectStatus           agentrun.EffectStatus
	OutcomeState           string
	CompletionEventPresent bool
	ClaimedNativeResume    bool
}

type writeAttemptJournal struct {
	SchemaVersion   int    `json:"schemaVersion"`
	EffectKey       string `json:"effectKey"`
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	ReservationID   string `json:"reservationId"`
	NativeSessionID string `json:"nativeSessionId"`
	ACK             bool   `json:"ack"`
	Settled         bool   `json:"settled"`
	Completed       bool   `json:"completed"`
	ReceiptID       string `json:"receiptId,omitempty"`
	PayloadDigest   string `json:"payloadDigest,omitempty"`
	RunID           string `json:"runId"`
	AttemptID       string `json:"attemptId"`
	StepID          string `json:"stepId"`
}

type taskOutcomeDoc struct {
	State        string `json:"state"`
	Version      int64  `json:"version"`
	GoalRevision int64  `json:"goalRevision"`
	Completion   string `json:"completion,omitempty"`
}

type TaskRecoveryService struct {
	uow       ExecutionBudgetUoW
	budget    *ExecutionBudgetService
	clock     Clock
	writeFile func(string, []byte) error
}

func NewTaskRecovery(u ExecutionBudgetUoW) *TaskRecoveryService {
	return &TaskRecoveryService{uow: u, budget: NewExecutionBudget(u), clock: systemClock{}, writeFile: defaultTaskWriteFile}
}

func (s *TaskRecoveryService) SetWriteFile(fn func(string, []byte) error) {
	if fn != nil {
		s.writeFile = fn
	}
}

func defaultTaskWriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *TaskRecoveryService) AttemptWriteEffect(ctx context.Context, in WriteEffectInput) (WriteEffectResult, error) {
	result := WriteEffectResult{EffectStatus: agentrun.EffectPrepared, OutcomeState: taskOutcomeUnknown}
	if in.EffectKey == "" || in.Path == "" || in.AttemptID == "" || in.StepID == "" || in.Permit.ReservationID == "" {
		return result, fmt.Errorf("%w: write effect identity required", agentrun.ErrInvalid)
	}
	digest := in.PayloadDigest
	if digest == "" {
		digest = sha256Hex(in.Content)
	}
	journal := writeAttemptJournal{
		SchemaVersion: 2, EffectKey: in.EffectKey, Path: in.Path, SHA256: sha256Hex(in.Content),
		ReservationID: in.Permit.ReservationID, NativeSessionID: in.NativeSessionID,
		ReceiptID: in.ReceiptID, PayloadDigest: digest, RunID: in.Scope.RunID,
		AttemptID: in.AttemptID, StepID: in.StepID,
	}
	if err := s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectPrepared, ""); err != nil {
		return result, err
	}
	if err := s.writeFile(in.Path, in.Content); err != nil {
		_ = s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectOutcomeUnknown, "")
		return s.currentWriteResult(ctx, in.Scope.TaskID, in.EffectKey, err)
	}
	if err := injectTaskWriteFault(TaskWriteFaultACKLost); err != nil {
		_ = s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectOutcomeUnknown, "")
		return s.currentWriteResult(ctx, in.Scope.TaskID, in.EffectKey, err)
	}
	journal.ACK = true
	if err := s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectPrepared, ""); err != nil {
		return result, err
	}
	if err := injectTaskWriteFault(TaskWriteFaultBeforeSettle); err != nil {
		_ = s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectOutcomeUnknown, "")
		return s.currentWriteResult(ctx, in.Scope.TaskID, in.EffectKey, err)
	}
	if err := s.budget.SettleCall(ctx, agentrun.CallSettlement{
		ReservationID: in.Permit.ReservationID, ReceiptID: in.ReceiptID, PayloadDigest: digest,
		SettlementRevision: 1, Usage: in.Usage, Integrity: "reported", Dispatched: true, Outcome: "completed",
	}); err != nil {
		_ = s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectOutcomeUnknown, "")
		return s.currentWriteResult(ctx, in.Scope.TaskID, in.EffectKey, err)
	}
	journal.Settled = true
	if err := s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectPrepared, ""); err != nil {
		return result, err
	}
	if err := injectTaskWriteFault(TaskWriteFaultCompleteEventLost); err != nil {
		_ = s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectOutcomeUnknown, "")
		return s.currentWriteResult(ctx, in.Scope.TaskID, in.EffectKey, err)
	}
	journal.Completed = true
	if err := s.persistAttempt(ctx, in.Scope.TaskID, journal, agentrun.EffectCommitted, in.ReceiptID); err != nil {
		return result, err
	}
	return WriteEffectResult{EffectStatus: agentrun.EffectCommitted, OutcomeState: taskOutcomeSucceeded}, nil
}

func (s *TaskRecoveryService) ResumeUnknownEffect(ctx context.Context, in ResumeUnknownInput) (TaskRecoveryResult, error) {
	out := TaskRecoveryResult{OutcomeState: taskOutcomeUnknown, ClaimedNativeResume: false}
	var isolates []agentrun.CallSettlement
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		task, err := tx.GetExecutionTask(in.TaskID)
		if err != nil {
			return err
		}
		steps, err := tx.ListExecutionStepOutcomes(in.TaskID)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		if len(steps) == 0 {
			return s.putTaskOutcome(tx, task, taskOutcomeUnknown, "unverified")
		}
		for _, step := range steps {
			journal, ok := decodeWriteJournal(step.OutcomeJSON)
			if !ok {
				continue
			}
			if err = s.reconcileUnknownEffect(tx, journal, now); err != nil {
				return err
			}
			out.EffectStatus = agentrun.EffectOutcomeUnknown
			row, loadErr := tx.LoadExecutionReservation(journal.ReservationID)
			if loadErr == nil && row.Status == "reserved" && row.Dispatched {
				isolates = append(isolates, agentrun.CallSettlement{
					ReservationID:      journal.ReservationID,
					ReceiptID:          "isolate:" + journal.ReservationID,
					PayloadDigest:      sha256Hex([]byte("unknown:" + journal.EffectKey)),
					SettlementRevision: 1,
					Integrity:          "unknown",
					Dispatched:         true,
					Outcome:            taskOutcomeUnknown,
				})
			}
			out.CompletionEventPresent = out.CompletionEventPresent || s.hasCompletionEvent(tx, journal.RunID)
		}
		return s.putTaskOutcome(tx, task, taskOutcomeUnknown, "unverified")
	})
	if err != nil {
		return out, err
	}
	for _, settle := range isolates {
		if err = s.budget.SettleCall(ctx, settle); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *TaskRecoveryService) ApplyLateReceipt(ctx context.Context, in LateEffectReceipt) (TaskRecoveryResult, error) {
	out := TaskRecoveryResult{OutcomeState: taskOutcomeUnknown}
	var journal writeAttemptJournal
	var task ExecutionTaskRow
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		var err error
		task, err = tx.GetExecutionTask(in.TaskID)
		if err != nil {
			return err
		}
		journal, err = s.findJournal(tx, in.TaskID, in.EffectKey)
		if err != nil {
			return err
		}
		if data, readErr := os.ReadFile(journal.Path); readErr == nil {
			if sha256Hex(data) != journal.SHA256 {
				return fmt.Errorf("%w: late receipt artifact digest mismatch", agentrun.ErrInvalid)
			}
		}
		journal.ACK = true
		journal.ReceiptID = in.ReceiptID
		journal.PayloadDigest = in.PayloadDigest
		return s.writeJournal(tx, task, journal, 0)
	})
	if err != nil {
		return out, err
	}
	row, err := s.loadReservation(ctx, journal.ReservationID)
	if err != nil {
		return out, err
	}
	if row.Status != "committed" {
		if err = s.budget.SettleCall(ctx, agentrun.CallSettlement{
			ReservationID: journal.ReservationID, ReceiptID: in.ReceiptID, PayloadDigest: in.PayloadDigest,
			SettlementRevision: in.SettlementRevision, Usage: in.Usage, Integrity: "reported",
			Dispatched: true, Outcome: "completed",
		}); err != nil {
			return out, err
		}
	}
	journal.Settled = true
	journal.Completed = true
	err = s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		if err := s.persistJournalAndEffect(tx, task, journal, agentrun.EffectCommitted, in.ReceiptID); err != nil {
			return err
		}
		if !s.hasCompletionEvent(tx, journal.RunID) {
			if err := appendRunEvent(tx, journal.RunID, EventExecutionTaskCompleted, map[string]any{
				"schemaVersion": 2, "taskId": in.TaskID, "effectKey": in.EffectKey, "receiptId": in.ReceiptID,
			}, s.clock.Now().UTC()); err != nil {
				return err
			}
		}
		return s.putTaskOutcome(tx, task, taskOutcomeSucceeded, "unverified")
	})
	if err != nil {
		return out, err
	}
	out.EffectStatus = agentrun.EffectCommitted
	out.OutcomeState = taskOutcomeSucceeded
	out.CompletionEventPresent = true
	return out, nil
}

func (s *TaskRecoveryService) persistAttempt(ctx context.Context, taskID string, journal writeAttemptJournal, status agentrun.EffectStatus, receiptID string) error {
	return s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		task, err := tx.GetExecutionTask(taskID)
		if err != nil {
			return err
		}
		return s.persistJournalAndEffect(tx, task, journal, status, receiptID)
	})
}

func (s *TaskRecoveryService) persistJournalAndEffect(tx ExecutionBudgetTx, task ExecutionTaskRow, journal writeAttemptJournal, status agentrun.EffectStatus, receiptID string) error {
	if err := s.writeJournal(tx, task, journal, 0); err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	existing, err := tx.GetEffectByKey(journal.EffectKey)
	if err == agentrun.ErrNotFound {
		existing = agentrun.EffectJournal{
			ID: ulid.Make().String(), RunID: journal.RunID, EffectKey: journal.EffectKey,
			RequestDigest: journal.SHA256, Status: agentrun.EffectPrepared, CreatedAt: now, UpdatedAt: now,
		}
		if err = tx.PutEffect(existing); err != nil {
			return err
		}
		if status == agentrun.EffectPrepared && receiptID == "" {
			return s.putTaskOutcome(tx, task, taskOutcomeUnknown, "unverified")
		}
	} else if err != nil {
		return err
	}
	if existing.Status != status {
		resolved, resolveErr := existing.Resolve(status, receiptID, now)
		if resolveErr != nil {
			return resolveErr
		}
		if err = tx.PutEffect(resolved); err != nil {
			return err
		}
	} else if receiptID != "" && existing.ReceiptID != receiptID {
		existing.ReceiptID = receiptID
		existing.UpdatedAt = now
		if err = tx.PutEffect(existing); err != nil {
			return err
		}
	}
	state := taskOutcomeUnknown
	if status == agentrun.EffectCommitted && journal.Completed {
		state = taskOutcomeSucceeded
	}
	return s.putTaskOutcome(tx, task, state, "unverified")
}

func (s *TaskRecoveryService) writeJournal(tx ExecutionBudgetTx, task ExecutionTaskRow, journal writeAttemptJournal, bump int64) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	row, err := tx.GetExecutionStepOutcome(task.TaskID, task.GoalRevision, journal.StepID, journal.AttemptID)
	now := s.clock.Now().UTC()
	if err == agentrun.ErrNotFound {
		return tx.PutExecutionStepOutcome(ExecutionStepOutcomeRow{
			TaskID: task.TaskID, GoalRevision: task.GoalRevision, StepID: journal.StepID, AttemptID: journal.AttemptID,
			State: taskOutcomeUnknown, OutcomeJSON: string(body), Revision: 1, CreatedAt: now,
		})
	}
	if err != nil {
		return err
	}
	row.OutcomeJSON = string(body)
	row.State = taskOutcomeUnknown
	if journal.Completed {
		row.State = taskOutcomeSucceeded
	}
	row.Revision += 1 + bump
	return tx.PutExecutionStepOutcome(row)
}

func (s *TaskRecoveryService) putTaskOutcome(tx ExecutionBudgetTx, task ExecutionTaskRow, state, completion string) error {
	doc := taskOutcomeDoc{State: state, Version: task.Revision, GoalRevision: task.GoalRevision, Completion: completion}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	task.OutcomeJSON = string(body)
	task.UpdatedAt = s.clock.Now().UTC()
	return tx.PutExecutionTask(task)
}

func (s *TaskRecoveryService) reconcileUnknownEffect(tx ExecutionBudgetTx, journal writeAttemptJournal, now time.Time) error {
	effect, err := tx.GetEffectByKey(journal.EffectKey)
	if err == agentrun.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if effect.Status == agentrun.EffectPrepared {
		effect, err = effect.Resolve(agentrun.EffectOutcomeUnknown, "", now)
		if err != nil {
			return err
		}
		return tx.PutEffect(effect)
	}
	return nil
}

func (s *TaskRecoveryService) hasCompletionEvent(tx ExecutionBudgetTx, runID string) bool {
	if runID == "" {
		return false
	}
	events, err := tx.ListEvents(runID)
	if err != nil {
		return false
	}
	for _, event := range events {
		if event.EventType == EventExecutionTaskCompleted {
			return true
		}
	}
	return false
}

func (s *TaskRecoveryService) findJournal(tx ExecutionBudgetTx, taskID, effectKey string) (writeAttemptJournal, error) {
	steps, err := tx.ListExecutionStepOutcomes(taskID)
	if err != nil {
		return writeAttemptJournal{}, err
	}
	for _, step := range steps {
		journal, ok := decodeWriteJournal(step.OutcomeJSON)
		if ok && journal.EffectKey == effectKey {
			return journal, nil
		}
	}
	return writeAttemptJournal{}, agentrun.ErrNotFound
}

func (s *TaskRecoveryService) currentWriteResult(ctx context.Context, taskID, effectKey string, cause error) (WriteEffectResult, error) {
	result := WriteEffectResult{EffectStatus: agentrun.EffectOutcomeUnknown, OutcomeState: taskOutcomeUnknown}
	_ = s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		if effect, err := tx.GetEffectByKey(effectKey); err == nil {
			result.EffectStatus = effect.Status
		}
		return nil
	})
	return result, cause
}

func (s *TaskRecoveryService) loadReservation(ctx context.Context, id string) (ExecutionReservationRow, error) {
	var row ExecutionReservationRow
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		var err error
		row, err = tx.LoadExecutionReservation(id)
		return err
	})
	return row, err
}

func decodeWriteJournal(raw string) (writeAttemptJournal, bool) {
	var journal writeAttemptJournal
	if raw == "" || json.Unmarshal([]byte(raw), &journal) != nil || journal.EffectKey == "" {
		return journal, false
	}
	return journal, true
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
