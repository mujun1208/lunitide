package agentrunapp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
)

// ResumeWithBudget rejects the old over-limit envelope. A caller must provide
// a complete replacement budget that covers all committed usage.
func (s *Service) ResumeWithBudget(ctx context.Context, key, actor string, request any, runID string, expectedVersion int64, budget agentrun.Budget) (agentrun.AgentRun, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return agentrun.AgentRun{}, ErrIdempotencyKeyRequired
	}
	if err := budget.Validate(); err != nil {
		return agentrun.AgentRun{}, fmt.Errorf("%w: %v", agentrun.ErrInvalid, err)
	}
	digest, err := requestDigest(request)
	if err != nil {
		return agentrun.AgentRun{}, err
	}
	var result agentrun.AgentRun
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		rec, found, e := tx.Idempotency("agent.run.resume", key, now)
		if e != nil {
			return e
		}
		if found {
			return replay(rec, digest, &result)
		}
		r, e := tx.GetRun(runID)
		if e != nil {
			return e
		}
		if r.Status != agentrun.RunPausedBudget {
			return agentrun.ErrInvalidTransition
		}
		if !budget.StrictlyExpands(r.Budget) || !budget.Covers(r.Used) {
			return agentrun.ErrBudgetExceeded
		}
		r, e = tx.ReplaceBudget(runID, expectedVersion, budget, now)
		if e != nil {
			return e
		}
		r, e = tx.TransitionRun(runID, r.Version, agentrun.RunRunning, now)
		if e != nil {
			return e
		}
		if e = appendRunEvent(tx, runID, "AgentRunResumeCompleted", runEventPayload(r), now); e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"budgetReplaced": true, "version": r.Version})
		if e = s.putAudit(tx, "agent.run.resumed", runID, actor, digest, meta, now); e != nil {
			return e
		}
		body, _ := json.Marshal(r)
		if e = tx.PutIdempotency(providerapp.Record{Operation: "agent.run.resume", Key: key, Digest: digest, Response: body, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); e != nil {
			return e
		}
		result = r
		return nil
	})
	return result, err
}
