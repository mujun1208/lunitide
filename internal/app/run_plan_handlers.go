package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// M4-H handlers: run.plan.put/evidence.list.
// run.plan.put stores the agent-authored plan projection under optimistic
// concurrency (expectedVersion CAS) and commits plan + RunPlanPutCompleted
// event + audit + idempotency record in one transaction. evidence.list is a
// read-only projection over the append-only evidence recorded by the
// web/changeset/command flows; it stays readable after run termination.

type runPlanDTO struct {
	ID         string          `json:"id"`
	RunID      string          `json:"runId"`
	PlanDigest string          `json:"planDigest"`
	Plan       json.RawMessage `json:"plan"`
	Version    int64           `json:"version"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

func newRunPlanDTO(p agentrun.RunPlan) runPlanDTO {
	return runPlanDTO{
		ID:         p.ID,
		RunID:      p.RunID,
		PlanDigest: p.PlanDigest,
		Plan:       json.RawMessage(p.Content),
		Version:    p.Version,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

func handleRunPlanPut(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID           string          `json:"runId"`
		ExpectedVersion int64           `json:"expectedVersion"`
		Plan            json.RawMessage `json:"plan"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || p.ExpectedVersion < 0 || len(p.Plan) == 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.plan.put 参数无效", false)
	}
	var plan any
	if err := json.Unmarshal(p.Plan, &plan); err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.plan.put 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.RunPlanPut(ctx, r.IdempotencyKey, agentRunMutationActor, p, agentrunapp.RunPlanPutInput{
		RunID:           p.RunID,
		ExpectedVersion: p.ExpectedVersion,
		Plan:            plan,
	})
	if err != nil {
		return agentRunFailure(r, err)
	}
	return r.Ok(struct {
		Plan runPlanDTO `json:"plan"`
	}{newRunPlanDTO(result.Plan)})
}

func handleEvidenceList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "evidence.list 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	result, err := e.agentRuns.EvidenceList(ctx, p.RunID)
	if err != nil {
		return agentRunFailure(r, err)
	}
	items := make([]evidenceDTO, 0, len(result.Evidence))
	for _, ev := range result.Evidence {
		items = append(items, newEvidenceDTO(ev))
	}
	return r.Ok(struct {
		RunID    string        `json:"runId"`
		Evidence []evidenceDTO `json:"evidence"`
	}{result.RunID, items})
}
