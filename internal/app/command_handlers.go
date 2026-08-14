package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// M4-F handlers: command.start/get/cancel.
// command.start launches a product-signed CommandSpec template inside a
// fenced write workspace; command.get is read-only; command.cancel kills the
// process tree and transitions the job to cancelled (first-terminal-wins).

type commandJobDTO struct {
	ID                string             `json:"id"`
	RunID             string             `json:"runId"`
	CommandSpecDigest string             `json:"commandSpecDigest"`
	Status            agentrun.JobStatus `json:"status"`
	ExitCode          *int64             `json:"exitCode,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
}

func newCommandJobDTO(j agentrun.CommandJob) commandJobDTO {
	d := commandJobDTO{
		ID:                j.ID,
		RunID:             j.RunID,
		CommandSpecDigest: j.CommandSpecDigest,
		Status:            j.Status,
		CreatedAt:         j.CreatedAt,
		UpdatedAt:         j.UpdatedAt,
	}
	if j.ExitCode != nil {
		d.ExitCode = j.ExitCode
	}
	return d
}

func commandFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, agentrunapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, agentrun.ErrTerminal):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_JOB_TERMINAL", "命令任务已终结", false)
	case errors.Is(err, agentrun.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_JOB_TRANSITION_INVALID", "命令任务状态不允许该操作", false)
	case errors.Is(err, agentrun.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_JOB_NOT_FOUND", "命令任务不存在", false)
	case errors.Is(err, agentrun.ErrReviewDigestMismatch):
		return bridge.Failure(r.ID, r.TraceID, "REVIEW_DIGEST_MISMATCH", "审批未绑定当前最终命令规格", false)
	case errors.Is(err, agentrun.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "命令参数无效", false)
	case errors.Is(err, agentrun.ErrReviewRequired):
		return bridge.Failure(r.ID, r.TraceID, "REVIEW_REQUIRED", "需要未消费的持久审批", false)
	case errors.Is(err, agentrunapp.ErrCommandTemplateUnknown):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_TEMPLATE_UNKNOWN", "命令模板不存在或版本不匹配", false)
	case errors.Is(err, agentrunapp.ErrCommandSpecMismatch):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_SPEC_MISMATCH", "命令摘要不匹配，拒绝执行", false)
	case errors.Is(err, agentrunapp.ErrCommandTargetInvalid):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_TARGET_INVALID", "命令目标参数无效", false)
	case errors.Is(err, agentrunapp.ErrCommandNotRunnable):
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_NOT_RUNNABLE", "命令可执行文件在当前环境不可解析", false)
	case errors.Is(err, agentrunapp.ErrFsLeaseInvalid):
		return bridge.Failure(r.ID, r.TraceID, "FS_LEASE_INVALID", "工作区租约不存在或已过期", false)
	case errors.Is(err, agentrunapp.ErrFsFencingStale):
		return bridge.Failure(r.ID, r.TraceID, "FS_FENCING_STALE", "租约 fencing token 已失效，请重新获取租约", false)
	case errors.Is(err, agentrunapp.ErrFsScopeDenied):
		return bridge.Failure(r.ID, r.TraceID, "FS_SCOPE_DENIED", "路径不在工作区授权范围内", false)
	case errors.Is(err, agentrunapp.ErrFsNotFound):
		return bridge.Failure(r.ID, r.TraceID, "FS_NOT_FOUND", "工作目录不存在", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令任务数据暂时不可用", true)
	}
}

func handleCommandReviewRequest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID           string `json:"runId"`
		LeaseID         string `json:"leaseId"`
		FencingToken    int64  `json:"fencingToken"`
		TemplateID      string `json:"templateId"`
		TemplateVersion string `json:"templateVersion"`
		Target          string `json:"target,omitempty"`
		WorkDir         string `json:"workDir,omitempty"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || !validFsLease(p.LeaseID, p.FencingToken) || p.TemplateID == "" || p.TemplateVersion == "" || (p.Target != "" && !validCommandTarget(p.Target)) || len(p.WorkDir) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.review.request 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令任务数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.CommandReviewRequest(ctx, r.IdempotencyKey, agentRunMutationActor, p, agentrunapp.CommandStartInput{RunID: p.RunID, LeaseID: p.LeaseID, FencingToken: p.FencingToken, TemplateID: p.TemplateID, TemplateVersion: p.TemplateVersion, Target: p.Target, WorkDir: p.WorkDir})
	if err != nil {
		return commandFailure(r, err)
	}
	return bridge.Success(r.ID, result)
}

func handleCommandStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID              string `json:"runId"`
		LeaseID            string `json:"leaseId"`
		FencingToken       int64  `json:"fencingToken"`
		TemplateID         string `json:"templateId"`
		TemplateVersion    string `json:"templateVersion"`
		Target             string `json:"target,omitempty"`
		WorkDir            string `json:"workDir,omitempty"`
		ExpectedSpecDigest string `json:"expectedSpecDigest,omitempty"`
		ApprovalDigest     string `json:"approvalDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || !validFsLease(p.LeaseID, p.FencingToken) ||
		p.TemplateID == "" || len(p.TemplateID) > 64 || p.TemplateVersion == "" || len(p.TemplateVersion) > 32 || !validLowerHexDigest(p.ApprovalDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.start 参数无效", false)
	}
	if p.Target != "" && (len(p.Target) > 128 || !validCommandTarget(p.Target)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.start 参数无效", false)
	}
	if p.WorkDir != "" && len(p.WorkDir) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.start 参数无效", false)
	}
	if !validLowerHexDigest(p.ExpectedSpecDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.start 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令任务数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	job, err := e.agentRuns.CommandStart(ctx, r.IdempotencyKey, agentRunMutationActor, p, agentrunapp.CommandStartInput{
		RunID:              p.RunID,
		LeaseID:            p.LeaseID,
		FencingToken:       p.FencingToken,
		TemplateID:         p.TemplateID,
		TemplateVersion:    p.TemplateVersion,
		Target:             p.Target,
		WorkDir:            p.WorkDir,
		ExpectedSpecDigest: p.ExpectedSpecDigest,
		ApprovalDigest:     p.ApprovalDigest,
	})
	if err != nil {
		return commandFailure(r, err)
	}
	return bridge.Success(r.ID, newCommandJobDTO(job))
}

func handleCommandGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		JobID string `json:"jobId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.JobID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.get 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令任务数据暂时不可用", true)
	}
	job, err := e.agentRuns.CommandGet(ctx, p.JobID)
	if err != nil {
		return commandFailure(r, err)
	}
	return bridge.Success(r.ID, newCommandJobDTO(job))
}

func handleCommandCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		JobID string `json:"jobId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.JobID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "command.cancel 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令任务数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	job, err := e.agentRuns.CommandCancel(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.JobID)
	if err != nil {
		return commandFailure(r, err)
	}
	return bridge.Success(r.ID, newCommandJobDTO(job))
}

// validCommandTarget mirrors the agentrunapp rule (charset + no escape).
func validCommandTarget(target string) bool {
	if len(target) < 1 || len(target) > 128 {
		return false
	}
	for i := 0; i < len(target); i++ {
		c := target[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_', c == '.', c == '/', c == '=', c == '-':
		default:
			return false
		}
	}
	if target[0] == '-' || target[0] == '/' {
		return false
	}
	// "..." is the Go package wildcard and is allowed; any remaining ".." is
	// a parent-directory escape.
	stripped := ""
	for i := 0; i < len(target); i++ {
		if i+2 < len(target) && target[i] == '.' && target[i+1] == '.' && target[i+2] == '.' {
			i += 2
			continue
		}
		stripped += string(target[i])
	}
	for i := 0; i+1 < len(stripped); i++ {
		if stripped[i] == '.' && stripped[i+1] == '.' {
			return false
		}
	}
	return true
}
