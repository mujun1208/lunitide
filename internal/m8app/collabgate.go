// M8 FR-17 application service (G1/G2): collabGate.evaluate / status /
// confirm plus the CheckGate preflight every write-collaboration
// orchestration call must pass.
//
// The capability stays disabled through all of M8: evaluate only produces
// the append-only snapshot and (on pass) a pending one-time-token decision;
// confirm enacts the single-use token, the expiry and the binding-drift
// rollback; status is a snapshot read that leaks zero orchestration detail
// while disabled. Evidence aggregation is read-only over M7 subagent audit
// and the M5/M6 EffectJournal; any unreadable source is M8-033 fail-closed.
package m8app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 FR-17 error family (04 错误矩阵 M8-028~034).
var (
	// ErrGateDisabled: a write-collaboration call reached the preflight
	// while the capability is disabled (M8-028, 403, zero side effects).
	ErrGateDisabled = errors.New("m8app: collab gate capability disabled")
	// ErrEvidenceUnavailable: an evidence source is unreadable - refuse the
	// whole evaluation (M8-033, 503 fail-closed, zero snapshots).
	ErrEvidenceUnavailable = errors.New("m8app: collab gate evidence unavailable")
	// ErrDecisionTokenInvalid: wrong token, expired or replayed decision
	// (M8-031; the decision moves to expired/revoked).
	ErrDecisionTokenInvalid = errors.New("m8app: collab gate decision token invalid")
	// ErrGateBindingDrift: the runtime policy/digest pair drifted from the
	// confirmed decision binding (M8-032; capability flips to disabled).
	ErrGateBindingDrift = errors.New("m8app: collab gate binding drift")
	// ErrDecisionNotFound: the decision row is missing.
	ErrDecisionNotFound = errors.New("m8app: collab gate decision not found")
)

// DecisionTTL freezes the pending-decision validity window (24h).
const DecisionTTL = 24 * time.Hour

// EvidenceSource aggregates the read-only evaluation evidence over one
// window. Production wires the M7 subagent audit + M5/M6 EffectJournal
// projection; tests inject fixtures. An error is M8-033 fail-closed.
type EvidenceSource interface {
	Aggregate(ctx context.Context, subjectID string, windowStart, windowEnd int64) (m8core.GateEvidence, error)
}

// CollabGateTx is the FR-17 single-writer transaction surface.
type CollabGateTx interface {
	GetEvaluationByKey(subjectID string, windowStart, windowEnd int64, criteriaVersion string) (m8core.GateEvaluation, bool, error)
	GetEvaluationByID(evaluationID string) (m8core.GateEvaluation, bool, error)
	PutEvaluation(m8core.GateEvaluation) error
	PutDecision(m8core.GateDecision) error
	GetDecision(decisionID string) (m8core.GateDecision, bool, error)
	SetDecisionState(decisionID, state, confirmedAt string) error
	LatestConfirmedDecision(subjectID string) (m8core.GateDecision, bool, error)
	LatestPendingDecision(subjectID string) (m8core.GateDecision, bool, error)
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// CollabGateUnitOfWork is the FR-17 single-writer boundary.
type CollabGateUnitOfWork interface {
	TransactCollabGate(ctx context.Context, fn func(CollabGateTx) error) error
}

// CollabGateService owns the evaluation/decision runtime.
type CollabGateService struct {
	uow     CollabGateUnitOfWork
	evidence EvidenceSource
	binding m8core.CapabilityBinding
	clock   Clock
}

// NewCollabGateService wires the gate. The binding freezes the runtime
// policy/capability digest pair every decision pins.
func NewCollabGateService(uow CollabGateUnitOfWork, evidence EvidenceSource, binding m8core.CapabilityBinding) *CollabGateService {
	return &CollabGateService{uow: uow, evidence: evidence, binding: binding, clock: systemClock{}}
}

// SetClock substitutes the clock (tests).
func (s *CollabGateService) SetClock(c Clock) { s.clock = c }

// EvaluateInput is the collabGate.evaluate command.
type EvaluateInput struct {
	SubjectID       string
	WindowStart     int64
	WindowEnd       int64
	CriteriaVersion string
}

// EvaluateResult is the collabGate.evaluate outcome.
type EvaluateResult struct {
	EvaluationID   string   `json:"evaluationId"`
	Outcome        string   `json:"outcome"`
	FailedCriteria []string `json:"failedCriteria"`
	EvidenceDigest string   `json:"evidenceDigest"`
}

// Evaluate enacts G1: aggregate the read-only evidence (M8-033 on any
// unreadable source), adjudicate the frozen criteria chain, persist the
// append-only snapshot idempotently, and - only on pass with the capability
// disabled - mint the pending enable decision (single-use token). While
// enabled, any completed evaluation mints the pending disable decision so
// the user-side shutdown keeps the same token path (no free-form off).
func (s *CollabGateService) Evaluate(ctx context.Context, in EvaluateInput) (EvaluateResult, error) {
	if s == nil || s.uow == nil {
		return EvaluateResult{}, ErrServiceUnavailable
	}
	if len(in.SubjectID) < 1 || len(in.SubjectID) > 128 ||
		in.WindowStart < 0 || in.WindowEnd <= in.WindowStart ||
		len(in.CriteriaVersion) < 1 || len(in.CriteriaVersion) > 64 {
		return EvaluateResult{}, ErrPayloadInvalid
	}
	// M8-033 fail-closed: refuse the whole evaluation when a source is
	// unreadable - no partial snapshots.
	evidence, err := s.evidence.Aggregate(ctx, in.SubjectID, in.WindowStart, in.WindowEnd)
	if err != nil {
		return EvaluateResult{}, fmt.Errorf("%w: %v", ErrEvidenceUnavailable, err)
	}
	outcome, failed := m8core.Adjudicate(evidence)
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out EvaluateResult
	err = s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		// Idempotency: the same four-part key answers the original
		// evaluation without re-adjudicating or re-minting decisions.
		if existing, has, gerr := tx.GetEvaluationByKey(in.SubjectID, in.WindowStart, in.WindowEnd, in.CriteriaVersion); gerr != nil {
			return gerr
		} else if has {
			out = EvaluateResult{
				EvaluationID: existing.EvaluationID, Outcome: existing.Outcome,
				FailedCriteria: existing.FailedCriteria,
				EvidenceDigest: existing.EvidenceDigest,
			}
			return nil
		}
		ev := m8core.GateEvaluation{
			EvaluationID:    ulid.Make().String(),
			SubjectID:       in.SubjectID,
			WindowStart:     in.WindowStart,
			WindowEnd:       in.WindowEnd,
			EvidenceJSON:    string(evidence.CanonicalJSON()),
			EvidenceDigest:  evidence.Digest(),
			CriteriaVersion: in.CriteriaVersion,
			Outcome:         outcome,
			FailedCriteria:  failed,
			CreatedAt:       now,
		}
		if perr := tx.PutEvaluation(ev); perr != nil {
			return perr
		}
		out = EvaluateResult{
			EvaluationID: ev.EvaluationID, Outcome: outcome,
			FailedCriteria: failed, EvidenceDigest: ev.EvidenceDigest,
		}
		// Decision minting: pass + disabled -> pending enable; enabled ->
		// pending disable (the user shutdown path keeps the token flow).
		capability, _, derr := s.capabilityLocked(tx, in.SubjectID)
		if derr != nil {
			return derr
		}
		if (outcome == m8core.EvalPass && capability == m8core.CapabilityDisabled) ||
			capability == m8core.CapabilityEnabled {
			action := m8core.DecisionEnable
			if capability == m8core.CapabilityEnabled {
				action = m8core.DecisionDisable
			}
			token, terr := newDecisionToken()
			if terr != nil {
				return terr
			}
			created := s.clock.Now().UTC()
			d := m8core.GateDecision{
				DecisionID: ulid.Make().String(), EvaluationID: ev.EvaluationID,
				SubjectID: in.SubjectID, DecisionToken: token,
				PolicyVersion: s.binding.PolicyVersion, CapabilityDigest: s.binding.CapabilityDigest,
				Action: action, State: m8core.DecisionPending,
				ExpiresAt: created.Add(DecisionTTL).Format(time.RFC3339),
				CreatedAt: created.Format(time.RFC3339),
			}
			if perr := tx.PutDecision(d); perr != nil {
				return perr
			}
		}
		_, aerr := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "collabGate.evaluate",
			ResourceType: "collab_gate_evaluation", ResourceID: ev.EvaluationID,
			Actor: in.SubjectID, CorrelationID: in.CriteriaVersion,
			AfterDigest: ev.EvidenceDigest, CreatedAt: now,
		})
		return aerr
	})
	if err != nil {
		return EvaluateResult{}, err
	}
	return out, nil
}

// StatusInput is the collabGate.status command.
type StatusInput struct {
	SubjectID string
}

// StatusResult is the collabGate.status outcome. While disabled the answer
// carries nothing but the capability flag (zero orchestration detail); the
// enabled answer exposes the binding quartet for reconciliation.
type StatusResult struct {
	Capability       string `json:"capability"`
	EvaluationID     string `json:"evaluationId,omitempty"`
	DecisionID       string `json:"decisionId,omitempty"`
	PolicyVersion    string `json:"policyVersion,omitempty"`
	CapabilityDigest string `json:"capabilityDigest,omitempty"`
	EffectiveAt      string `json:"effectiveAt,omitempty"`
}

// Status enacts the snapshot read: derive the capability from the latest
// confirmed decision, roll back on runtime binding drift (M8-032, audited),
// and hide every detail while disabled.
func (s *CollabGateService) Status(ctx context.Context, in StatusInput) (StatusResult, error) {
	if s == nil || s.uow == nil {
		return StatusResult{}, ErrServiceUnavailable
	}
	if len(in.SubjectID) < 1 || len(in.SubjectID) > 128 {
		return StatusResult{}, ErrPayloadInvalid
	}
	var out StatusResult
	err := s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		capability, decision, err := s.capabilityLocked(tx, in.SubjectID)
		if err != nil {
			return err
		}
		if capability != m8core.CapabilityEnabled {
			out = StatusResult{Capability: m8core.CapabilityDisabled}
			return nil
		}
		out = StatusResult{
			Capability:       m8core.CapabilityEnabled,
			DecisionID:       decision.DecisionID,
			PolicyVersion:    decision.PolicyVersion,
			CapabilityDigest: decision.CapabilityDigest,
			EffectiveAt:      decision.ConfirmedAt,
		}
		if ev, has, gerr := tx.GetEvaluationByID(decision.EvaluationID); gerr != nil {
			return gerr
		} else if has {
			out.EvaluationID = ev.EvaluationID
		}
		return nil
	})
	return out, err
}

// GateConfirmInput is the collabGate.confirm command.
type GateConfirmInput struct {
	DecisionID    string
	DecisionToken string
}

// GateConfirmResult is the collabGate.confirm outcome.
type GateConfirmResult struct {
	Capability  string `json:"capability"`
	EffectiveAt string `json:"effectiveAt"`
}

// Confirm enacts G2: the token is single-use; expiry/replay answers M8-031
// (decision -> expired/revoked); a runtime binding drift answers M8-032 and
// flips the capability back to disabled; a fresh pending token confirms
// atomically with the audit event. Re-confirming an already-confirmed
// decision with the same token is the idempotent "already effective" read.
//
// Punish transitions (revoke/expire) persist in their own committed
// transaction before the error is answered - a rollback of the failed call
// must not resurrect a replayed or drifted decision.
func (s *CollabGateService) Confirm(ctx context.Context, in GateConfirmInput) (GateConfirmResult, error) {
	if s == nil || s.uow == nil {
		return GateConfirmResult{}, ErrServiceUnavailable
	}
	if len(in.DecisionID) != 26 || len(in.DecisionToken) != 64 {
		return GateConfirmResult{}, ErrDecisionTokenInvalid
	}
	nowT := s.clock.Now().UTC()
	now := nowT.Format(time.RFC3339)
	var out GateConfirmResult
	// punish captures the terminal transition a refused call must still
	// persist (empty when the path confirms or answers idempotently).
	var punish struct {
		decisionID string
		state      string
		auditEvent string
		actor      string
	}
	confirmErr := s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		d, has, gerr := tx.GetDecision(in.DecisionID)
		if gerr != nil {
			return gerr
		}
		if !has {
			return ErrDecisionNotFound
		}
		// M8-032 preflight: any runtime drift rolls the capability back to
		// disabled and revokes the decision before token semantics apply.
		if !s.binding.Matches(m8core.CapabilityBinding{PolicyVersion: d.PolicyVersion, CapabilityDigest: d.CapabilityDigest}) {
			if d.State == m8core.DecisionPending || d.State == m8core.DecisionConfirmed {
				punish.decisionID, punish.state = d.DecisionID, m8core.DecisionRevoked
				punish.auditEvent, punish.actor = "collabGate.binding.drift", d.SubjectID
			}
			return ErrGateBindingDrift
		}
		switch d.State {
		case m8core.DecisionConfirmed:
			if d.DecisionToken != in.DecisionToken {
				punish.decisionID, punish.state = d.DecisionID, m8core.DecisionRevoked
				punish.auditEvent, punish.actor = "collabGate.decision.revoked", d.SubjectID
				return ErrDecisionTokenInvalid
			}
			// Idempotent: the same token re-reads the effective state.
			capability := m8core.CapabilityDisabled
			if d.Action == m8core.DecisionEnable {
				capability = m8core.CapabilityEnabled
			}
			out = GateConfirmResult{Capability: capability, EffectiveAt: d.ConfirmedAt}
			return nil
		case m8core.DecisionPending:
			if d.DecisionToken != in.DecisionToken {
				punish.decisionID, punish.state = d.DecisionID, m8core.DecisionRevoked
				punish.auditEvent, punish.actor = "collabGate.decision.revoked", d.SubjectID
				return ErrDecisionTokenInvalid
			}
			if nowT.Format(time.RFC3339) >= d.ExpiresAt {
				punish.decisionID, punish.state = d.DecisionID, m8core.DecisionExpired
				punish.auditEvent, punish.actor = "collabGate.decision.expired", d.SubjectID
				return ErrDecisionTokenInvalid
			}
			if serr := tx.SetDecisionState(d.DecisionID, m8core.DecisionConfirmed, now); serr != nil {
				return serr
			}
			capability := m8core.CapabilityDisabled
			if d.Action == m8core.DecisionEnable {
				capability = m8core.CapabilityEnabled
			}
			_, aerr := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "collabGate.confirm",
				ResourceType: "collab_gate_decision", ResourceID: d.DecisionID,
				Actor: d.SubjectID, AfterDigest: d.CapabilityDigest, CreatedAt: now,
			})
			if aerr != nil {
				return aerr
			}
			out = GateConfirmResult{Capability: capability, EffectiveAt: now}
			return nil
		default: // expired / revoked
			return ErrDecisionTokenInvalid
		}
	})
	// Persist the punish transition outside the refused transaction so the
	// rollback of the failed call cannot resurrect the decision.
	if confirmErr != nil {
		if punish.decisionID != "" {
			perr := s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
				if serr := tx.SetDecisionState(punish.decisionID, punish.state, ""); serr != nil {
					return serr
				}
				_, aerr := tx.AppendAuditEvent(audit.Event{
					ID: ulid.Make().String(), Action: punish.auditEvent,
					ResourceType: "collab_gate_decision", ResourceID: punish.decisionID,
					Actor: punish.actor, CreatedAt: now,
				})
				return aerr
			})
			if perr != nil {
				return GateConfirmResult{}, fmt.Errorf("%w (punish persist: %v)", confirmErr, perr)
			}
		}
		return GateConfirmResult{}, confirmErr
	}
	return out, nil
}

// CheckGate is the preflight every write-collaboration orchestration call
// must pass: the capability must be enabled and the runtime binding must
// still match the confirmed decision. Anything else is M8-028 with zero
// side effects (drift rolls back first, audited).
func (s *CollabGateService) CheckGate(ctx context.Context, subjectID string) error {
	if s == nil || s.uow == nil {
		return ErrGateDisabled
	}
	if len(subjectID) < 1 || len(subjectID) > 128 {
		return ErrPayloadInvalid
	}
	return s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		capability, _, err := s.capabilityLocked(tx, subjectID)
		if err != nil {
			return err
		}
		if capability != m8core.CapabilityEnabled {
			return ErrGateDisabled
		}
		return nil
	})
}

// PendingDecision answers the newest pending decision of one subject - the
// evaluation-report data source for the renderer confirm card (the local
// single-user engine shows the one-time token in the report; it never
// leaves the local boundary). Missing decisions answer has=false.
func (s *CollabGateService) PendingDecision(ctx context.Context, subjectID string) (m8core.GateDecision, bool, error) {
	if s == nil || s.uow == nil {
		return m8core.GateDecision{}, false, ErrServiceUnavailable
	}
	if len(subjectID) < 1 || len(subjectID) > 128 {
		return m8core.GateDecision{}, false, ErrPayloadInvalid
	}
	var out m8core.GateDecision
	var has bool
	err := s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		d, ok, gerr := tx.LatestPendingDecision(subjectID)
		if gerr != nil {
			return gerr
		}
		out, has = d, ok
		return nil
	})
	return out, has, err
}

// capabilityLocked derives the live capability inside one transaction:
// the latest confirmed decision decides, but any runtime binding drift
// flips it back to disabled (the decision is revoked and audited - the
// immediate M8-032 rollback path).
func (s *CollabGateService) capabilityLocked(tx CollabGateTx, subjectID string) (string, m8core.GateDecision, error) {
	d, has, err := tx.LatestConfirmedDecision(subjectID)
	if err != nil {
		return "", d, err
	}
	if !has || d.Action == m8core.DecisionDisable {
		return m8core.CapabilityDisabled, d, nil
	}
	if !s.binding.Matches(m8core.CapabilityBinding{PolicyVersion: d.PolicyVersion, CapabilityDigest: d.CapabilityDigest}) {
		_ = tx.SetDecisionState(d.DecisionID, m8core.DecisionRevoked, "")
		now := s.clock.Now().UTC().Format(time.RFC3339)
		_, _ = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "collabGate.binding.drift",
			ResourceType: "collab_gate_decision", ResourceID: d.DecisionID,
			Actor: d.SubjectID, CreatedAt: now,
		})
		return m8core.CapabilityDisabled, d, nil
	}
	return m8core.CapabilityEnabled, d, nil
}

// newDecisionToken mints one unpredictable 32-byte hex token (64 chars).
func newDecisionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
