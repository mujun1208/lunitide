package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

type ExecutionBudgetUoW interface {
	TransactExecutionBudget(ctx context.Context, fn func(ExecutionBudgetTx) error) error
}

type ExecutionBudgetTx interface {
	Tx
	SessionExists(sessionID string) error
	EnsureAccountingSession() (sessionID string, err error)
	GetExecutionTask(taskID string) (ExecutionTaskRow, error)
	PutExecutionTask(ExecutionTaskRow) error
	GetExecutionBinding(taskID, scopeID string) (ExecutionBindingRow, error)
	GetExecutionBindingByRef(ownerScope, runKind, executionRef string) (ExecutionBindingRow, error)
	PutExecutionBinding(ExecutionBindingRow) error
	ActiveExecutionUsage(taskID, scopeID string) (agentrun.ExecutionUsageRollup, error)
	InsertExecutionReservation(ExecutionReservationRow) error
	LoadExecutionReservation(id string) (ExecutionReservationRow, error)
	UpdateExecutionReservation(ExecutionReservationRow) error
	InsertCallAttemptIntent(CallAttemptIntentRow) error
	PutExecutionStepOutcome(ExecutionStepOutcomeRow) error
	GetExecutionStepOutcome(taskID string, goalRevision int64, stepID, attemptID string) (ExecutionStepOutcomeRow, error)
	ListExecutionStepOutcomes(taskID string) ([]ExecutionStepOutcomeRow, error)
}

type ExecutionStepOutcomeRow struct {
	TaskID, StepID, AttemptID string
	GoalRevision              int64
	State                     string
	OutcomeJSON               string
	Revision                  int64
	CreatedAt                 time.Time
}

type ExecutionTaskRow struct {
	TaskID, OwnerScope, SessionID string
	GoalRevision                  int64
	SpecJSON                      string
	Policy                        agentrun.ExecutionBudgetPolicy
	PolicyRevision                string
	Revision                      int64
	OutcomeJSON                   string
	ActiveElapsedMS               int64
	ActiveSince, LastHeartbeatAt  string
	RuntimeEpoch                  string
	ActivityIntegrity             string
	CreatedAt, UpdatedAt          time.Time
}

type ExecutionBindingRow struct {
	RunID, OwnerScope, TaskID, ScopeID, ParentScopeID string
	RunKind, ExecutionRef, ExecutionMode              string
	Policy                                            agentrun.ExecutionBudgetPolicy
	PolicyRevision                                    string
	ActiveElapsedMS                                   int64
	ActiveSince, LastHeartbeatAt                      string
	RuntimeEpoch                                      string
	ActivityIntegrity                                 string
	CreatedAt                                         time.Time
}

type ExecutionReservationRow struct {
	ID, RunID, TaskID, ScopeID, AttemptID, RequestDigest string
	Status                                               string
	Dispatched                                           bool
	Integrity                                            string
	Reserved                                             reservationV2
	Settled                                              settledV2
	ReceiptID                                            string
	SettlementRevision                                   int64
	SettlementDigest                                     string
	IsolatedJSON                                         string
	ReservedJSON, CommittedJSON                          string
	CreatedAt, UpdatedAt                                 time.Time
}

type CallAttemptIntentRow struct {
	ID, OwnerScope, TaskID, CallID, AttemptID, Purpose, RequestDigest string
	StartedAt                                                         time.Time
}

type reservationV2 struct {
	SchemaVersion    int   `json:"schemaVersion"`
	InputTokensUpper int64 `json:"inputTokensUpper"`
	OutputTokenCap   int64 `json:"outputTokenCap"`
	OutputBytesCap   int64 `json:"outputBytesCap"`
}

type settledV2 struct {
	SchemaVersion     int   `json:"schemaVersion"`
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	ModelOutputBytes  int64 `json:"modelOutputBytes"`
}

type ExecutionBudgetService struct {
	uow   ExecutionBudgetUoW
	clock Clock
}

func NewExecutionBudget(u ExecutionBudgetUoW) *ExecutionBudgetService {
	return &ExecutionBudgetService{uow: u, clock: systemClock{}}
}

var _ agentrun.ExecutionBudget = (*ExecutionBudgetService)(nil)

func (s *ExecutionBudgetService) EnsureExecutionBinding(ctx context.Context, ownerScope, sessionID, taskID, runKind, executionRef, parentScopeID string, policy agentrun.ExecutionBudgetPolicy) (agentrun.ExecutionScope, error) {
	if err := policy.ValidateEffective(); err != nil {
		return agentrun.ExecutionScope{}, err
	}
	if ownerScope == "" || sessionID == "" || taskID == "" || runKind == "" || executionRef == "" {
		return agentrun.ExecutionScope{}, fmt.Errorf("%w: identity fields required", agentrun.ErrExecutionBinding)
	}
	var out agentrun.ExecutionScope
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		if err := tx.SessionExists(sessionID); err != nil {
			return err
		}
		now := s.clock.Now()
		policyRev := policyRevision(policy)
		task, err := tx.GetExecutionTask(taskID)
		if err == agentrun.ErrNotFound {
			task = ExecutionTaskRow{
				TaskID: taskID, OwnerScope: ownerScope, SessionID: sessionID,
				GoalRevision: 1, SpecJSON: "{}", Policy: policy, PolicyRevision: policyRev,
				Revision: 1, OutcomeJSON: "{}", ActivityIntegrity: "measured",
				CreatedAt: now, UpdatedAt: now,
			}
			if err = tx.PutExecutionTask(task); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if task.OwnerScope != ownerScope || task.SessionID != sessionID {
			return fmt.Errorf("%w: task owner/session mismatch", agentrun.ErrExecutionBinding)
		}
		if existing, getErr := tx.GetExecutionBindingByRef(ownerScope, runKind, executionRef); getErr == nil {
			if existing.TaskID != taskID {
				return fmt.Errorf("%w: execution_ref rebound to another task", agentrun.ErrExecutionBinding)
			}
			out = scopeFrom(existing, task)
			return nil
		} else if getErr != agentrun.ErrNotFound {
			return getErr
		}
		scopeID := executionRef
		if runKind == "task_root" {
			scopeID = taskID
		}
		if parentScopeID != "" {
			parent, parentErr := tx.GetExecutionBinding(taskID, parentScopeID)
			if parentErr != nil {
				return fmt.Errorf("%w: parent scope: %v", agentrun.ErrExecutionBinding, parentErr)
			}
			if parent.ScopeID == scopeID {
				return fmt.Errorf("%w: scope cannot parent itself", agentrun.ErrExecutionBinding)
			}
		} else if runKind != "task_root" {
			return fmt.Errorf("%w: child scope requires parent", agentrun.ErrExecutionBinding)
		}
		runID := ulid.Make().String()
		run := agentrun.AgentRun{
			ID: runID, SessionID: sessionID, Status: agentrun.RunQueued,
			Budget:    agentrun.Budget{PolicyVersion: 2, ExecutionTaskID: taskID},
			Version:   1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err = tx.PutRun(run); err != nil {
			return err
		}
		binding := ExecutionBindingRow{
			RunID: runID, OwnerScope: ownerScope, TaskID: taskID, ScopeID: scopeID,
			ParentScopeID: parentScopeID, RunKind: runKind, ExecutionRef: executionRef,
			ExecutionMode: "accounting_only", Policy: policy, PolicyRevision: policyRev,
			ActivityIntegrity: "measured", CreatedAt: now,
		}
		if err = tx.PutExecutionBinding(binding); err != nil {
			return err
		}
		out = scopeFrom(binding, task)
		return nil
	})
	return out, err
}

func (s *ExecutionBudgetService) EnsureDedicatedExecutionBinding(ctx context.Context, ownerScope, taskID, executionRef string, policy agentrun.ExecutionBudgetPolicy) (agentrun.ExecutionScope, error) {
	if executionRef == "" {
		executionRef = taskID
	}
	var existing agentrun.ExecutionScope
	var found bool
	var sessionID string
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		binding, getErr := tx.GetExecutionBindingByRef(ownerScope, "task_root", executionRef)
		if getErr == nil {
			task, taskErr := tx.GetExecutionTask(binding.TaskID)
			if taskErr != nil {
				return taskErr
			}
			existing = scopeFrom(binding, task)
			found = true
			return nil
		}
		if getErr != agentrun.ErrNotFound {
			return getErr
		}
		var ensErr error
		sessionID, ensErr = tx.EnsureAccountingSession()
		return ensErr
	})
	if err != nil {
		return agentrun.ExecutionScope{}, err
	}
	if found {
		return existing, nil
	}
	return s.EnsureExecutionBinding(ctx, ownerScope, sessionID, ulid.Make().String(), "task_root", executionRef, "", policy)
}

func (s *ExecutionBudgetService) AdmitCall(ctx context.Context, scope agentrun.ExecutionScope, estimate agentrun.CallEstimate) (agentrun.CallPermit, error) {
	if estimate.CallID == "" || estimate.AttemptID == "" || estimate.RequestDigest == "" {
		return agentrun.CallPermit{}, fmt.Errorf("%w: call/attempt/digest required", agentrun.ErrExecutionReservation)
	}
	if err := estimate.CheckContextWindow(); err != nil {
		return agentrun.CallPermit{}, err
	}
	if estimate.InputTokensUpper < 0 || estimate.OutputTokenCap < 0 || estimate.OutputBytesCap < 0 {
		return agentrun.CallPermit{}, fmt.Errorf("%w: estimate must be nonnegative", agentrun.ErrExecutionBudget)
	}
	var permit agentrun.CallPermit
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		task, err := tx.GetExecutionTask(scope.TaskID)
		if err != nil {
			return err
		}
		binding, err := tx.GetExecutionBinding(scope.TaskID, scope.ScopeID)
		if err != nil {
			return err
		}
		if err = task.Policy.ValidateEffective(); err != nil {
			return err
		}
		if err = binding.Policy.ValidateEffective(); err != nil {
			return err
		}
		taskUsage, err := tx.ActiveExecutionUsage(scope.TaskID, "")
		if err != nil {
			return err
		}
		childUsage, err := tx.ActiveExecutionUsage(scope.TaskID, scope.ScopeID)
		if err != nil {
			return err
		}
		request := estimate.RequestedTokens()
		if !admitPolicy(task.Policy, taskUsage, request, estimate.OutputTokenCap, estimate.OutputBytesCap) {
			return fmt.Errorf("%w: parent remaining", agentrun.ErrExecutionBudget)
		}
		if !admitPolicy(binding.Policy, childUsage, request, estimate.OutputTokenCap, estimate.OutputBytesCap) {
			return fmt.Errorf("%w: child remaining", agentrun.ErrExecutionBudget)
		}
		now := s.clock.Now()
		purpose := estimate.Purpose
		if purpose == "" {
			purpose = "model"
		}
		if err = tx.InsertCallAttemptIntent(CallAttemptIntentRow{
			ID: ulid.Make().String(), OwnerScope: task.OwnerScope, TaskID: scope.TaskID,
			CallID: estimate.CallID, AttemptID: estimate.AttemptID, Purpose: purpose,
			RequestDigest: estimate.RequestDigest, StartedAt: now,
		}); err != nil {
			return err
		}
		resID := ulid.Make().String()
		legacy, _ := json.Marshal(agentrun.Usage{Tokens: request, OutputBytes: estimate.OutputBytesCap})
		row := ExecutionReservationRow{
			ID: resID, RunID: binding.RunID, TaskID: scope.TaskID, ScopeID: scope.ScopeID,
			AttemptID: estimate.AttemptID, RequestDigest: estimate.RequestDigest,
			Status: "reserved", Reserved: reservationV2{
				SchemaVersion: 2, InputTokensUpper: estimate.InputTokensUpper,
				OutputTokenCap: estimate.OutputTokenCap, OutputBytesCap: estimate.OutputBytesCap,
			},
			ReservedJSON: string(legacy), CreatedAt: now, UpdatedAt: now,
		}
		if err = tx.InsertExecutionReservation(row); err != nil {
			return err
		}
		permit = agentrun.CallPermit{
			ReservationID:  resID,
			AttemptID:      estimate.AttemptID,
			OutputTokenCap: estimate.OutputTokenCap,
		}
		if binding.Policy.Legacy != nil && binding.Policy.Legacy.Deadline != nil {
			permit.Deadline = binding.Policy.Legacy.Deadline.UTC()
		}
		return nil
	})
	return permit, err
}

func (s *ExecutionBudgetService) MarkDispatched(ctx context.Context, permit agentrun.CallPermit) error {
	return s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		row, err := tx.LoadExecutionReservation(permit.ReservationID)
		if err != nil {
			return err
		}
		if row.Status != "reserved" {
			return fmt.Errorf("%w: mark dispatched requires reserved", agentrun.ErrExecutionReservation)
		}
		row.Dispatched = true
		row.UpdatedAt = s.clock.Now()
		return tx.UpdateExecutionReservation(row)
	})
}

func (s *ExecutionBudgetService) SettleCall(ctx context.Context, settle agentrun.CallSettlement) error {
	if settle.ReservationID == "" || settle.ReceiptID == "" || settle.PayloadDigest == "" {
		return fmt.Errorf("%w: settlement identity required", agentrun.ErrExecutionReservation)
	}
	return s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		row, err := tx.LoadExecutionReservation(settle.ReservationID)
		if err != nil {
			return err
		}
		digest := settle.PayloadDigest
		if row.SettlementRevision != 0 && row.ReceiptID == settle.ReceiptID && row.SettlementRevision == settle.SettlementRevision {
			if row.SettlementDigest != digest {
				return agentrun.ErrExecutionReceiptConflict
			}
			return nil
		}
		if settle.SettlementRevision < row.SettlementRevision {
			return nil
		}
		if row.SettlementRevision != 0 && row.ReceiptID == settle.ReceiptID && row.SettlementDigest != "" && row.SettlementDigest != digest {
			return agentrun.ErrExecutionReceiptConflict
		}
		if settle.Integrity == "reported" {
			if settle.Usage.InputTokens < row.Settled.InputTokens || settle.Usage.OutputTokens < row.Settled.OutputTokens {
				settle.Usage.InputTokens = max64(settle.Usage.InputTokens, row.Settled.InputTokens)
				settle.Usage.OutputTokens = max64(settle.Usage.OutputTokens, row.Settled.OutputTokens)
				row.Integrity = "receipt_conflict"
			}
		}
		row.Settled = settledV2{
			SchemaVersion: 2, InputTokens: settle.Usage.InputTokens, OutputTokens: settle.Usage.OutputTokens,
			CachedInputTokens: settle.Usage.CachedInputTokens, ModelOutputBytes: settle.ModelOutputBytes,
		}
		row.ReceiptID = settle.ReceiptID
		row.SettlementRevision = settle.SettlementRevision
		row.SettlementDigest = digest
		if settle.Dispatched {
			row.Dispatched = true
		}
		switch settle.Integrity {
		case "reported":
			row.Status = "committed"
			row.Integrity = "reported"
			committed, _ := json.Marshal(agentrun.Usage{Tokens: settle.Usage.ConsumedTokens(), OutputBytes: settle.ModelOutputBytes})
			row.CommittedJSON = string(committed)
		case "partial", "unknown":
			row.Status = "isolated"
			row.Integrity = settle.Integrity
		default:
			return fmt.Errorf("%w: integrity %q", agentrun.ErrExecutionReservation, settle.Integrity)
		}
		if settle.Usage.ConsumedTokens() > row.Reserved.InputTokensUpper+row.Reserved.OutputTokenCap {
			row.Integrity = "overrun"
		}
		row.UpdatedAt = s.clock.Now()
		return tx.UpdateExecutionReservation(row)
	})
}

func (s *ExecutionBudgetService) ReleaseUnsent(ctx context.Context, permit agentrun.CallPermit, _ string) error {
	return s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		row, err := tx.LoadExecutionReservation(permit.ReservationID)
		if err != nil {
			return err
		}
		if row.Dispatched || row.Status != "reserved" {
			return fmt.Errorf("%w: only undispatched reserved may release", agentrun.ErrExecutionReservation)
		}
		row.Status = "released"
		row.UpdatedAt = s.clock.Now()
		return tx.UpdateExecutionReservation(row)
	})
}

func (s *ExecutionBudgetService) Snapshot(ctx context.Context, scope agentrun.ExecutionScope) (agentrun.BudgetSnapshot, error) {
	var snap agentrun.BudgetSnapshot
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		task, err := tx.GetExecutionTask(scope.TaskID)
		if err != nil {
			return err
		}
		scopeID := scope.ScopeID
		if scope.ParentScopeID == "" && scope.ScopeID == scope.TaskID {
			scopeID = ""
		}
		usage, err := tx.ActiveExecutionUsage(scope.TaskID, scopeID)
		if err != nil {
			return err
		}
		snap = agentrun.BudgetSnapshot{
			ConsumedTotal: usage.ConsumedTotal, ConsumedOutput: usage.ConsumedOutput,
			ReservedTotal: usage.ReservedTotal, IsolatedTotal: usage.IsolatedTotal,
			TaskRevision: task.Revision, Attempts: usage.Attempts, ActiveMillis: task.ActiveElapsedMS,
			ConsumedOutputBytes: usage.ConsumedOutputBytes, ReservedOutputBytes: usage.ReservedOutputBytes,
			IsolatedOutputBytes: usage.IsolatedOutputBytes, Overrun: usage.Overrun, Integrity: usage.Integrity,
		}
		return nil
	})
	return snap, err
}

func (s *ExecutionBudgetService) TransitionActivity(ctx context.Context, scope agentrun.ExecutionScope, tr agentrun.ActivityTransition) (agentrun.ActivityResult, error) {
	var result agentrun.ActivityResult
	err := s.uow.TransactExecutionBudget(ctx, func(tx ExecutionBudgetTx) error {
		task, err := tx.GetExecutionTask(scope.TaskID)
		if err != nil {
			return err
		}
		if tr.ExpectedRevision != 0 && tr.ExpectedRevision != task.Revision {
			return agentrun.ErrVersionConflict
		}
		task.Revision++
		task.UpdatedAt = tr.At
		if tr.At.IsZero() {
			task.UpdatedAt = s.clock.Now()
		}
		if err = tx.PutExecutionTask(task); err != nil {
			return err
		}
		result = agentrun.ActivityResult{TaskRevision: task.Revision, ActiveElapsedMillis: task.ActiveElapsedMS, Integrity: task.ActivityIntegrity}
		return nil
	})
	return result, err
}

func admitPolicy(policy agentrun.ExecutionBudgetPolicy, usage agentrun.ExecutionUsageRollup, tokens, outputTokens, outputBytes int64) bool {
	if policy.MaxTotalTokens != nil && !agentrun.CanReserve(*policy.MaxTotalTokens, usage.ConsumedTotal, usage.ReservedTotal, usage.IsolatedTotal, tokens) {
		return false
	}
	if policy.MaxOutputTokens != nil && !agentrun.CanReserve(*policy.MaxOutputTokens, usage.ConsumedOutput, 0, 0, outputTokens) {
		return false
	}
	if policy.MaxOutputBytes != nil && !agentrun.CanReserve(*policy.MaxOutputBytes, usage.ConsumedOutputBytes, usage.ReservedOutputBytes, usage.IsolatedOutputBytes, outputBytes) {
		return false
	}
	if policy.MaxModelAttempts != nil && !agentrun.CanReserve(*policy.MaxModelAttempts, usage.Attempts, 0, 0, 1) {
		return false
	}
	return true
}

func scopeFrom(binding ExecutionBindingRow, task ExecutionTaskRow) agentrun.ExecutionScope {
	return agentrun.ExecutionScope{
		TaskID: binding.TaskID, RunID: binding.RunID, ScopeID: binding.ScopeID,
		ParentScopeID: binding.ParentScopeID, GoalRevision: task.GoalRevision, PolicyRevision: binding.PolicyRevision,
	}
}

func policyRevision(policy agentrun.ExecutionBudgetPolicy) string {
	body, _ := json.Marshal(policy)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
