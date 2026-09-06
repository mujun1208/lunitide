package agentrunapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/oklog/ulid/v2"
)

func planExecutionTx(tx Tx) (agentrun.PlanExecutionTx, error) {
	p, ok := tx.(agentrun.PlanExecutionTx)
	if !ok {
		return nil, errors.New("plan execution storage unavailable")
	}
	return p, nil
}

func (s *Service) GetPlanExecution(ctx context.Context, id string) (out agentrun.PlanExecution, err error) {
	if err = s.available(); err != nil {
		return
	}
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, e := planExecutionTx(tx)
		if e != nil {
			return e
		}
		out, e = p.GetPlanExecution(id)
		return e
	})
	return
}

// StartPlanExecution claims one task once, independent of expiring HTTP
// idempotency keys. A repeated start returns its original durable binding.
func (s *Service) StartPlanExecution(ctx context.Context, id, providerID, modelID string, budget agentrun.Budget) (out agentrun.PlanExecution, created bool, err error) {
	return s.claimPlanExecution(ctx, id, providerID, modelID, budget, 0, false)
}

func (s *Service) RetryPlanExecution(ctx context.Context, id, providerID, modelID string, budget agentrun.Budget, expectedVersion int64, acknowledgeUncertain bool) (agentrun.PlanExecution, bool, error) {
	if expectedVersion < 1 {
		return agentrun.PlanExecution{}, false, agentrun.ErrInvalid
	}
	return s.claimPlanExecution(ctx, id, providerID, modelID, budget, expectedVersion, acknowledgeUncertain)
}

func (s *Service) claimPlanExecution(ctx context.Context, id, providerID, modelID string, budget agentrun.Budget, expectedVersion int64, acknowledgeUncertain bool) (out agentrun.PlanExecution, created bool, err error) {
	if err = s.available(); err != nil {
		return
	}
	if err = budget.Validate(); err != nil {
		return
	}
	if providerID == "" || modelID == "" {
		return out, false, agentrun.ErrInvalid
	}
	// Review preparation commits separately from the execution claim: a blocked
	// start must leave a review that can actually be inspected and approved.
	var approved bool
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, e := planExecutionTx(tx)
		if e != nil {
			return e
		}
		if existing, e := p.GetPlanExecution(id); e == nil && expectedVersion == 0 {
			out = existing
			approved = true
			return nil
		} else if e != nil && !errors.Is(e, agentrun.ErrNotFound) {
			return e
		}
		approved, e = p.PreparePlanExecutionReview(id, s.clock.Now().UTC())
		return e
	})
	if err != nil {
		return out, false, err
	}
	if !approved {
		return out, false, planningapp.ErrReviewRequired
	}
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, e := planExecutionTx(tx)
		if e != nil {
			return e
		}
		out, e = p.GetPlanExecution(id)
		if e == nil && expectedVersion == 0 {
			return nil
		}
		previous := out
		if expectedVersion > 0 {
			if e != nil {
				return e
			}
			if out.Version != expectedVersion {
				return agentrun.ErrVersionConflict
			}
			if !out.Terminal() || out.Status == "succeeded" {
				return agentrun.ErrInvalidTransition
			}
			if out.Status == "outcome_unknown" && !acknowledgeUncertain {
				return fmt.Errorf("%w: acknowledge uncertain operations before retry", agentrun.ErrInvalid)
			}
			if e = p.ReopenPlanExecution(out); e != nil {
				return e
			}
		} else if !errors.Is(e, agentrun.ErrNotFound) {
			return e
		}
		spec, e := p.LoadPlanExecutionSpec(id)
		if e != nil {
			return e
		}
		now := s.clock.Now().UTC()
		if e = p.CheckPlanExecutionReady(id, now); e != nil {
			return e
		}
		spec.ProviderID, spec.ModelID, spec.Budget = providerID, modelID, budget
		if expectedVersion > 0 {
			spec.PreviousRunID = previous.RunID
		}
		sessionID := ulid.Make().String()
		if e = p.CreatePlanExecutionSession(sessionID, spec.ProjectID, "计划执行 · "+spec.Title, now); e != nil {
			return e
		}
		run := agentrun.AgentRun{ID: ulid.Make().String(), SessionID: sessionID, Status: agentrun.RunQueued, Budget: budget, Version: 1, CreatedAt: now, UpdatedAt: now}
		if e = tx.PutRun(run); e != nil {
			return e
		}
		run, e = tx.TransitionRun(run.ID, 1, agentrun.RunRunning, now)
		if e != nil {
			return e
		}
		turn := agentrun.AgentTurn{ID: ulid.Make().String(), RunID: run.ID, TurnNo: 1, Status: agentrun.TurnRunning, Version: 1, CreatedAt: now, UpdatedAt: now}
		if e = tx.PutTurn(turn); e != nil {
			return e
		}
		if e = tx.PutStep(agentrun.AgentStep{ID: ulid.Make().String(), TurnID: turn.ID, StepNo: 1, Kind: agentrun.StepModel, Status: agentrun.StepRunning, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		out = agentrun.PlanExecution{PlanRunID: id, RunID: run.ID, SessionID: sessionID, Spec: spec, Status: "running", Version: expectedVersion + 1, CreatedAt: now, UpdatedAt: now}
		if e = p.PutPlanExecution(out, expectedVersion); e != nil {
			return e
		}
		body, e := json.Marshal(spec)
		if e != nil {
			return e
		}
		digest, e := requestDigest(spec)
		if e != nil {
			return e
		}
		if e = tx.PutRunPlan(agentrun.RunPlan{ID: ulid.Make().String(), RunID: run.ID, PlanDigest: digest, Content: body, Version: 1, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		if e = appendRunEvent(tx, run.ID, "PlanExecutionStarted", out, now); e != nil {
			return e
		}
		if e = s.putAudit(tx, "agent.run.started", run.ID, "plan-executor", digest, body, now); e != nil {
			return e
		}
		created = true
		return nil
	})
	if err != nil {
		created = false
	}
	return
}

func runningPlan(tx Tx, id string, now time.Time) (agentrun.PlanExecution, agentrun.AgentRun, agentrun.AgentStep, error) {
	p, err := planExecutionTx(tx)
	if err != nil {
		return agentrun.PlanExecution{}, agentrun.AgentRun{}, agentrun.AgentStep{}, err
	}
	ex, err := p.GetPlanExecution(id)
	if err != nil {
		return ex, agentrun.AgentRun{}, agentrun.AgentStep{}, err
	}
	if ex.Status != "running" {
		return ex, agentrun.AgentRun{}, agentrun.AgentStep{}, agentrun.ErrInvalidTransition
	}
	if err = p.CheckPlanExecutionReady(id, now); err != nil {
		return ex, agentrun.AgentRun{}, agentrun.AgentStep{}, err
	}
	run, err := tx.GetRun(ex.RunID)
	if err != nil {
		return ex, run, agentrun.AgentStep{}, err
	}
	if run.Status != agentrun.RunRunning {
		return ex, run, agentrun.AgentStep{}, agentrun.ErrInvalidTransition
	}
	turns, err := tx.ListTurns(run.ID)
	if err != nil {
		return ex, run, agentrun.AgentStep{}, err
	}
	if len(turns) == 0 {
		return ex, run, agentrun.AgentStep{}, agentrun.ErrNotFound
	}
	steps, err := tx.ListSteps(turns[len(turns)-1].ID)
	if err != nil {
		return ex, run, agentrun.AgentStep{}, err
	}
	if len(steps) == 0 {
		return ex, run, agentrun.AgentStep{}, agentrun.ErrNotFound
	}
	return ex, run, steps[len(steps)-1], nil
}

func chargePlanUsage(tx Tx, runID string, usage agentrun.Usage, now time.Time) error {
	if err := usage.Validate(); err != nil {
		return err
	}
	id := ulid.Make().String()
	if _, err := tx.ReserveUsage(runID, id, usage, now); err != nil {
		return err
	}
	_, err := tx.CommitUsage(runID, id, usage, now)
	return err
}

func (s *Service) ChargePlanExecution(ctx context.Context, id, expectedRunID string, usage agentrun.Usage) error {
	return s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		_, run, _, err := runningPlan(tx, id, now)
		if err != nil {
			return err
		}
		if run.ID != expectedRunID {
			return agentrun.ErrVersionConflict
		}
		return chargePlanUsage(tx, run.ID, usage, now)
	})
}

// PreparePlanTool commits the tool intent before any call crosses into a tool
// host. Its ID, not model-provided prose, identifies the eventual receipt.
func (s *Service) PreparePlanTool(ctx context.Context, id, expectedRunID, name string, args json.RawMessage) (call agentrun.ToolCall, err error) {
	if name == "" || len(args) > 1<<20 || !json.Valid(args) {
		return call, agentrun.ErrInvalid
	}
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		_, run, step, e := runningPlan(tx, id, now)
		if e != nil {
			return e
		}
		if run.ID != expectedRunID {
			return agentrun.ErrVersionConflict
		}
		if e = chargePlanUsage(tx, run.ID, agentrun.Usage{ToolCalls: 1}, now); e != nil {
			return e
		}
		digest, e := requestDigest(struct {
			Name string
			Args json.RawMessage
		}{name, args})
		if e != nil {
			return e
		}
		call = agentrun.ToolCall{ID: ulid.Make().String(), StepID: step.ID, ToolName: name, ArgsDigest: digest, Status: agentrun.CallRunning, CreatedAt: now, UpdatedAt: now}
		if e = tx.PutToolCall(call); e != nil {
			return e
		}
		if e = tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: run.ID, EffectKey: "plan.tool/" + call.ID, RequestDigest: digest, Status: agentrun.EffectPrepared, CreatedAt: now, UpdatedAt: now}); e != nil {
			return e
		}
		return appendRunEvent(tx, run.ID, "PlanToolPrepared", call, now)
	})
	return
}

func (s *Service) ReceiptPlanTool(ctx context.Context, id, callID, output string, toolErr error, uncertain bool) error {
	return s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, err := planExecutionTx(tx)
		if err != nil {
			return err
		}
		ex, err := p.GetPlanExecution(id)
		if err != nil {
			return err
		}
		call, err := tx.GetToolCall(callID)
		if err != nil {
			return err
		}
		runID, err := tx.RunIDForStep(call.StepID)
		if err != nil {
			return err
		}
		if runID != ex.RunID {
			return agentrun.ErrInvalid
		}
		if call.Status.Terminal() {
			return agentrun.ErrTerminal
		}
		now := s.clock.Now().UTC()
		status, effectStatus := agentrun.CallSucceeded, agentrun.EffectCommitted
		if toolErr != nil {
			status, effectStatus = agentrun.CallFailed, agentrun.EffectFailed
		}
		if uncertain {
			status, effectStatus = agentrun.CallOutcomeUnknown, agentrun.EffectOutcomeUnknown
		}
		call, err = call.Transition(status, now)
		if err != nil {
			return err
		}
		if err = tx.PutToolCall(call); err != nil {
			return err
		}
		effect, err := tx.GetEffectByKey("plan.tool/" + call.ID)
		if err != nil {
			return err
		}
		receiptID := ulid.Make().String()
		effect, err = effect.Resolve(effectStatus, receiptID, now)
		if err != nil {
			return err
		}
		if err = tx.PutEffect(effect); err != nil {
			return err
		}
		digest, err := requestDigest(output)
		if err != nil {
			return err
		}
		if err = tx.AppendObservation(agentrun.Observation{ID: receiptID, StepID: call.StepID, Kind: "plan.tool.receipt", ContentDigest: digest, CapturedAt: now, CreatedAt: now}); err != nil {
			return err
		}
		return appendRunEvent(tx, ex.RunID, "PlanToolReceipt", map[string]any{"callId": call.ID, "tool": call.ToolName, "status": status, "outputDigest": digest}, now)
	})
}

func (s *Service) CancelPlanExecution(ctx context.Context, id string) (out agentrun.PlanExecution, err error) {
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, e := planExecutionTx(tx)
		if e != nil {
			return e
		}
		out, e = p.GetPlanExecution(id)
		if e != nil {
			return e
		}
		if out.Terminal() || out.Status == "cancel_requested" {
			return nil
		}
		version := out.Version
		out.Status, out.UpdatedAt, out.Version = "cancel_requested", s.clock.Now().UTC(), version+1
		if e = p.PutPlanExecution(out, version); e != nil {
			return e
		}
		return appendRunEvent(tx, out.RunID, "PlanCancellationRequested", map[string]any{"planRunId": id}, out.UpdatedAt)
	})
	return
}

func (s *Service) FinishPlanExecution(ctx context.Context, id, expectedRunID, status, summary, failure string, artifacts []agentrun.PlanArtifact, receipts []string) (out agentrun.PlanExecution, err error) {
	if status != "succeeded" && status != "failed" && status != "cancelled" && status != "interrupted" && status != "outcome_unknown" {
		return out, agentrun.ErrInvalid
	}
	if len(summary) > 16384 || len(failure) > 4096 || len(artifacts) > 32 {
		return out, agentrun.ErrInvalid
	}
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, e := planExecutionTx(tx)
		if e != nil {
			return e
		}
		out, e = p.GetPlanExecution(id)
		if e != nil {
			return e
		}
		if out.RunID != expectedRunID {
			return agentrun.ErrVersionConflict
		}
		if out.Terminal() {
			return nil
		}
		now := s.clock.Now().UTC()
		effects, e := tx.ListEffects(out.RunID)
		if e != nil {
			return e
		}
		for _, ef := range effects {
			if ef.Status == agentrun.EffectPrepared || ef.Status == agentrun.EffectOutcomeUnknown {
				status, failure = "outcome_unknown", "执行回执不完整；请核对已发生的操作后再处理"
				if ef.Status == agentrun.EffectPrepared {
					ef, e = ef.Resolve(agentrun.EffectOutcomeUnknown, "", now)
					if e != nil {
						return e
					}
					if e = tx.PutEffect(ef); e != nil {
						return e
					}
				}
			}
		}
		if status == "succeeded" && out.Status == "cancel_requested" {
			status = "cancelled"
		}
		if status == "succeeded" {
			if e = p.CheckPlanExecutionReady(id, now); e != nil {
				return e
			}
			if strings.TrimSpace(summary) == "" || len(artifacts) == 0 || len(receipts) == 0 {
				return fmt.Errorf("%w: verified artifacts and receipts required", agentrun.ErrInvalid)
			}
			hasCommand := false
			for _, receipt := range receipts {
				call, e := tx.GetToolCall(receipt)
				if e != nil {
					return e
				}
				runID, e := tx.RunIDForStep(call.StepID)
				if e != nil {
					return e
				}
				if runID != out.RunID || call.Status != agentrun.CallSucceeded {
					return agentrun.ErrInvalid
				}
				hasCommand = hasCommand || call.ToolName == "command.run"
			}
			if (out.Spec.Role == "tester" || out.Spec.Role == "test") && !hasCommand {
				return fmt.Errorf("%w: tester requires a successful command receipt", agentrun.ErrInvalid)
			}
			for _, artifact := range artifacts {
				if artifact.Bytes < 1 || len(artifact.Path) < 1 || len(artifact.Path) > 512 {
					return agentrun.ErrInvalid
				}
				if e = tx.AppendEvidence(agentrun.Evidence{ID: ulid.Make().String(), RunID: out.RunID, Kind: "plan.artifact.verified", SourceURI: "session://" + out.SessionID + "/" + artifact.Path, ContentDigest: artifact.Digest, CapturedAt: now, CreatedAt: now}); e != nil {
					return e
				}
			}
		}
		target := map[string]agentrun.RunStatus{"succeeded": agentrun.RunCompleted, "failed": agentrun.RunFailed, "cancelled": agentrun.RunCancelled, "interrupted": agentrun.RunInterrupted, "outcome_unknown": agentrun.RunOutcomeUnknown}[status]
		run, e := tx.GetRun(out.RunID)
		if e != nil {
			return e
		}
		if run.Status.Terminal() {
			status = map[agentrun.RunStatus]string{agentrun.RunCompleted: "succeeded", agentrun.RunFailed: "failed", agentrun.RunCancelled: "cancelled", agentrun.RunInterrupted: "interrupted", agentrun.RunOutcomeUnknown: "outcome_unknown"}[run.Status]
			if status == "succeeded" && len(artifacts) == 0 {
				return fmt.Errorf("%w: completed runtime lacks plan evidence", agentrun.ErrInvalid)
			}
		}
		if status != "succeeded" {
			summary, artifacts = "", nil
		}
		if e = closePlanWork(tx, run.ID, status == "succeeded", now); e != nil {
			return e
		}
		if !run.Status.Terminal() {
			if _, e = tx.TransitionRun(run.ID, run.Version, target, now); e != nil {
				return e
			}
		}
		version := out.Version
		out.Status, out.Summary, out.Failure, out.Artifacts, out.Version, out.UpdatedAt = status, summary, failure, artifacts, version+1, now
		if e = p.PutPlanExecution(out, version); e != nil {
			return e
		}
		if e = appendRunEvent(tx, out.RunID, "PlanExecutionTerminal", out, now); e != nil {
			return e
		}
		digest, e := requestDigest(out)
		if e != nil {
			return e
		}
		body, e := json.Marshal(map[string]any{"planRunId": out.PlanRunID, "status": out.Status, "resultDigest": digest, "artifactCount": len(out.Artifacts)})
		if e != nil {
			return e
		}
		return s.putAudit(tx, "agent.run.reconciled", out.RunID, "plan-executor", digest, body, now)
	})
	return
}

func closePlanWork(tx Tx, runID string, succeeded bool, now time.Time) error {
	turns, err := tx.ListTurns(runID)
	if err != nil {
		return err
	}
	for _, turn := range turns {
		steps, err := tx.ListSteps(turn.ID)
		if err != nil {
			return err
		}
		for _, step := range steps {
			calls, err := tx.ListToolCalls(step.ID)
			if err != nil {
				return err
			}
			for _, call := range calls {
				if call.Status.Terminal() {
					continue
				}
				if succeeded {
					return fmt.Errorf("%w: unfinished tool", agentrun.ErrInvalid)
				}
				status := agentrun.CallCancelled
				if call.Status == agentrun.CallRunning {
					status = agentrun.CallOutcomeUnknown
				}
				call, err = call.Transition(status, now)
				if err != nil {
					return err
				}
				if err = tx.PutToolCall(call); err != nil {
					return err
				}
			}
			if !step.Status.Terminal() {
				step.Status, step.UpdatedAt = agentrun.StepFailed, now
				if succeeded {
					step.Status = agentrun.StepCompleted
				}
				if err = tx.PutStep(step); err != nil {
					return err
				}
			}
		}
		if !turn.Status.Terminal() {
			turn.Status, turn.UpdatedAt, turn.Version = agentrun.TurnFailed, now, turn.Version+1
			if succeeded {
				turn.Status = agentrun.TurnCompleted
			}
			if err = tx.PutTurn(turn); err != nil {
				return err
			}
		}
	}
	return nil
}

// RecoverPlanExecutions closes process-owned work after the generic runtime
// recovery scanner. No model or tool is replayed by recovery.
func (s *Service) RecoverPlanExecutions(ctx context.Context) error {
	var pending []agentrun.PlanExecution
	if err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		p, err := planExecutionTx(tx)
		if err != nil {
			return err
		}
		pending, err = p.ListPlanExecutions()
		return err
	}); err != nil {
		return err
	}
	for _, ex := range pending {
		if ex.Terminal() {
			continue
		}
		status := "interrupted"
		if ex.Status == "cancel_requested" {
			status = "cancelled"
		}
		if _, err := s.FinishPlanExecution(ctx, ex.PlanRunID, ex.RunID, status, "", "引擎重启，执行已中断；未自动重放工具", nil, nil); err != nil {
			return err
		}
	}
	return nil
}
