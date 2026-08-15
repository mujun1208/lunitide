package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-1 handlers (T-7.1.x): workflow.createVersion / workflow.publish /
// workflow.startStage / workflow.transitionStage / workflow.captureInput.
//
// Error mapping follows the M7 wire contract: fixed-set/DAG violations map to
// M7-WF-002/M7-WF-003, optimistic-lock conflicts to M7-WF-004, duplicate
// (project,version) to M7-WF-005 and the published-immutability trigger to
// M7-WF-001. Stage-level guards reuse the same family.

func handleWorkflowCreateVersion(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		RequestID string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.createVersion 参数无效", false)
	}
	if e.m7workflow == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工作流服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	v, err := e.m7workflow.CreateVersion(ctx, p.ProjectID)
	if err != nil {
		return m7WorkflowFailure(r, err, "workflow.createVersion")
	}
	return bridge.Success(r.ID, struct {
		WorkflowVersionID string `json:"workflowVersionId"`
		Version           int64  `json:"version"`
		Status            string `json:"status"`
		DefinitionDigest  string `json:"definitionDigest"`
	}{v.ID, v.Version, v.Status, v.DefinitionDigest})
}

func handleWorkflowPublish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		WorkflowVersionID string `json:"workflowVersionId"`
		RequestID         string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.WorkflowVersionID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.publish 参数无效", false)
	}
	if e.m7workflow == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工作流服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	v, err := e.m7workflow.Publish(ctx, p.WorkflowVersionID)
	if err != nil {
		return m7WorkflowFailure(r, err, "workflow.publish")
	}
	publishedAt := ""
	if v.PublishedAt != nil {
		publishedAt = v.PublishedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return bridge.Success(r.ID, struct {
		WorkflowVersionID string `json:"workflowVersionId"`
		Status            string `json:"status"`
		PublishedAt       string `json:"publishedAt"`
	}{v.ID, v.Status, publishedAt})
}

func handleWorkflowStartStage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		StageKey  string `json:"stageKey"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || !m7FixedStageKey(p.StageKey) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.startStage 参数无效", false)
	}
	if e.m7workflow == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工作流服务暂时不可用", true)
	}
	res, err := e.m7workflow.StartStage(ctx, p.ProjectID, p.StageKey)
	if err != nil {
		return m7WorkflowFailure(r, err, "workflow.startStage")
	}
	return bridge.Success(r.ID, struct {
		InstanceID       string `json:"instanceId"`
		StageRunID       string `json:"stageRunId"`
		State            string `json:"state"`
		AttemptNo        int64  `json:"attemptNo"`
		Created          bool   `json:"created"`
		DependenciesMet  bool   `json:"dependenciesMet"`
	}{res.Instance.ID, res.Run.ID, res.Run.State, res.Run.AttemptNo, res.NewRun, !res.Dependent})
}

func handleWorkflowTransitionStage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StageRunID      string `json:"stageRunId"`
		To              string `json:"to"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StageRunID) ||
		!m7TargetRunState(p.To) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.transitionStage 参数无效", false)
	}
	if e.m7workflow == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工作流服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	// The optimistic lock is checked against the caller's expectedVersion:
	// a mismatch is M7-WF-004 even before the canonical machine rejects it.
	run, err := e.m7workflow.TransitionStageChecked(ctx, p.StageRunID, p.To, p.ExpectedVersion)
	if err != nil {
		return m7WorkflowFailure(r, err, "workflow.transitionStage")
	}
	return bridge.Success(r.ID, struct {
		StageRunID  string `json:"stageRunId"`
		State       string `json:"state"`
		LockVersion int64  `json:"lockVersion"`
	}{run.ID, run.State, run.LockVersion})
}

func handleWorkflowCaptureInput(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StageRunID string         `json:"stageRunId"`
		Inputs     map[string]any `json:"inputs"`
		RequestID  string         `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StageRunID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.Inputs) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.captureInput 参数无效", false)
	}
	if e.m7workflow == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工作流服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	snap, err := e.m7workflow.CaptureInput(ctx, p.StageRunID, p.Inputs)
	if err != nil {
		return m7WorkflowFailure(r, err, "workflow.captureInput")
	}
	return bridge.Success(r.ID, struct {
		SnapshotID    string `json:"snapshotId"`
		SnapshotDigest string `json:"snapshotDigest"`
		CapturedAt    string `json:"capturedAt"`
	}{snap.ID, snap.Digest, snap.CapturedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")})
}

// m7FixedStageKey reports whether key is one of the nine fixed stage keys
// (subprocess keys like integration/security are rejected here first).
func m7FixedStageKey(key string) bool {
	switch key {
	case "INITIATION_BOUNDARY", "RESEARCH_EVIDENCE", "REQUIREMENT_DEFINITION",
		"SOLUTION_EXPERIENCE", "ARCHITECTURE_PLAN", "DEVELOPMENT_CHANGE",
		"VERIFICATION_ACCEPTANCE", "RELEASE_DELIVERY", "OPERATIONS_RETROSPECTIVE":
		return true
	}
	return false
}

// m7TargetRunState reports whether to is a legal transition target (draft is
// only ever set at creation, never a wire target).
func m7TargetRunState(to string) bool {
	switch to {
	case "ready", "running", "waiting_review", "approved", "completed",
		"blocked", "paused", "cancelled":
		return true
	}
	return false
}

// m7WorkflowFailure maps m7app/m7flow errors onto the M7 wire family.
func m7WorkflowFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrProjectNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "项目不存在", false)
	case errors.Is(err, m7app.ErrVersionNotFound), errors.Is(err, m7app.ErrInstanceNotFound),
		errors.Is(err, m7app.ErrStageRunNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "工作流对象不存在", false)
	case errors.Is(err, m7app.ErrAlreadyPublished):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-001", "已发布版本不可变，请克隆新版本", false)
	case errors.Is(err, m7flow.ErrStageFixedSet):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-002", "阶段集合未匹配全局九阶段固定模型", false)
	case errors.Is(err, m7flow.ErrStageCycle):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-003", "阶段依赖存在循环", false)
	case errors.Is(err, m7app.ErrNotPublished):
		return bridge.Failure(r.ID, r.TraceID, "WORKFLOW_VERSION_UNPUBLISHED", "工作流版本未发布，请先发布版本", false)
	case errors.Is(err, m7app.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-004", "乐观锁冲突，请刷新后重试", false)
	case errors.Is(err, m7app.ErrDuplicateVersion):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-005", "工作流版本已存在", false)
	case errors.Is(err, m7app.ErrIllegalTransition):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-006", "非法阶段状态转换", false)
	case errors.Is(err, m7app.ErrDependencyIncomplete):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-007", "上游阶段未完成", false)
	case errors.Is(err, m7app.ErrSnapshotChanged):
		return bridge.Failure(r.ID, r.TraceID, "M7-SNP-002", "阶段输入已变化，请重新捕获", false)
	case errors.Is(err, m7app.ErrM6TreeDigest):
		return bridge.Failure(r.ID, r.TraceID, "M7-SNP-001", "M6 final-tree 输入缺少 rootId/digest", false)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
