package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/oklog/ulid/v2"
)

type planRunDTO struct {
	ID               string                    `json:"id"`
	ParentRunID      string                    `json:"parentRunId,omitempty"`
	PlanID           string                    `json:"planId"`
	NodeID           string                    `json:"nodeId"`
	Role             string                    `json:"role"`
	Todo             planTodoDTO               `json:"todo"`
	Status           agentorchestration.Status `json:"status"`
	Depth            int                       `json:"depth"`
	CreatedAt        time.Time                 `json:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
	Version          uint64                    `json:"version"`
	AgentRunID       string                    `json:"agentRunId,omitempty"`
	ExecutionStatus  string                    `json:"executionStatus,omitempty"`
	ExecutionVersion int64                     `json:"executionVersion,omitempty"`
	Summary          string                    `json:"summary,omitempty"`
	Failure          string                    `json:"failure,omitempty"`
	Artifacts        []agentrun.PlanArtifact   `json:"artifacts,omitempty"`
}
type planTodoDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func runDTO(r agentorchestration.AgentRun) planRunDTO {
	return planRunDTO{ID: r.ID, ParentRunID: r.ParentRunID, PlanID: r.PlanID, NodeID: r.NodeID, Role: r.Role, Todo: planTodoDTO{r.Todo.ID, r.Todo.Title, r.Todo.Description}, Status: r.Status, Depth: r.Depth, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version, AgentRunID: r.Todo.Metadata["agentRunId"], ExecutionStatus: r.Todo.Metadata["executionStatus"], Failure: r.Failure}
}
func coordinationResult(r agentorchestration.AgentRun) any {
	return struct {
		Run              planRunDTO `json:"run"`
		ExecutionStarted bool       `json:"executionStarted"`
	}{runDTO(r), r.Todo.Metadata["agentRunId"] != ""}
}
func orchestrationFailure(req bridge.Request, err error) bridge.Response {
	code, msg := "COORDINATION_INVALID", "协调状态请求无效"
	if errors.Is(err, agentorchestration.ErrNotFound) {
		code, msg = "COORDINATION_NOT_FOUND", "协调运行不存在"
	} else if errors.Is(err, agentorchestration.ErrDepthLimit) || errors.Is(err, agentorchestration.ErrConcurrencyLimit) {
		code, msg = "COORDINATION_LIMIT_REACHED", "协调状态已达到限制"
	} else if errors.Is(err, agentorchestration.ErrInvalidTransition) || errors.Is(err, agentorchestration.ErrNoChildren) {
		code, msg = "COORDINATION_CONFLICT", "协调状态冲突"
	} else if errors.Is(err, agentrun.ErrVersionConflict) || errors.Is(err, agentrun.ErrInvalidTransition) {
		code, msg = "PLAN_EXECUTION_CONFLICT", "任务已发生变化，请刷新后处理"
	} else if errors.Is(err, planningapp.ErrPlanNotActive) {
		code, msg = "PLAN_NOT_ACTIVE", "请先激活或恢复计划"
	} else if errors.Is(err, planningapp.ErrReviewRequired) {
		code, msg = "PLAN_REVIEW_REQUIRED", "当前任务需要有效审批后才能执行"
	} else if errors.Is(err, planningapp.ErrNodeNotReady) || errors.Is(err, planningapp.ErrDependencyNotMet) {
		code, msg = "PLAN_NODE_NOT_READY", "任务依赖尚未完成，或节点当前不可执行"
	}
	return req.Fail(code, msg, false)
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := e.coordinator.CreateRoot(ctx, p.PlanID, p.NodeID, p.Role, agentorchestration.Todo{ID: ulid.Make().String(), Title: p.Title, Description: *p.Description})
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return r.Ok(coordinationResult(run))
}
func handlePlanRunStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	if e.agentRuns == nil || e.tools == nil || e.providers == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "计划执行服务暂时不可用", true)
	}
	if _, err := e.startPlanExecution(ctx, p.RunID); err != nil {
		return orchestrationFailure(r, err)
	}
	run, err := e.coordinator.Get(ctx, p.RunID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return r.Ok(coordinationResult(run))
}
func handlePlanRunCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return runIDMutation(e, ctx, r, func(ctx context.Context, id string) (agentorchestration.AgentRun, error) {
		root, err := e.coordinator.Get(ctx, id)
		if err != nil {
			return root, err
		}
		runs, err := e.coordinator.ListPlanRuns(ctx, root.PlanID)
		if err != nil {
			return root, err
		}
		ids := map[string]bool{id: true}
		for changed := true; changed; {
			changed = false
			for _, run := range runs {
				if ids[run.ParentRunID] && !ids[run.ID] {
					ids[run.ID] = true
					changed = true
				}
			}
		}
		e.cancelPlanExecutionWorkers(ids)
		for runID := range ids {
			if e.agentRuns != nil {
				if _, err = e.agentRuns.CancelPlanExecution(ctx, runID); err != nil && !errors.Is(err, agentrun.ErrNotFound) {
					return root, err
				}
			}
		}
		return e.coordinator.CancelRun(ctx, id)
	})
}
func runIDMutation(e *Engine, ctx context.Context, r bridge.Request, fn func(context.Context, string) (agentorchestration.AgentRun, error)) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := fn(ctx, p.RunID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return r.Ok(coordinationResult(run))
}
func handlePlanRunTree(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	runs, err := e.coordinator.ListPlanRuns(ctx, p.PlanID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	items := make([]planRunDTO, len(runs))
	for i, x := range runs {
		items[i] = runDTO(x)
		if e.agentRuns != nil {
			if execution, err := e.agentRuns.GetPlanExecution(ctx, x.ID); err == nil {
				items[i].AgentRunID = execution.RunID
				items[i].ExecutionStatus = execution.Status
				items[i].ExecutionVersion = execution.Version
				items[i].Summary = execution.Summary
				items[i].Failure = execution.Failure
				items[i].Artifacts = execution.Artifacts
			} else if !errors.Is(err, agentrun.ErrNotFound) {
				return orchestrationFailure(r, err)
			}
		}
	}
	return r.Ok(struct {
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	run, err := e.coordinator.SpawnChild(ctx, p.ParentRunID, p.NodeID, p.Role, agentorchestration.Todo{ID: ulid.Make().String(), Title: p.Title, Description: *p.Description})
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return r.Ok(coordinationResult(run))
}
func handlePlanRunJoin(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string                      `json:"runId"`
		Mode  agentorchestration.JoinMode `json:"mode"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || (p.Mode != agentorchestration.JoinAll && p.Mode != agentorchestration.JoinAny) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	if e.agentRuns != nil {
		if _, err := e.agentRuns.GetPlanExecution(ctx, p.RunID); err == nil {
			return r.Fail("COORDINATION_CONFLICT", "执行任务由工具与产物验证收尾，不能用协调汇合覆盖执行结果", false)
		} else if !errors.Is(err, agentrun.ErrNotFound) {
			return orchestrationFailure(r, err)
		}
	}
	run, err := e.coordinator.JoinChildren(ctx, p.RunID, p.Mode)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	return r.Ok(coordinationResult(run))
}

func handlePlanRunRetry(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID                string `json:"runId"`
		ExpectedVersion      int64  `json:"expectedVersion"`
		AcknowledgeUncertain bool   `json:"acknowledgeUncertain"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || p.ExpectedVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "参数无效", false)
	}
	ex, err := e.launchPlanExecution(ctx, p.RunID, p.ExpectedVersion, p.AcknowledgeUncertain)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	run, err := e.coordinator.Get(ctx, p.RunID)
	if err != nil {
		return orchestrationFailure(r, err)
	}
	dto := runDTO(run)
	dto.ExecutionVersion = ex.Version
	return r.Ok(struct {
		Run              planRunDTO `json:"run"`
		ExecutionStarted bool       `json:"executionStarted"`
	}{dto, true})
}
