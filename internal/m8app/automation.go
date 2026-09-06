// M8 slice-4 application service (T-8.4.x): automation.dispatch.
//
// Dispatch enacts the M8-021~026 contract on top of the workflow_bundles /
// automation_runs projection (migration 0064): quarantined bundles and
// checksum mismatches quarantine with zero dispatch, actions outside the
// permission allow list are denied, budgets over the bundle ceiling are
// refused, high-risk actions wait at the execution point for just-in-time
// confirmation, and idempotency keys replay the original run. AutomationRun
// is an M5/M6 canonical Run projection - M8 never advances run state
// itself, so dispatch only writes the projection rows it owns.
package m8app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 slice-4 error family (04 错误矩阵 M8-021~026).
var (
	// ErrBundleNotFound: bundle row missing.
	ErrBundleNotFound = errors.New("m8app: bundle not found")
	// ErrBundleChecksumInvalid: checksum precheck failed; zero dispatch
	// (M8-021).
	ErrBundleChecksumInvalid = errors.New("m8app: bundle checksum invalid")
	// ErrBundlePermissionDenied: permission precheck failed; zero
	// dispatch (M8-022).
	ErrBundlePermissionDenied = errors.New("m8app: bundle permission denied")
	// ErrAutomationConfirmationRequired: high-risk action waiting at the
	// execution point (M8-023).
	ErrAutomationConfirmationRequired = errors.New("m8app: automation confirmation required")
	// ErrAutomationBudgetExceeded: budget over the bundle ceiling
	// (M8-024).
	ErrAutomationBudgetExceeded = errors.New("m8app: automation budget exceeded")
	// ErrRunQuarantined: run or bundle quarantined (M8-026).
	ErrAutomationIdempotencyConflict = errors.New("m8app: automation request key belongs to different input")
	ErrRunQuarantined                = errors.New("m8app: run quarantined")
)

// AutomationTx is the slice-4 single-writer transaction.
type AutomationTx interface {
	GetBundle(bundleID string) (m8core.WorkflowBundle, error)
	PutBundle(m8core.WorkflowBundle) error
	GetRunByIdempotencyKey(key string) (m8core.AutomationRun, bool, error)
	PutAutomationRun(m8core.AutomationRun) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// AutomationUnitOfWork is the slice-4 single-writer boundary.
type AutomationUnitOfWork interface {
	TransactAutomation(ctx context.Context, fn func(AutomationTx) error) error
}

// AutomationService implements the slice-4 use cases.
type AutomationService struct {
	uow   AutomationUnitOfWork
	clock Clock
}

// NewAutomationService wires the slice-4 service.
func NewAutomationService(uow AutomationUnitOfWork) *AutomationService {
	return &AutomationService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the clock (tests).
func (s *AutomationService) SetClock(c Clock) { s.clock = c }

// RegisterBundleInput feeds the internal verify path (the plugin slice
// shares the bundle table; here bundles arrive pre-verified).
type RegisterBundleInput struct {
	BundleID    string
	Version     int64
	Checksum    string
	Permissions m8core.BundlePermissions
	RollbackRef string
}

// RegisterBundle stores one verified bundle (idempotent on checksum).
func (s *AutomationService) RegisterBundle(ctx context.Context, in RegisterBundleInput) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	if len(in.BundleID) != 26 || in.Version < 1 || !m8core.ValidHexDigest(in.Checksum) {
		return fmt.Errorf("%w: bundle fields invalid", ErrPayloadInvalid)
	}
	perms, err := json.Marshal(in.Permissions)
	if err != nil {
		return err
	}
	return s.uow.TransactAutomation(ctx, func(tx AutomationTx) error {
		return tx.PutBundle(m8core.WorkflowBundle{
			ID: in.BundleID, Version: in.Version, Checksum: in.Checksum,
			Permissions: string(perms), RollbackRef: in.RollbackRef,
			State:     m8core.BundleVerified,
			CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
		})
	})
}

// DispatchInput is the automation.dispatch command.
type DispatchInput struct {
	BundleID      string
	BundleVersion int64
	Trigger       json.RawMessage
	Budget        json.RawMessage
	RequestID     string
	Actor         string
}

// DispatchResult is the automation.dispatch outcome.
type DispatchResult struct {
	ExecutionStarted bool   `json:"executionStarted"`
	RunID            string `json:"runId"`
	State            string `json:"state"`
}

// budgetDoc is the decoded budget payload.
type budgetDoc struct {
	MaxTokens *int64 `json:"maxTokens"`
}

// triggerDoc is the decoded trigger payload.
type triggerDoc struct {
	Type    string   `json:"type"`
	Actions []string `json:"actions"`
}

// Dispatch runs the precheck chain then writes the run projection. The
// idempotency key is the requestId: replays answer the original run id and
// state without re-running prechecks (migration 0064 unique key).
func (s *AutomationService) Dispatch(ctx context.Context, in DispatchInput) (DispatchResult, error) {
	if s == nil || s.uow == nil {
		return DispatchResult{}, ErrServiceUnavailable
	}
	if len(in.BundleID) != 26 || in.BundleVersion < 1 ||
		len(in.Trigger) < 2 || len(in.Trigger) > 64<<10 || len(in.Budget) < 2 || len(in.Budget) > 1024 ||
		len(in.RequestID) < 1 || len(in.RequestID) > m8core.MaxIdempotencyKey {
		return DispatchResult{}, fmt.Errorf("%w: dispatch fields invalid", ErrPayloadInvalid)
	}
	var trig triggerDoc
	if err := decodeAutomationObject(in.Trigger, &trig); err != nil {
		return DispatchResult{}, fmt.Errorf("%w: trigger invalid", ErrPayloadInvalid)
	}
	var bud budgetDoc
	if err := decodeAutomationObject(in.Budget, &bud); err != nil {
		return DispatchResult{}, fmt.Errorf("%w: budget invalid", ErrPayloadInvalid)
	}
	if trig.Type == "" || len(trig.Type) > 64 || len(trig.Actions) > 100 || bud.MaxTokens == nil || *bud.MaxTokens < 0 {
		return DispatchResult{}, ErrPayloadInvalid
	}
	for _, action := range trig.Actions {
		if action == "" || len(action) > 256 {
			return DispatchResult{}, ErrPayloadInvalid
		}
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	canonical, _ := json.Marshal(struct {
		BundleID string
		Version  int64
		Trigger  triggerDoc
		Budget   budgetDoc
		Actor    string
	}{in.BundleID, in.BundleVersion, trig, bud, actorOr(in.Actor)})
	inputDigest := m8core.DigestOf(string(canonical))
	var refusal error
	var out DispatchResult
	err := s.uow.TransactAutomation(ctx, func(tx AutomationTx) error {
		if prior, has, err := tx.GetRunByIdempotencyKey(in.RequestID); err != nil {
			return err
		} else if has {
			if prior.BundleID != in.BundleID || prior.InputDigest != inputDigest {
				return ErrAutomationIdempotencyConflict
			}
			out = DispatchResult{RunID: prior.ID, State: dispatchView(prior.State)}
			if prior.State == m8core.RunQuarantined {
				var saved struct {
					Code   string `json:"code"`
					Reason string `json:"reason"`
				}
				if json.Unmarshal([]byte(prior.CheckpointJSON), &saved) != nil {
					refusal = ErrRunQuarantined
				} else {
					refusal = dispatchBlockedError(saved.Code, saved.Reason)
				}
			}
			return nil
		}
		b, err := tx.GetBundle(in.BundleID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrBundleNotFound, in.BundleID)
		}
		if err != nil {
			return err
		}
		if b.Version != in.BundleVersion {
			return fmt.Errorf("%w: bundle version %d != %d", ErrBundleChecksumInvalid, b.Version, in.BundleVersion)
		}
		perms, err := b.DecodePermissions()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBundleChecksumInvalid, err)
		}
		highRisk := map[string]bool{}
		for _, a := range perms.HighRisk {
			highRisk[a] = true
		}
		oc := m8core.Precheck(m8core.PrecheckInput{
			BundleID: b.ID, BundleVersion: b.Version, BundleState: b.State,
			Checksum: b.Checksum, Permissions: perms,
			TriggerActions: trig.Actions, BudgetTokens: *bud.MaxTokens,
			HighRiskHit: func(action string) bool { return highRisk[action] },
		})
		switch oc.Decision {
		case "blocked":
			// Zero dispatch: park the run projection at QUARANTINED so
			// the refusal is auditable and replayable.
			run := m8core.AutomationRun{
				ID: ulid.Make().String(), BundleID: b.ID,
				State:          m8core.RunQuarantined,
				BudgetJSON:     string(in.Budget),
				IdempotencyKey: in.RequestID,
				InputDigest:    inputDigest, CreatedAt: now,
			}
			decision, _ := json.Marshal(struct {
				Code   string `json:"code"`
				Reason string `json:"reason"`
			}{oc.Code, oc.Reason})
			run.CheckpointJSON = string(decision)
			if err := tx.PutAutomationRun(run); err != nil {
				return err
			}
			if _, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "automation.dispatch.blocked",
				ResourceType: "automation_run", ResourceID: run.ID,
				Actor: actorOr(in.Actor), CorrelationID: oc.Code,
				AfterDigest: inputDigest, CreatedAt: now,
			}); err != nil {
				return err
			}
			out = DispatchResult{RunID: run.ID, State: "blocked"}
			refusal = dispatchBlockedError(oc.Code, oc.Reason)
			return nil
		case "waiting_confirmation":
			run := m8core.AutomationRun{
				ID: ulid.Make().String(), BundleID: b.ID,
				State:          m8core.RunWaitingConfirmation,
				BudgetJSON:     string(in.Budget),
				IdempotencyKey: in.RequestID,
				InputDigest:    inputDigest, CreatedAt: now,
			}
			if err := tx.PutAutomationRun(run); err != nil {
				return err
			}
			if _, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "automation.dispatch.waiting_confirmation",
				ResourceType: "automation_run", ResourceID: run.ID,
				Actor: actorOr(in.Actor), CorrelationID: "M8-023",
				AfterDigest: inputDigest, CreatedAt: now,
			}); err != nil {
				return err
			}
			out = DispatchResult{RunID: run.ID, State: "waiting_confirmation"}
			return nil
		}
		// Dispatch: the projection row lands at DISPATCHED; the actual
		// execution is owned by the M5/M6 kernel (M8 never advances it).
		run := m8core.AutomationRun{
			ID: ulid.Make().String(), BundleID: b.ID,
			State:          m8core.RunDispatched,
			BudgetJSON:     string(in.Budget),
			IdempotencyKey: in.RequestID,
			InputDigest:    inputDigest, CreatedAt: now,
		}
		if err := tx.PutAutomationRun(run); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "automation.dispatch",
			ResourceType: "automation_run", ResourceID: run.ID,
			Actor: actorOr(in.Actor), CorrelationID: in.RequestID,
			AfterDigest: inputDigest, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = DispatchResult{RunID: run.ID, State: "dispatched"}
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, refusal
}

// dispatchView maps the stored run state onto the wire state enum.
func dispatchView(state string) string {
	switch state {
	case m8core.RunWaitingConfirmation:
		return "waiting_confirmation"
	case m8core.RunQuarantined:
		return "blocked"
	}
	return "dispatched"
}

// dispatchBlockedError answers the typed refusal of a blocked precheck.
func dispatchBlockedError(code, reason string) error {
	switch code {
	case "M8-021":
		return fmt.Errorf("%w: %s", ErrBundleChecksumInvalid, reason)
	case "M8-022":
		return fmt.Errorf("%w: %s", ErrBundlePermissionDenied, reason)
	case "M8-024":
		return fmt.Errorf("%w: %s", ErrAutomationBudgetExceeded, reason)
	case "M8-026":
		return fmt.Errorf("%w: %s", ErrRunQuarantined, reason)
	}
	return fmt.Errorf("m8app: dispatch blocked: %s", reason)
}

func decodeAutomationObject(raw []byte, dst any) error {
	if !json.Valid(raw) || len(bytes.TrimSpace(raw)) < 2 || bytes.TrimSpace(raw)[0] != '{' {
		return ErrPayloadInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
