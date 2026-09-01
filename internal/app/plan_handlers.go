package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/planningapp"
)

type PlanningService interface {
	Get(context.Context, string) (*planning.Plan, error)
	ListByProject(context.Context, string) ([]planning.Plan, error)
	ListNodes(context.Context, string) ([]planning.Node, error)
	CreatePlan(context.Context, planning.Plan) (planning.Plan, error)
	CreateNode(context.Context, planning.Node) (planning.Node, error)
	Activate(context.Context, string) error
	CompletePlan(context.Context, string) error
	StartNode(context.Context, string) error
	CompleteNode(context.Context, string) error
	FailNode(context.Context, string) error
	PausePlan(context.Context, string) error
	ResumePlan(context.Context, string) error
}

type planDTO struct {
	ID          string              `json:"id"`
	ProjectID   string              `json:"projectId"`
	StageID     *string             `json:"stageId,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Version     int64               `json:"version"`
	Status      planning.PlanStatus `json:"status"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

type planNodeDTO struct {
	ID             string              `json:"id"`
	PlanID         string              `json:"planId"`
	ParentNodeID   *string             `json:"parentNodeId,omitempty"`
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	Status         planning.NodeStatus `json:"status"`
	RiskLevel      planning.RiskLevel  `json:"riskLevel"`
	BudgetTokens   *int64              `json:"budgetTokens,omitempty"`
	EstimateTokens *int64              `json:"estimateTokens,omitempty"`
	WorkerRole     string              `json:"workerRole"`
	Sequence       int64               `json:"sequence"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
}

func newPlanDTO(p planning.Plan) planDTO {
	return planDTO{
		ID:          p.ID,
		ProjectID:   p.ProjectID,
		StageID:     p.StageID,
		Name:        p.Name,
		Description: p.Description,
		Version:     p.Version,
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func newPlanNodeDTO(n planning.Node) planNodeDTO {
	return planNodeDTO{
		ID:             n.ID,
		PlanID:         n.PlanID,
		ParentNodeID:   n.ParentNodeID,
		Name:           n.Name,
		Description:    n.Description,
		Status:         n.Status,
		RiskLevel:      n.RiskLevel,
		BudgetTokens:   n.BudgetTokens,
		EstimateTokens: n.EstimateTokens,
		WorkerRole:     n.WorkerRole,
		Sequence:       n.Sequence,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
	}
}

func planningServiceAvailable(service PlanningService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Pointer || !v.IsNil()
}

func handlePlanGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.get 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	plan, err := e.planning.Get(ctx, p.ID)
	if err != nil {
		return planFailure(r, err)
	}
	return r.Ok(newPlanDTO(*plan))
}

func handlePlanCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID   string  `json:"projectId"`
		StageID     *string `json:"stageId,omitempty"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.Name) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.create 参数无效", false)
	}
	if p.StageID != nil && !validCanonicalULID(*p.StageID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.create 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	plan, err := e.planning.CreatePlan(ctx, planning.Plan{
		ProjectID:   p.ProjectID,
		StageID:     p.StageID,
		Name:        p.Name,
		Description: p.Description,
	})
	if err != nil {
		return planFailure(r, err)
	}
	return r.Ok(struct {
		Plan planDTO `json:"plan"`
	}{Plan: newPlanDTO(plan)})
}

func handlePlanList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.list 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	items, err := e.planning.ListByProject(ctx, p.ProjectID)
	if err != nil {
		return planFailure(r, err)
	}
	dtos := make([]planDTO, len(items))
	for i := range items {
		dtos[i] = newPlanDTO(items[i])
	}
	return r.Ok(struct {
		Items []planDTO `json:"items"`
	}{Items: dtos})
}

func handlePlanActivate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.activate 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.Activate(ctx, p.PlanID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"activated": true})
}

func handlePlanComplete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.complete 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.CompletePlan(ctx, p.PlanID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"completed": true})
}

func handlePlanPause(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.pause 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.PausePlan(ctx, p.PlanID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"paused": true})
}

func handlePlanResume(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "plan.resume 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.ResumePlan(ctx, p.PlanID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"resumed": true})
}

func handleNodeList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID string `json:"planId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.list 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	items, err := e.planning.ListNodes(ctx, p.PlanID)
	if err != nil {
		return planFailure(r, err)
	}
	dtos := make([]planNodeDTO, len(items))
	for i := range items {
		dtos[i] = newPlanNodeDTO(items[i])
	}
	return r.Ok(struct {
		Items []planNodeDTO `json:"items"`
	}{Items: dtos})
}

func handleNodeCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PlanID         string  `json:"planId"`
		ParentNodeID   *string `json:"parentNodeId,omitempty"`
		Name           string  `json:"name"`
		Description    string  `json:"description"`
		RiskLevel      string  `json:"riskLevel"`
		BudgetTokens   *int64  `json:"budgetTokens,omitempty"`
		EstimateTokens *int64  `json:"estimateTokens,omitempty"`
		WorkerRole     string  `json:"workerRole"`
		Sequence       int64   `json:"sequence"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PlanID) || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.RiskLevel) == "" || strings.TrimSpace(p.WorkerRole) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.create 参数无效", false)
	}
	if p.ParentNodeID != nil && !validCanonicalULID(*p.ParentNodeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.create 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	node, err := e.planning.CreateNode(ctx, planning.Node{
		PlanID:         p.PlanID,
		ParentNodeID:   p.ParentNodeID,
		Name:           p.Name,
		Description:    p.Description,
		RiskLevel:      planning.RiskLevel(p.RiskLevel),
		BudgetTokens:   p.BudgetTokens,
		EstimateTokens: p.EstimateTokens,
		WorkerRole:     p.WorkerRole,
		Sequence:       p.Sequence,
	})
	if err != nil {
		return planFailure(r, err)
	}
	return r.Ok(struct {
		Node planNodeDTO `json:"node"`
	}{Node: newPlanNodeDTO(node)})
}

func handleNodeStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		NodeID string `json:"nodeId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.NodeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.start 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.StartNode(ctx, p.NodeID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"started": true})
}

func handleNodeComplete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		NodeID string `json:"nodeId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.NodeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.complete 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.CompleteNode(ctx, p.NodeID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"completed": true})
}

func handleNodeFail(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		NodeID string `json:"nodeId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.NodeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "node.fail 参数无效", false)
	}
	if !planningServiceAvailable(e.planning) {
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
	if err := e.planning.FailNode(ctx, p.NodeID); err != nil {
		return planFailure(r, err)
	}
	return r.Ok(map[string]any{"failed": true})
}

func planFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, planningapp.ErrPlanNotFound):
		return r.Fail("PLAN_NOT_FOUND", "计划不存在", false)
	case errors.Is(err, planningapp.ErrNodeNotFound):
		return r.Fail("NODE_NOT_FOUND", "节点不存在", false)
	case errors.Is(err, planningapp.ErrInvalidTransition):
		return r.Fail("PLAN_INVALID_TRANSITION", "状态转换无效", false)
	case errors.Is(err, planningapp.ErrPlanNotActive):
		return r.Fail("PLAN_NOT_ACTIVE", "计划未激活", false)
	case errors.Is(err, planningapp.ErrNodeNotReady):
		return r.Fail("NODE_NOT_READY", "节点未就绪", false)
	case errors.Is(err, planningapp.ErrCyclicDependency):
		return r.Fail("PLAN_CYCLIC_DEPENDENCY", "计划存在循环依赖", false)
	case errors.Is(err, planningapp.ErrReviewRequired):
		return r.Fail("REVIEW_REQUIRED", "此操作需要先通过治理审批", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "计划数据暂时不可用", true)
	}
}
