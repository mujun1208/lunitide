// M4-H plan/evidence use cases: run.plan.put and evidence.list.
// run.plan.put is a pure projection write — no external side effect — so the
// whole use case (idempotency claim, running-run guard, version CAS, plan
// upsert, run event, audit) commits in one SQLite transaction. evidence.list
// is a read-only projection over the append-only evidence recorded by the
// web/changeset/command flows (PRD M4-FR-08).
package agentrunapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// planMaxCanonicalBytes bounds the canonical plan document. The plan shape is
// deliberately not standardized yet (M5 template format is ADR-012 blocked);
// any JSON object within the size cap is accepted.
const planMaxCanonicalBytes = 64 << 10

// RunPlanPutInput is the validated run.plan.put request. Plan holds the raw
// plan document as decoded JSON (any object shape).
type RunPlanPutInput struct {
	RunID           string
	ExpectedVersion int64
	Plan            any
}

// RunPlanPutResult is the committed plan projection.
type RunPlanPutResult struct {
	Plan agentrun.RunPlan `json:"plan"`
}

// RunPlanPut stores the run's plan projection with optimistic concurrency:
// expectedVersion 0 requires that no plan exists yet (create at version 1);
// otherwise it must equal the stored version (update to version+1). The
// stored content is the canonical JSON encoding and planDigest its SHA-256,
// so two callers putting semantically equal plans converge on one digest.
func (s *Service) RunPlanPut(ctx context.Context, key, actor string, request any, in RunPlanPutInput) (RunPlanPutResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return RunPlanPutResult{}, ErrIdempotencyKeyRequired
	}
	if err := s.available(); err != nil {
		return RunPlanPutResult{}, err
	}
	digest, err := requestDigest(request)
	if err != nil {
		return RunPlanPutResult{}, err
	}
	content, planDigest, err := canonicalPlan(in.Plan)
	if err != nil {
		return RunPlanPutResult{}, err
	}
	var result RunPlanPutResult
	err = s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency("run.plan.put", key, now)
		if err != nil {
			return err
		}
		if found {
			return replay(record, digest, &result)
		}
		run, err := tx.GetRun(in.RunID)
		if err != nil {
			return err
		}
		if run.Status != agentrun.RunRunning {
			return fmt.Errorf("%w: run.plan.put requires a running run, got %s", agentrun.ErrInvalidTransition, run.Status)
		}
		plan, err := tx.GetRunPlan(in.RunID)
		switch {
		case err == agentrun.ErrNotFound:
			if in.ExpectedVersion != 0 {
				return fmt.Errorf("%w: run.plan.put create expects version 0, got %d", agentrun.ErrVersionConflict, in.ExpectedVersion)
			}
			plan = agentrun.RunPlan{ID: ulid.Make().String(), RunID: run.ID, Version: 0, CreatedAt: now}
		case err != nil:
			return err
		case plan.Version != in.ExpectedVersion:
			return fmt.Errorf("%w: run.plan.put expected %d, got %d", agentrun.ErrVersionConflict, in.ExpectedVersion, plan.Version)
		}
		plan.PlanDigest = planDigest
		plan.Content = content
		plan.Version++
		plan.UpdatedAt = now
		if err := plan.Validate(); err != nil {
			return err
		}
		if err := tx.PutRunPlan(plan); err != nil {
			return err
		}
		if err := appendRunEvent(tx, run.ID, agentrun.EventRunPlanPutCompleted, map[string]any{
			"schemaVersion": 1,
			"runId":         run.ID,
			"planId":        plan.ID,
			"planDigest":    plan.PlanDigest,
			"version":       plan.Version,
		}, now); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"planId": plan.ID, "planDigest": plan.PlanDigest, "version": plan.Version})
		if err := s.putAudit(tx, "run.plan.updated", run.ID, actor, digest, meta, now); err != nil {
			return err
		}
		result = RunPlanPutResult{Plan: plan}
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.PutIdempotency(providerapp.Record{Operation: "run.plan.put", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// canonicalPlan validates and canonicalizes the plan document: it must be a
// JSON object within the size cap. The digest is over the canonical encoding.
func canonicalPlan(plan any) ([]byte, string, error) {
	switch plan.(type) {
	case map[string]any:
	default:
		return nil, "", fmt.Errorf("%w: plan must be a JSON object", agentrun.ErrInvalid)
	}
	content, err := token.CanonicalJSON(plan)
	if err != nil {
		return nil, "", fmt.Errorf("%w: plan is not encodable JSON", agentrun.ErrInvalid)
	}
	if len(content) > planMaxCanonicalBytes {
		return nil, "", fmt.Errorf("%w: plan exceeds %d bytes", agentrun.ErrInvalid, planMaxCanonicalBytes)
	}
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:]), nil
}

// EvidenceListResult is the read-only evidence projection for one run.
type EvidenceListResult struct {
	RunID    string             `json:"runId"`
	Evidence []agentrun.Evidence `json:"evidence"`
}

// EvidenceList returns the append-only evidence recorded for a run, in
// capture order. Evidence stays readable after the run terminates: it is the
// provenance trail, not live state.
func (s *Service) EvidenceList(ctx context.Context, runID string) (EvidenceListResult, error) {
	if err := s.available(); err != nil {
		return EvidenceListResult{}, err
	}
	var result EvidenceListResult
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		if _, err := tx.GetRun(runID); err != nil {
			return err
		}
		evidence, err := tx.ListEvidence(runID)
		if err != nil {
			return err
		}
		result = EvidenceListResult{RunID: runID, Evidence: evidence}
		return nil
	})
	return result, err
}
