// M7 slice 6 application service (T-7.6.x): the read-only subagent runtime.
// Spawn validates the frozen whitelist/guards, Join re-verifies digests
// (TOCTOU) and answers summaries only, Tree pages per root, and root
// terminal states cascade-cancel every live subagent. All decisions append
// to the shared M7 audit ledger.
package m7app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrSubagentPurpose: purpose missing or over 2000 chars (M7-SAG-001).
	ErrSubagentPurpose = errors.New("m7app: subagent purpose invalid")
	// ErrSubagentCaps: readCaps outside the frozen whitelist (M7-SAG-002).
	ErrSubagentCaps = errors.New("m7app: subagent capability refused")
	// ErrSubagentQuota: concurrency/budget/deadline guard (M7-SAG-003).
	ErrSubagentQuota = errors.New("m7app: subagent quota exceeded")
	// ErrSubagentJoinStale: join target gone/terminal or digest drift
	// (M7-SAG-004 TOCTOU).
	ErrSubagentJoinStale = errors.New("m7app: subagent join refused")
	// ErrSubagentDeadline: deadline exceeded, run marked partial
	// (M7-SAG-005).
	ErrSubagentDeadline = errors.New("m7app: subagent deadline exceeded")
	// ErrSubagentTransition: illegal state machine edge.
	ErrSubagentTransition = errors.New("m7app: illegal subagent transition")
	// ErrSubagentNotFound: referenced subagent missing.
	ErrSubagentNotFound = errors.New("m7app: subagent not found")
)

// SubagentTx is the slice-6 single-writer transaction: subagent tables plus
// the shared audit ledger only.
type SubagentTx interface {
	PutSubagentRun(m7flow.SubagentRun) error
	GetSubagentRun(id string) (m7flow.SubagentRun, error)
	FindSubagentByIdempotency(rootRunID, key string) (m7flow.SubagentRun, error)
	CountActiveSubagents(rootRunID string) (int, error)
	UpdateSubagentStatus(id, from, to string, spentTokens int64, completedAt *time.Time) error
	CancelActiveByRoot(rootRunID string, now time.Time) (int, error)
	ListSubagentsByRoot(rootRunID string, afterID string, limit int) ([]m7flow.SubagentRun, error)
	PutSubagentObservation(m7flow.SubagentObservation) error
	ListSubagentObservations(subagentRunID string) ([]m7flow.SubagentObservation, error)
	AppendAuditEvent(e audit.Event) (audit.Event, error)
}

// SubagentUnitOfWork is the slice-6 single-writer boundary.
type SubagentUnitOfWork interface {
	TransactSubagent(ctx context.Context, fn func(SubagentTx) error) error
}

// SubagentService implements subagent.spawn / subagent.join /
// subagent.tree plus the internal complete/cancel/reconcile paths.
type SubagentService struct {
	uow           SubagentUnitOfWork
	clock         Clock
	policyVersion func() string
}

func NewSubagentService(uow SubagentUnitOfWork) *SubagentService {
	return &SubagentService{uow: uow, clock: systemClock{}, policyVersion: func() string { return "m7-policy-v1" }}
}

func (s *SubagentService) SetClock(c Clock) { s.clock = c }

// SetPolicyVersion substitutes the policy version probe (hot-reload tests).
func (s *SubagentService) SetPolicyVersion(fn func() string) { s.policyVersion = fn }

// SpawnInput is the subagent.spawn command.
type SpawnInput struct {
	RootRunID      string
	StageRunID     string
	Purpose        string
	ReadCaps       []string
	PersonaDigest  string
	BudgetTokens   int64
	DeadlineMS     int64
	IdempotencyKey string
	Actor          string
}

// Spawn validates the frozen guards and creates one running subagent.
// Whitelist violations are refused AND audited (scenario 36).
func (s *SubagentService) Spawn(ctx context.Context, in SpawnInput) (m7flow.SubagentRun, error) {
	if s == nil || s.uow == nil {
		return m7flow.SubagentRun{}, ErrServiceUnavailable
	}
	if len(in.Purpose) < 1 || len(in.Purpose) > m7flow.SubagentMaxPurpose {
		return m7flow.SubagentRun{}, fmt.Errorf("%w: purpose length %d", ErrSubagentPurpose, len(in.Purpose))
	}
	if len(in.IdempotencyKey) < 1 || len(in.IdempotencyKey) > 128 || len(in.RootRunID) < 1 || len(in.RootRunID) > 128 {
		return m7flow.SubagentRun{}, ErrSubagentPurpose
	}
	for _, cap := range in.ReadCaps {
		if !m7flow.SagCapAllowed(cap) {
			// fail-closed + audit (M7-SAG-002)
			s.auditRefuse(ctx, "subagent.spawn.refused", in.RootRunID, in.Actor, "cap "+cap)
			return m7flow.SubagentRun{}, fmt.Errorf("%w: %q", ErrSubagentCaps, cap)
		}
	}
	if in.BudgetTokens < 1 || in.BudgetTokens > m7flow.SubagentMaxBudgetTokens {
		return m7flow.SubagentRun{}, fmt.Errorf("%w: budgetTokens %d", ErrSubagentQuota, in.BudgetTokens)
	}
	if in.DeadlineMS < m7flow.SubagentMinDeadlineMS || in.DeadlineMS > m7flow.SubagentMaxDeadlineMS {
		return m7flow.SubagentRun{}, fmt.Errorf("%w: deadlineMs %d", ErrSubagentQuota, in.DeadlineMS)
	}
	var out m7flow.SubagentRun
	err := s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		if existing, err := tx.FindSubagentByIdempotency(in.RootRunID, in.IdempotencyKey); err == nil {
			out = existing // idempotent replay answers the original run
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		n, err := tx.CountActiveSubagents(in.RootRunID)
		if err != nil {
			return err
		}
		if n >= m7flow.SubagentMaxConcurrent {
			return fmt.Errorf("%w: %d active", ErrSubagentQuota, n)
		}
		now := s.clock.Now().UTC()
		run := m7flow.SubagentRun{
			ID:               ulid.Make().String(),
			RootRunID:        in.RootRunID,
			StageRunID:       in.StageRunID,
			Purpose:          in.Purpose,
			CapabilityDigest: m7flow.SubagentCapsDigest(in.ReadCaps),
			PolicyVersion:    s.policyVersion(),
			PersonaDigest:    in.PersonaDigest,
			Status:           m7flow.SagRunning,
			BudgetTokens:     in.BudgetTokens,
			SpentTokens:      0,
			DeadlineMS:       in.DeadlineMS,
			IdempotencyKey:   in.IdempotencyKey,
			CreatedAt:        now.Format(time.RFC3339),
		}
		if err := tx.PutSubagentRun(run); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "subagent.spawn",
			ResourceType: "subagent_run", ResourceID: run.ID, Actor: actorOr(in.Actor),
			AfterDigest: digestOf(run.CapabilityDigest + run.PolicyVersion),
			CreatedAt:   now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		out = run
		return nil
	})
	if err != nil {
		return m7flow.SubagentRun{}, err
	}
	return out, nil
}

// ObservationInput materializes one collected evidence summary.
type ObservationInput struct {
	EvidenceID string
	Summary    string
}

// Complete finishes a running subagent: observations are appended in seq
// order (Evidence + TraceEdge materialization happens upstream), the run
// lands in completed and spent tokens are recorded.
func (s *SubagentService) Complete(ctx context.Context, subagentRunID string, spentTokens int64, observations []ObservationInput) (m7flow.SubagentRun, error) {
	if s == nil || s.uow == nil {
		return m7flow.SubagentRun{}, ErrServiceUnavailable
	}
	var out m7flow.SubagentRun
	err := s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		run, err := tx.GetSubagentRun(subagentRunID)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		for i, ob := range observations {
			if len(ob.Summary) < 1 || len(ob.Summary) > m7flow.SubagentMaxPurpose {
				return fmt.Errorf("%w: observation %d summary", ErrSubagentPurpose, i)
			}
			o := m7flow.SubagentObservation{
				ID:            ulid.Make().String(),
				SubagentRunID: run.ID,
				Seq:           int64(i + 1),
				EvidenceID:    ob.EvidenceID,
				Summary:       ob.Summary,
				Digest:        digestOf(ob.Summary),
				CreatedAt:     now.Format(time.RFC3339),
			}
			if err := tx.PutSubagentObservation(o); err != nil {
				return err
			}
		}
		completed := now
		if err := tx.UpdateSubagentStatus(run.ID, m7flow.SagRunning, m7flow.SagCompleted, spentTokens, &completed); err != nil {
			return err
		}
		run.Status = m7flow.SagCompleted
		run.SpentTokens = spentTokens
		cs := now.Format(time.RFC3339)
		run.CompletedAt = &cs
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "subagent.complete",
			ResourceType: "subagent_run", ResourceID: run.ID, Actor: "system",
			AfterDigest: digestOf(run.Status), CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		out = run
		return nil
	})
	if err != nil {
		return m7flow.SubagentRun{}, err
	}
	return out, nil
}

// JoinResult is the summary-only projection the main Run may receive -
// raw collection content never enters the main context (scenario 40).
type JoinResult struct {
	SubagentRunID string
	State         string
	Summary       string
	Digests       []string
	Truncated     bool
	SpentTokens   int64
}

// JoinInput is the subagent.join command.
type JoinInput struct {
	SubagentRunID            string
	ExpectedCapabilityDigest string
	ExpectedPolicyVersion    string
	ExpectedPersonaDigest    string
	MaxSummaryBytes          int
}

// Join re-verifies capability/policy/persona digests before handing back
// the observation summaries (TOCTOU, M7-SAG-004). The summary is capped at
// maxSummaryBytes; overflow marks truncated.
func (s *SubagentService) Join(ctx context.Context, in JoinInput) (JoinResult, error) {
	if s == nil || s.uow == nil {
		return JoinResult{}, ErrServiceUnavailable
	}
	if in.MaxSummaryBytes < 1 {
		in.MaxSummaryBytes = m7flow.SubagentMaxSummary
	}
	var out JoinResult
	err := s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		run, err := tx.GetSubagentRun(in.SubagentRunID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrSubagentJoinStale, in.SubagentRunID)
		}
		if err != nil {
			return err
		}
		if m7flow.SagTerminal(run.Status) {
			if run.Status != m7flow.SagCompleted {
				return fmt.Errorf("%w: terminal %s", ErrSubagentJoinStale, run.Status)
			}
		} else {
			return fmt.Errorf("%w: still %s", ErrSubagentJoinStale, run.Status)
		}
		// TOCTOU re-verification (scenario 39).
		if in.ExpectedCapabilityDigest != "" && in.ExpectedCapabilityDigest != run.CapabilityDigest {
			return fmt.Errorf("%w: capability digest drift", ErrSubagentJoinStale)
		}
		if in.ExpectedPolicyVersion != "" && in.ExpectedPolicyVersion != run.PolicyVersion {
			return fmt.Errorf("%w: policy version drift", ErrSubagentJoinStale)
		}
		if in.ExpectedPersonaDigest != "" && run.PersonaDigest != "" && in.ExpectedPersonaDigest != run.PersonaDigest {
			return fmt.Errorf("%w: persona digest drift", ErrSubagentJoinStale)
		}
		obs, err := tx.ListSubagentObservations(run.ID)
		if err != nil {
			return err
		}
		summary := ""
		for _, o := range obs {
			out.Digests = append(out.Digests, o.Digest)
			if len(summary)+len(o.Summary) > in.MaxSummaryBytes {
				out.Truncated = true
				continue
			}
			if summary != "" {
				summary += "\n"
			}
			summary += o.Summary
		}
		out.SubagentRunID = run.ID
		out.State = run.Status
		out.Summary = summary
		out.SpentTokens = run.SpentTokens
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "subagent.join",
			ResourceType: "subagent_run", ResourceID: run.ID, Actor: "system",
			CorrelationID: run.RootRunID, AfterDigest: digestOf(summary),
			CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return JoinResult{}, err
	}
	return out, nil
}

// Tree answers the subagents of one root, cursor-paged by id.
func (s *SubagentService) Tree(ctx context.Context, rootRunID, cursor string, limit int) ([]m7flow.SubagentRun, string, error) {
	if s == nil || s.uow == nil {
		return nil, "", ErrServiceUnavailable
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var runs []m7flow.SubagentRun
	var next string
	err := s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		list, err := tx.ListSubagentsByRoot(rootRunID, cursor, limit+1)
		if err != nil {
			return err
		}
		if len(list) > limit {
			next = list[limit-1].ID
			list = list[:limit]
		}
		runs = list
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return runs, next, nil
}

// CancelRoot cascades a root terminal/cancel onto every live subagent
// (scenario 38): queued/running -> cancelled, zero orphans.
func (s *SubagentService) CancelRoot(ctx context.Context, rootRunID, actor string) (int, error) {
	if s == nil || s.uow == nil {
		return 0, ErrServiceUnavailable
	}
	cancelled := 0
	err := s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		n, err := tx.CancelActiveByRoot(rootRunID, s.clock.Now().UTC())
		if err != nil {
			return err
		}
		cancelled = n
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "subagent.cascade_cancel",
			ResourceType: "root_run", ResourceID: rootRunID, Actor: actorOr(actor),
			AfterDigest: digestOf(fmt.Sprintf("cancelled=%d", n)),
			CreatedAt:   s.clock.Now().UTC().Format(time.RFC3339),
		}); err != nil {
			return err
		}
		return nil
	})
	return cancelled, err
}

// auditRefuse records a fail-closed refusal on the shared ledger.
func (s *SubagentService) auditRefuse(ctx context.Context, action, rootRunID, actor, detail string) {
	if s == nil || s.uow == nil {
		return
	}
	_ = s.uow.TransactSubagent(ctx, func(tx SubagentTx) error {
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: action,
			ResourceType: "root_run", ResourceID: rootRunID, Actor: actorOr(actor),
			CorrelationID: detail, CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
		})
		return err
	})
}

func actorOr(actor string) string {
	if actor == "" {
		return "anonymous"
	}
	return actor
}

func digestOf(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}