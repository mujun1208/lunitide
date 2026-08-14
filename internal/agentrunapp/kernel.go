package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

// AdvanceKind is deliberately single-agent: no child/fanout variants exist.
type AdvanceKind string

const (
	AdvanceToolCall    AdvanceKind = "tool_call"
	AdvanceObservation AdvanceKind = "observation"
	AdvanceComplete    AdvanceKind = "complete"
	AdvanceFail        AdvanceKind = "fail"
)

type AdvanceInput struct {
	Kind            AdvanceKind
	ToolName        string
	Args            json.RawMessage
	ObservationKind string
	Observation     []byte
	Usage           agentrun.Usage
}

// Advance transactionally advances the minimal kernel and its usage ledger.
// Tool effects are only proposed here; execution must happen after reservation.
func (s *Service) Advance(ctx context.Context, runID string, in AdvanceInput) (agentrun.AgentRun, error) {
	if in.Kind != AdvanceToolCall && in.Kind != AdvanceObservation && in.Kind != AdvanceComplete && in.Kind != AdvanceFail {
		return agentrun.AgentRun{}, agentrun.ErrInvalid
	}
	var result agentrun.AgentRun
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		r, err := tx.GetRun(runID)
		if err != nil {
			return err
		}
		if r.Status.Terminal() {
			return agentrun.ErrTerminal
		}
		if r.Status != agentrun.RunRunning {
			return agentrun.ErrInvalidTransition
		}
		turns, err := tx.ListTurns(runID)
		if err != nil || len(turns) == 0 {
			if err != nil {
				return err
			}
			return agentrun.ErrNotFound
		}
		turn := turns[len(turns)-1]
		steps, err := tx.ListSteps(turn.ID)
		if err != nil || len(steps) == 0 {
			if err != nil {
				return err
			}
			return agentrun.ErrNotFound
		}
		step := steps[len(steps)-1]
		if in.Usage != (agentrun.Usage{}) {
			rid := ulid.Make().String()
			if _, err = tx.ReserveUsage(runID, rid, in.Usage, now); err != nil {
				if errors.Is(err, agentrun.ErrBudgetExceeded) {
					target := agentrun.RunPausedBudget
					if r.Budget.HardCeiling {
						target = agentrun.RunFailed
					}
					result, err = tx.TransitionRun(runID, r.Version, target, now)
					if err != nil {
						return err
					}
					return appendRunEvent(tx, runID, "AgentRunBudgetExceeded", map[string]any{
						"schemaVersion": 1, "runId": runID, "status": string(result.Status),
						"version": result.Version, "attemptedUsage": in.Usage,
					}, now)
				}
				return err
			}
			r, err = tx.CommitUsage(runID, rid, in.Usage, now)
			if err != nil {
				return err
			}
		}
		switch in.Kind {
		case AdvanceToolCall:
			if step.Status != agentrun.StepRunning {
				return agentrun.ErrInvalidTransition
			}
			sum := sha256.Sum256(in.Args)
			call := agentrun.ToolCall{ID: ulid.Make().String(), StepID: step.ID, ToolName: in.ToolName, ArgsDigest: hex.EncodeToString(sum[:]), Status: agentrun.CallProposed, CreatedAt: now, UpdatedAt: now}
			if err = tx.PutToolCall(call); err != nil {
				return err
			}
			if err = appendRunEvent(tx, runID, "ToolCallProposed", map[string]any{"schemaVersion": 1, "runId": runID, "toolName": in.ToolName, "argsDigest": call.ArgsDigest}, now); err != nil {
				return err
			}
		case AdvanceObservation:
			sum := sha256.Sum256(in.Observation)
			o := agentrun.Observation{ID: ulid.Make().String(), StepID: step.ID, Kind: in.ObservationKind, ContentDigest: hex.EncodeToString(sum[:]), CapturedAt: now, CreatedAt: now}
			if err = tx.AppendObservation(o); err != nil {
				return err
			}
			if err = appendRunEvent(tx, runID, "ObservationCaptured", map[string]any{"schemaVersion": 1, "runId": runID, "kind": in.ObservationKind, "contentDigest": o.ContentDigest}, now); err != nil {
				return err
			}
			step.Status = agentrun.StepCompleted
			step.UpdatedAt = now
			if err = tx.PutStep(step); err != nil {
				return err
			}
			next := agentrun.AgentStep{ID: ulid.Make().String(), TurnID: turn.ID, StepNo: step.StepNo + 1, Kind: agentrun.StepModel, Status: agentrun.StepRunning, CreatedAt: now, UpdatedAt: now}
			if err = tx.PutStep(next); err != nil {
				return err
			}
		case AdvanceComplete, AdvanceFail:
			step.Status = agentrun.StepCompleted
			if in.Kind == AdvanceFail {
				step.Status = agentrun.StepFailed
			}
			step.UpdatedAt = now
			if err = tx.PutStep(step); err != nil {
				return err
			}
			turn.Status = agentrun.TurnCompleted
			if in.Kind == AdvanceFail {
				turn.Status = agentrun.TurnFailed
			}
			turn.Version++
			turn.UpdatedAt = now
			if err = tx.PutTurn(turn); err != nil {
				return err
			}
			target := agentrun.RunCompleted
			if in.Kind == AdvanceFail {
				target = agentrun.RunFailed
			}
			r, err = tx.TransitionRun(runID, r.Version, target, now)
			if err != nil {
				return err
			}
			if err = appendRunEvent(tx, runID, "AgentRunTerminal", runEventPayload(r), now); err != nil {
				return err
			}
		}
		result = r
		return nil
	})
	return result, err
}

type RecoveryResult struct{ Runs, Steps, ToolCalls, Effects int }

// RunRecoveryScanner converges all process-owned in-flight state before ready.
func (s *Service) RunRecoveryScanner(ctx context.Context) (RecoveryResult, error) {
	var out RecoveryResult
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		prepared, err := tx.ListPreparedEffects()
		if err != nil {
			return err
		}
		// Command and changeset effects have domain-specific recovery. Preserve
		// their entire run while prepared so the generic scanner cannot erase
		// the evidence needed by those reconcilers (or by an idempotent retry).
		protectedRuns := map[string]bool{}
		for _, ef := range prepared {
			if strings.HasPrefix(ef.EffectKey, "command.start/") || strings.HasPrefix(ef.EffectKey, "changeset.apply/") || strings.HasPrefix(ef.EffectKey, "changeset.revert/") {
				protectedRuns[ef.RunID] = true
			}
		}
		calls, err := tx.ListActiveToolCalls()
		if err != nil {
			return err
		}
		unknownRuns := map[string]bool{}
		for _, c := range calls {
			runID, e := tx.RunIDForStep(c.StepID)
			if e != nil {
				return e
			}
			if protectedRuns[runID] {
				continue
			}
			if c.Status == agentrun.CallRunning {
				c.Status = agentrun.CallOutcomeUnknown
				unknownRuns[runID] = true
			} else {
				c.Status = agentrun.CallCancelled
			}
			c.UpdatedAt = now
			if err = tx.PutToolCall(c); err != nil {
				return err
			}
			out.ToolCalls++
		}
		runs, err := tx.ListActiveRuns()
		if err != nil {
			return err
		}
		for _, r := range runs {
			if protectedRuns[r.ID] {
				continue
			}
			to := agentrun.RunInterrupted
			if unknownRuns[r.ID] {
				to = agentrun.RunOutcomeUnknown
			}
			if _, err = tx.RecoverRun(r.ID, to, now); err != nil && !errors.Is(err, agentrun.ErrTerminal) {
				return err
			}
			out.Runs++
		}
		steps, err := tx.ListRunningSteps()
		if err != nil {
			return err
		}
		for _, st := range steps {
			runID, e := tx.RunIDForStep(st.ID)
			if e != nil {
				return e
			}
			if protectedRuns[runID] {
				continue
			}
			st.Status = agentrun.StepFailed
			st.UpdatedAt = now
			if err = tx.PutStep(st); err != nil {
				return err
			}
			out.Steps++
		}
		for _, ef := range prepared {
			if protectedRuns[ef.RunID] {
				continue
			}
			ef, err = ef.Resolve(agentrun.EffectOutcomeUnknown, "", now)
			if err != nil {
				return err
			}
			if err = tx.PutEffect(ef); err != nil {
				return err
			}
			out.Effects++
		}
		return nil
	})
	return out, err
}
