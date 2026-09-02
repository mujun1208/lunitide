// memory_autoaccept.go is the M1 governed low-risk auto-accept path.
//
// FREEZE RELATIONSHIP: the memory core freeze is "the confirmation token
// is the only path to memory_facts; confidence/frequency/compaction never
// promote". M1 does NOT relax that generic rule - AutoPromote stays a
// hard, audited refusal (FR-11). M1 adds a SEPARATE channel that only
// fires when (a) the operator has armed the default-off memory-auto-accept
// switch AND (b) the candidate's payload classifies as low risk. High-risk
// payloads are always left pending for the explicit human journey. The
// promotion still runs through the exact same fact/leaf/transition writes
// as ConfirmCandidate, inside one transaction, and is audited under a
// distinct action so the auto-accepted tail is always separable in review.
package m8app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// AutoAcceptResult reports the outcome of an auto-accept attempt.
type AutoAcceptResult struct {
	CandidateID string
	Risk        string // low | high
	Accepted    bool
	Fact        *FactRef
}

// AutoAcceptCandidate promotes a pending candidate to a fact without a
// human token IFF its payload is low risk. High-risk candidates are left
// pending and answered with ErrExplicitConfirmationRequired (M8-003), and
// the hold is audited. Callers MUST gate this on the default-off
// governance switch; the service does not read process flags itself.
func (s *MemoryService) AutoAcceptCandidate(ctx context.Context, candidateID, source string) (AutoAcceptResult, error) {
	if s == nil || s.uow == nil {
		return AutoAcceptResult{}, ErrServiceUnavailable
	}
	if len(candidateID) != 26 {
		return AutoAcceptResult{}, fmt.Errorf("%w: candidateId malformed", ErrConfirmTokenInvalid)
	}
	var out AutoAcceptResult
	err := s.uow.TransactMemory(ctx, func(tx MemoryTx) error {
		cand, err := tx.GetCandidate(candidateID)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m8core.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrCandidateNotFound, candidateID)
		}
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		if m8core.CandTerminal(cand.State) {
			if cand.State == m8core.CandExpired {
				return fmt.Errorf("%w: %s", ErrCandidateExpired, candidateID)
			}
			// Already confirmed/rejected: nothing to auto-accept.
			return fmt.Errorf("%w: terminal %s", ErrConfirmTokenInvalid, cand.State)
		}
		expiresAt, perr := time.Parse(time.RFC3339, cand.ExpiresAt)
		if perr != nil {
			return fmt.Errorf("%w: expiry %q", ErrConfirmTokenInvalid, cand.ExpiresAt)
		}
		if now.After(expiresAt) {
			if err := tx.TransitionCandidate(cand.CandidateID, m8core.CandPending, m8core.CandExpired, ""); err != nil {
				return err
			}
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.candidate.expire",
				ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
				Actor: actorOr(source), BeforeDigest: cand.PayloadDigest,
				CreatedAt: now.Format(time.RFC3339),
			})
			if err != nil {
				return err
			}
			return fmt.Errorf("%w: %s", ErrCandidateExpired, cand.CandidateID)
		}
		var doc m8core.PayloadDoc
		if err := json.Unmarshal([]byte(cand.Payload), &doc); err != nil {
			return fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
		if err := doc.Validate(); err != nil {
			if strings.Contains(err.Error(), "source leaf") {
				return fmt.Errorf("%w: %v", ErrSourceLeafRequired, err)
			}
			return fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
		// M1 gate: high-risk stays pending for the explicit human journey.
		if m8core.ClassifyMemoryRisk(doc) != m8core.RiskLow {
			out = AutoAcceptResult{CandidateID: cand.CandidateID, Risk: m8core.RiskHigh, Accepted: false}
			_, aerr := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "memory.candidate.auto_hold",
				ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
				Actor: actorOr(source), BeforeDigest: cand.PayloadDigest,
				CreatedAt: now.Format(time.RFC3339),
			})
			if aerr != nil {
				return aerr
			}
			return fmt.Errorf("%w: high-risk candidate held", ErrExplicitConfirmationRequired)
		}
		finalDigest, err := doc.PayloadDigest()
		if err != nil {
			return err
		}
		// Re-verify every leaf exactly as ConfirmCandidate does (M8-008).
		for i, l := range doc.Leaves {
			if err := s.verifyEv(ctx, l.EvidenceRef, l.Digest); err != nil {
				return fmt.Errorf("%w: leaf %d: %v", ErrSourceEvidenceUnavailable, i, err)
			}
		}
		factID := ulid.Make().String()
		if err := tx.PutFact(m8core.MemoryFact{
			FactID: factID, ScopeID: doc.ScopeID, Version: 1,
			Sensitivity: doc.Sensitivity, State: m8core.FactActive,
			CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		leaves := make([]m8core.SourceLeaf, len(doc.Leaves))
		for i, l := range doc.Leaves {
			leaves[i] = m8core.SourceLeaf{
				ID: ulid.Make().String(), FactID: factID, FactVersion: 1,
				JSONPointer: l.JSONPointer, EvidenceRef: l.EvidenceRef,
				Digest: l.Digest, CreatedAt: now.Format(time.RFC3339),
			}
		}
		if err := tx.PutSourceLeaves(leaves); err != nil {
			return err
		}
		if err := tx.TransitionCandidate(cand.CandidateID, m8core.CandPending, m8core.CandConfirmed, now.Format(time.RFC3339)); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "memory.candidate.auto_confirm",
			ResourceType: "memory_candidate", ResourceID: cand.CandidateID,
			Actor: actorOr(source), BeforeDigest: cand.PayloadDigest,
			AfterDigest: finalDigest, CreatedAt: now.Format(time.RFC3339),
		})
		if err != nil {
			return err
		}
		out = AutoAcceptResult{
			CandidateID: cand.CandidateID, Risk: m8core.RiskLow, Accepted: true,
			Fact: &FactRef{FactID: factID, Version: 1},
		}
		return nil
	})
	if err != nil {
		return AutoAcceptResult{}, err
	}
	return out, nil
}
