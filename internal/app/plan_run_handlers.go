package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type planRunDTO struct {
	ID          string                    `json:"id"`
	ParentRunID string                    `json:"parentRunId,omitempty"`
	PlanID      string                    `json:"planId"`
	NodeID      string                    `json:"nodeId"`
	Role        string                    `json:"role"`
	Todo        planTodoDTO               `json:"todo"`
	Status      agentorchestration.Status `json:"status"`
	Depth       int                       `json:"depth"`
	CreatedAt   time.Time                 `json:"createdAt"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
	Version     uint64                    `json:"version"`
}
type planTodoDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func runDTO(r agentorchestration.AgentRun) planRunDTO {
	return planRunDTO{r.ID, r.ParentRunID, r.PlanID, r.NodeID, r.Role, planTodoDTO{r.Todo.ID, r.Todo.Title, r.Todo.Description}, r.Status, r.Depth, r.CreatedAt, r.UpdatedAt, r.Version}
}
func coordinationResult(r agentorchestration.AgentRun) any {
	return struct {
		Run              planRunDTO `json:"run"`
		ExecutionStarted bool       `json:"executionStarted"`
	}{runDTO(r), false}
}
func orchestrationFailure(req bridge.Request, err error) bridge.Response {
	code, msg := "COORDINATION_INVALID", "协调状态请求无效"
	if errors.Is(err, agentorchestration.ErrNotFound) {
		code, msg = "COORDINATION_NOT_FOUND", "协调运行不存在"
	} else if errors.Is(err, agentorchestration.ErrDepthLimit) || errors.Is(err, agentorchestration.ErrConcurrencyLimit) {
		code, msg = "COORDINATION_LIMIT_REACHED", "协调状态已达到限制"
	} else if errors.Is(err, agentorchestration.ErrInvalidTransition) || errors.Is(err, agentorchestration.ErrNoChildren) {
		code, msg = "COORDINATION_CONFLICT", "协调状态冲突"
	}
	return bridge.Failure(req.ID, req.TraceID, code, msg, false)
}
func validRunText(p *struct{ Role, Title, Description string }) bool {
	return strings.TrimSpace(p.Role) != "" && len(p.Role) <= 128 && strings.TrimSpace(p.Title) != "" && len(p.Title) <= 200 && len(p.Description) <= 4096
}
func handlePlanTodoCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID      string  `json:"planId"`
		NodeID      string  `json:"nodeId"`
		Role        string  `json:"role"`
		Title       string  `json:"title"`
		Description *string `json:"description"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Description == nil || !validCanonicalULID(p.PlanID) || !validCanonicalULID(p.NodeID) || !validRunText(&struct{ Role, Title, Description string }{p.Role, p.Title, *p.Description}) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := e.coordinator.CreateRoot(ctx, p.PlanID, p.NodeID, p.Role, agentorchestration.Todo{ID: ulid.Make().String(), Title: p.Title, Description: *p.Description})
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return bridge.Success(r.ID, coordinationResult(run))
}
func handlePlanRunStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return runIDMutation(e, ctx, r, e.coordinator.Start)
}
func handlePlanRunCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return runIDMutation(e, ctx, r, e.coordinator.CancelRun)
}
func runIDMutation(e *Engine, ctx context.Context, r bridge.Request, fn func(context.Context, string) (agentorchestration.AgentRun, error)) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := fn(ctx, p.RunID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return bridge.Success(r.ID, coordinationResult(run))
}
func handlePlanRunTree(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	runs, err := e.coordinator.ListPlanRuns(ctx, p.PlanID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	items := make([]planRunDTO, len(runs))
	for i, x := range runs {
		items[i] = runDTO(x)
	}
	return bridge.Success(r.ID, struct {
		Items []planRunDTO `json:"items"`
	}{items})
}
func handlePlanRunSpawn(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ParentRunID string  `json:"parentRunId"`
		NodeID      string  `json:"nodeId"`
		Role        string  `json:"role"`
		Title       string  `json:"title"`
		Description *string `json:"description"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Description == nil || !validCanonicalULID(p.ParentRunID) || !validCanonicalULID(p.NodeID) || !validRunText(&struct{ Role, Title, Description string }{p.Role, p.Title, *p.Description}) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := e.coordinator.SpawnChild(ctx, p.ParentRunID, p.NodeID, p.Role, agentorchestration.Todo{ID: ulid.Make().String(), Title: p.Title, Description: *p.Description})
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return bridge.Success(r.ID, coordinationResult(run))
}
func handlePlanRunJoin(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string                      `json:"runId"`
		Mode  agentorchestration.JoinMode `json:"mode"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || (p.Mode != agentorchestration.JoinAll && p.Mode != agentorchestration.JoinAny) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := e.coordinator.JoinChildren(ctx, p.RunID, p.Mode)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return bridge.Success(r.ID, coordinationResult(run))
}
