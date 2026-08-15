package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-2 handlers (T-7.2.x): trace.addEdge / trace.query /
// trace.markStale / trace.resolveStale / workflow.evaluateGate /
// workflow.createCheckpoint / review.submit / devTask.create /
// devTask.transition / evidence.attachTest / evidence.attachScan.
//
// Error mapping follows the M7 wire contract: dangling trace endpoints map
// to M7-TRC-001, digest-conflicting duplicate edges to M7-TRC-003,
// checkpoint denials to M7-CHK-001, self reviews to M7-REV-001 and illegal
// dev-task transitions to M7-TSK-001.

// m7TraceDigest reports whether d is a lowercase 64-hex sha256 digest.
func m7TraceDigest(d string) bool {
	if len(d) != 64 {
		return false
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// m7TraceNodeType reports whether t is one of the traceable node types
// (mirrors the storage nodeTables registry, TRC-001).
func m7TraceNodeType(t string) bool {
	switch t {
	case "project", "workflow_version", "workflow_instance", "stage_run",
		"stage_input_snapshot", "artifact_version", "review", "trace_edge",
		"dev_task", "test_run", "scan_run":
		return true
	}
	return false
}

func handleTraceAddEdge(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FromType   string `json:"fromType"`
		FromID     string `json:"fromId"`
		FromDigest string `json:"fromDigest"`
		Relation   string `json:"relation"`
		ToType     string `json:"toType"`
		ToID       string `json:"toId"`
		ToDigest   string `json:"toDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || !m7TraceNodeType(p.FromType) || !validCanonicalULID(p.FromID) ||
		!m7TraceDigest(p.FromDigest) || !m7flow.LegalRelation(p.Relation) ||
		!m7TraceNodeType(p.ToType) || !validCanonicalULID(p.ToID) || !m7TraceDigest(p.ToDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "trace.addEdge 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "追踪服务暂时不可用", true)
	}
	edge, err := e.m7trace.AddEdge(ctx, m7flow.TraceEdge{
		FromType: p.FromType, FromID: p.FromID, FromDigest: p.FromDigest,
		Relation: p.Relation, ToType: p.ToType, ToID: p.ToID, ToDigest: p.ToDigest,
	})
	if err != nil {
		return m7EvidenceFailure(r, err, "trace.addEdge")
	}
	return bridge.Success(r.ID, struct {
		EdgeID string `json:"edgeId"`
	}{edge.ID})
}

func handleTraceQuery(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootType   string `json:"rootType"`
		RootID     string `json:"rootId"`
		Direction  string `json:"direction"`
		Depth      int    `json:"depth"`
		NextCursor string `json:"nextCursor"`
	}
	if decodePayload(r.Payload, &p) != nil || !m7TraceNodeType(p.RootType) || !validCanonicalULID(p.RootID) ||
		(p.Direction != "up" && p.Direction != "down") || p.Depth < 1 || p.Depth > 10 ||
		len(p.NextCursor) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "trace.query 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "追踪服务暂时不可用", true)
	}
	page, err := e.m7trace.Query(ctx, p.RootType, p.RootID, p.Direction, p.Depth, p.NextCursor)
	if err != nil {
		return m7EvidenceFailure(r, err, "trace.query")
	}
	type edgeDTO struct {
		EdgeID     string `json:"edgeId"`
		FromType   string `json:"fromType"`
		FromID     string `json:"fromId"`
		FromDigest string `json:"fromDigest"`
		Relation   string `json:"relation"`
		ToType     string `json:"toType"`
		ToID       string `json:"toId"`
		ToDigest   string `json:"toDigest"`
		CreatedAt  string `json:"createdAt"`
	}
	edges := make([]edgeDTO, 0, len(page.Edges))
	for _, ed := range page.Edges {
		edges = append(edges, edgeDTO{
			ed.ID, ed.FromType, ed.FromID, ed.FromDigest, ed.Relation,
			ed.ToType, ed.ToID, ed.ToDigest,
			ed.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
	}
	return bridge.Success(r.ID, struct {
		Edges      []edgeDTO `json:"edges"`
		NextCursor string    `json:"nextCursor"`
	}{edges, page.NextCursor})
}

func handleTraceMarkStale(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectType string `json:"subjectType"`
		SubjectID   string `json:"subjectId"`
		CauseEdge   string `json:"causeEdge"`
	}
	if decodePayload(r.Payload, &p) != nil || !m7TraceNodeType(p.SubjectType) || !validCanonicalULID(p.SubjectID) ||
		len(p.CauseEdge) < 1 || len(p.CauseEdge) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "trace.markStale 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "追踪服务暂时不可用", true)
	}
	mark, err := e.m7trace.MarkStale(ctx, p.SubjectType, p.SubjectID, p.CauseEdge)
	if err != nil {
		return m7EvidenceFailure(r, err, "trace.markStale")
	}
	return bridge.Success(r.ID, struct {
		StaleMarkID string `json:"staleMarkId"`
	}{mark.ID})
}

func handleTraceResolveStale(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StaleMarkID    string `json:"staleMarkId"`
		ResolutionType string `json:"resolutionType"`
		ReevaluationID string `json:"reevaluationId"`
		ResolvedBy     string `json:"resolvedBy"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StaleMarkID) ||
		(p.ResolutionType != m7flow.ResolveRecaptured && p.ResolutionType != m7flow.ResolveReevaluated && p.ResolutionType != m7flow.ResolveWaived) ||
		(p.ReevaluationID != "" && !validCanonicalULID(p.ReevaluationID)) ||
		len(p.ResolvedBy) < 1 || len(p.ResolvedBy) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "trace.resolveStale 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "追踪服务暂时不可用", true)
	}
	res, err := e.m7trace.ResolveStale(ctx, p.StaleMarkID, p.ResolutionType, p.ReevaluationID, p.ResolvedBy)
	if err != nil {
		return m7EvidenceFailure(r, err, "trace.resolveStale")
	}
	return bridge.Success(r.ID, struct {
		ResolutionID string `json:"resolutionId"`
	}{res.ID})
}

func handleWorkflowEvaluateGate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StageRunID string `json:"stageRunId"`
		GateKey    string `json:"gateKey"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StageRunID) || !m7GateKey(p.GateKey) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.evaluateGate 参数无效", false)
	}
	if e.m7gate == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "门禁服务暂时不可用", true)
	}
	eval, err := e.m7gate.Evaluate(ctx, p.StageRunID, p.GateKey)
	if err != nil {
		return m7EvidenceFailure(r, err, "workflow.evaluateGate")
	}
	findings := eval.Findings
	if findings == nil {
		findings = []m7flow.Finding{}
	}
	return bridge.Success(r.ID, struct {
		Decision string           `json:"decision"`
		Findings []m7flow.Finding `json:"findings"`
	}{eval.Decision, findings})
}

// m7GateKey reports whether k is one of the four server-side gate keys.
func m7GateKey(k string) bool {
	switch k {
	case "stage.exit", "dev.integration", "verify.security", "release.package":
		return true
	}
	return false
}

func handleWorkflowCreateCheckpoint(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StageRunID string `json:"stageRunId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StageRunID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workflow.createCheckpoint 参数无效", false)
	}
	if e.m7gate == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "门禁服务暂时不可用", true)
	}
	cp, err := e.m7gate.CreateCheckpoint(ctx, p.StageRunID)
	if err != nil {
		return m7EvidenceFailure(r, err, "workflow.createCheckpoint")
	}
	return bridge.Success(r.ID, struct {
		CheckpointID string `json:"checkpointId"`
		Sequence     int64  `json:"sequence"`
	}{cp.ID, cp.Sequence})
}

func handleReviewSubmit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectType    string `json:"subjectType"`
		SubjectID      string `json:"subjectId"`
		SubjectVersion int64  `json:"subjectVersion"`
		Verdict        string `json:"verdict"`
		ReviewerID     string `json:"reviewerId"`
		AuthorID       string `json:"authorId"`
		Reason         string `json:"reason"`
	}
	if decodePayload(r.Payload, &p) != nil || !m7TraceNodeType(p.SubjectType) || !validCanonicalULID(p.SubjectID) ||
		p.SubjectVersion < 0 ||
		(p.Verdict != m7flow.VerdictApprove && p.Verdict != m7flow.VerdictReject) ||
		len(p.ReviewerID) < 1 || len(p.ReviewerID) > 128 ||
		len(p.AuthorID) < 1 || len(p.AuthorID) > 128 ||
		len(p.Reason) < 1 || len(p.Reason) > 2000 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "review.submit 参数无效", false)
	}
	if e.m7review == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "评审服务暂时不可用", true)
	}
	rev, edgeID, err := e.m7review.SubmitReview(ctx, m7flow.Review{
		SubjectType: p.SubjectType, SubjectID: p.SubjectID, SubjectVersion: p.SubjectVersion,
		Verdict: p.Verdict, ReviewerID: p.ReviewerID, Reason: p.Reason,
	}, p.AuthorID)
	if err != nil {
		return m7EvidenceFailure(r, err, "review.submit")
	}
	return bridge.Success(r.ID, struct {
		ReviewID    string `json:"reviewId"`
		TraceEdgeID string `json:"traceEdgeId"`
	}{rev.ID, edgeID})
}

func handleDevTaskCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		StageRunID       string `json:"stageRunId"`
		Title            string `json:"title"`
		Priority         string `json:"priority"`
		Risk             string `json:"risk"`
		AcceptanceDigest string `json:"acceptanceDigest"`
		AssigneeID       string `json:"assigneeId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.StageRunID) ||
		len(p.Title) < 1 || len(p.Title) > 512 || !m7TaskPriority(p.Priority) || !m7TaskRisk(p.Risk) ||
		!m7TraceDigest(p.AcceptanceDigest) || len(p.AssigneeID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "devTask.create 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "任务服务暂时不可用", true)
	}
	task, err := e.m7trace.CreateDevTask(ctx, m7flow.DevTask{
		StageRunID: p.StageRunID, Title: p.Title, Priority: p.Priority, Risk: p.Risk,
		AcceptanceDigest: p.AcceptanceDigest, AssigneeID: p.AssigneeID,
	})
	if err != nil {
		return m7EvidenceFailure(r, err, "devTask.create")
	}
	return bridge.Success(r.ID, struct {
		TaskID      string `json:"taskId"`
		State       string `json:"state"`
		LockVersion int64  `json:"lockVersion"`
	}{task.ID, task.State, task.LockVersion})
}

// m7TaskPriority / m7TaskRisk guard the dev-task enum sets.
func m7TaskPriority(p string) bool {
	return p == m7flow.PriorityP0 || p == m7flow.PriorityP1 || p == m7flow.PriorityP2 || p == m7flow.PriorityP3
}

func m7TaskRisk(r string) bool {
	return r == m7flow.RiskLow || r == m7flow.RiskMedium || r == m7flow.RiskHigh
}

// m7TaskState reports whether s is one of the eight dev-task states.
func m7TaskState(s string) bool {
	switch s {
	case m7flow.TaskDraft, m7flow.TaskReady, m7flow.TaskInProgress, m7flow.TaskBlocked,
		m7flow.TaskInReview, m7flow.TaskDone, m7flow.TaskReopened, m7flow.TaskCancelled:
		return true
	}
	return false
}

func handleDevTaskTransition(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskID          string `json:"taskId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		To              string `json:"to"`
		Reason          string `json:"reason"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) || p.ExpectedVersion < 1 ||
		!m7TaskState(p.To) || p.To == m7flow.TaskDraft || len(p.Reason) > 2000 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "devTask.transition 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "任务服务暂时不可用", true)
	}
	task, err := e.m7trace.TransitionDevTask(ctx, p.TaskID, p.ExpectedVersion, p.To, p.Reason)
	if err != nil {
		return m7EvidenceFailure(r, err, "devTask.transition")
	}
	return bridge.Success(r.ID, struct {
		TaskID      string `json:"taskId"`
		State       string `json:"state"`
		LockVersion int64  `json:"lockVersion"`
	}{task.ID, task.State, task.LockVersion})
}

func handleEvidenceAttachTest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskRef      string `json:"taskRef"`
		Result       string `json:"result"`
		ReportDigest string `json:"reportDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.TaskRef) < 1 || len(p.TaskRef) > 64 ||
		!m7TestResult(p.Result) || !m7TraceDigest(p.ReportDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "evidence.attachTest 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "证据服务暂时不可用", true)
	}
	tr, err := e.m7trace.AttachTestRun(ctx, p.TaskRef, p.Result, p.ReportDigest)
	if err != nil {
		return m7EvidenceFailure(r, err, "evidence.attachTest")
	}
	return bridge.Success(r.ID, struct {
		TestID string `json:"testId"`
	}{tr.ID})
}

func m7TestResult(s string) bool {
	switch s {
	case "pass", "fail", "error", "timeout":
		return true
	}
	return false
}

func handleEvidenceAttachScan(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskRef      string `json:"taskRef"`
		Scanner      string `json:"scanner"`
		SeverityGate string `json:"severityGate"`
		ReportDigest string `json:"reportDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.TaskRef) < 1 || len(p.TaskRef) > 64 ||
		len(p.Scanner) < 1 || len(p.Scanner) > 128 || len(p.SeverityGate) < 1 || len(p.SeverityGate) > 32 ||
		!m7TraceDigest(p.ReportDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "evidence.attachScan 参数无效", false)
	}
	if e.m7trace == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "证据服务暂时不可用", true)
	}
	sr, err := e.m7trace.AttachScanRun(ctx, p.TaskRef, p.Scanner, p.SeverityGate, p.ReportDigest)
	if err != nil {
		return m7EvidenceFailure(r, err, "evidence.attachScan")
	}
	return bridge.Success(r.ID, struct {
		ScanID string `json:"scanId"`
	}{sr.ID})
}

// m7EvidenceFailure maps m7app/m7flow slice-2 errors onto the M7 wire family.
func m7EvidenceFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrEdgeNotFound), errors.Is(err, m7app.ErrStaleNotFound),
		errors.Is(err, m7app.ErrGateNotFound), errors.Is(err, m7app.ErrTaskNotFound),
		errors.Is(err, m7app.ErrEvidenceNotFound), errors.Is(err, m7app.ErrStageRunNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "证据对象不存在", false)
	case errors.Is(err, m7flow.ErrDanglingEdge):
		return bridge.Failure(r.ID, r.TraceID, "M7-TRC-001", "追踪端点不存在，拒绝悬空边", false)
	case errors.Is(err, m7flow.ErrStaleOutstanding):
		return bridge.Failure(r.ID, r.TraceID, "M7-TRC-002", "存在未清除的过期标记", false)
	case errors.Is(err, m7app.ErrDuplicateEdge):
		return bridge.Failure(r.ID, r.TraceID, "M7-TRC-003", "追踪边已存在且摘要冲突", false)
	case errors.Is(err, m7flow.ErrBadRelation), errors.Is(err, m7flow.ErrBadResolution),
		errors.Is(err, m7flow.ErrBadVerdict), errors.Is(err, m7app.ErrBadNodeRef),
		errors.Is(err, m7app.ErrBadDepth), errors.Is(err, m7app.ErrBadDirection):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", method+" 参数无效", false)
	case errors.Is(err, m7flow.ErrGateInputsIncomplete):
		return bridge.Failure(r.ID, r.TraceID, "M7-GAT-001", "门禁输入不完整", false)
	case errors.Is(err, m7flow.ErrCheckpointDenied):
		return bridge.Failure(r.ID, r.TraceID, "M7-CHK-001", "仅门禁通过且输入未变时可创建检查点", false)
	case errors.Is(err, m7flow.ErrSelfReview):
		return bridge.Failure(r.ID, r.TraceID, "M7-REV-001", "评审人与作者不得为同一人", false)
	case errors.Is(err, m7app.ErrTaskTransition):
		return bridge.Failure(r.ID, r.TraceID, "M7-TSK-001", "非法任务状态转换", false)
	case errors.Is(err, m7app.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "M7-WF-004", "乐观锁冲突，请刷新后重试", false)
	case errors.Is(err, m7app.ErrEvidenceServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "证据服务暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
