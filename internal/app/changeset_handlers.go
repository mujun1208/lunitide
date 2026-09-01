package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// M4-E handlers: changeset.preview/apply/revert. Change sets are digest-bound
// file mutations authorized by a fenced write lease; approval and revert
// bind the exact approval digest captured at preview time, and a base-state
// CAS failure marks the set conflicted instead of applying onto drift.

type changeSetDTO struct {
	ID             string                   `json:"id"`
	RunID          string                   `json:"runId"`
	BaseDigest     string                   `json:"baseDigest"`
	ApprovalDigest string                   `json:"approvalDigest"`
	Status         agentrun.ChangeSetStatus `json:"status"`
	Version        int64                    `json:"version"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
}

func newChangeSetDTO(v agentrun.ChangeSet) changeSetDTO {
	return changeSetDTO{
		ID:             v.ID,
		RunID:          v.RunID,
		BaseDigest:     v.BaseDigest,
		ApprovalDigest: v.ApprovalDigest,
		Status:         v.Status,
		Version:        v.Version,
		CreatedAt:      v.CreatedAt,
		UpdatedAt:      v.UpdatedAt,
	}
}

func changesetFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrIdempotencyKeyRequired):
		return r.Fail("IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, agentrunapp.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, agentrun.ErrNotFound):
		return r.Fail("CHANGESET_NOT_FOUND", "变更集不存在", false)
	case errors.Is(err, agentrun.ErrVersionConflict):
		return r.Fail("CHANGESET_VERSION_CONFLICT", "变更集版本已变化，请读取新版本后重试", false)
	case errors.Is(err, agentrun.ErrTerminal):
		return r.Fail("CHANGESET_TERMINAL", "变更集已终结", false)
	case errors.Is(err, agentrun.ErrInvalidTransition):
		return r.Fail("CHANGESET_TRANSITION_INVALID", "变更集或运行状态不允许该操作", false)
	case errors.Is(err, agentrun.ErrReviewDigestMismatch):
		return r.Fail("REVIEW_DIGEST_MISMATCH", "审批摘要与变更集摘要不一致", false)
	case errors.Is(err, agentrun.ErrReviewRequired):
		return r.Fail("REVIEW_REQUIRED", "需要未消费的持久审批", false)
	case errors.Is(err, agentrun.ErrChangeSetBaseConflict):
		return r.Fail("CHANGESET_BASE_CONFLICT", "工作区基线已变化，变更集已标记冲突，请重新预览", false)
	case errors.Is(err, agentrunapp.ErrChangeSetApplyFailed):
		return r.Fail("CHANGESET_APPLY_FAILED", "文件写入失败，已尽力回滚并将变更集标记冲突", false)
	case errors.Is(err, agentrunapp.ErrFsLeaseInvalid):
		return r.Fail("FS_LEASE_INVALID", "工作区租约不存在或已过期", false)
	case errors.Is(err, agentrunapp.ErrFsFencingStale):
		return r.Fail("FS_FENCING_STALE", "租约 fencing token 已失效，请重新获取租约", false)
	case errors.Is(err, agentrunapp.ErrFsScopeDenied):
		return r.Fail("FS_SCOPE_DENIED", "路径不在工作区授权范围内", false)
	case errors.Is(err, agentrunapp.ErrFsPathInvalid):
		return r.Fail("FS_PATH_INVALID", "路径无效", false)
	case errors.Is(err, agentrunapp.ErrFsNotFound):
		return r.Fail("FS_NOT_FOUND", "路径不存在", false)
	case errors.Is(err, agentrunapp.ErrFsPathExists):
		return r.Fail("FS_PATH_EXISTS", "目标路径已存在", false)
	case errors.Is(err, agentrunapp.ErrFsNotAFile):
		return r.Fail("FS_NOT_A_FILE", "目标不是常规文件", false)
	case errors.Is(err, agentrunapp.ErrFsBinary):
		return r.Fail("FS_BINARY", "文件不是 UTF-8 文本", false)
	case errors.Is(err, agentrunapp.ErrFsTooLarge):
		return r.Fail("FS_TOO_LARGE", "文件超出变更集可处理大小上限", false)
	case errors.Is(err, agentrun.ErrInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "变更集参数无效", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "变更集数据暂时不可用", true)
	}
}

func handleChangesetPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID        string `json:"runId"`
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Ops          []struct {
			Op      string `json:"op"`
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"ops"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || !validFsLease(p.LeaseID, p.FencingToken) ||
		len(p.Ops) < 1 || len(p.Ops) > 64 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.preview 参数无效", false)
	}
	ops := make([]agentrunapp.ChangeSetPreviewOp, len(p.Ops))
	for i, op := range p.Ops {
		switch op.Op {
		case "create", "update":
			if len(op.Content) > 1048576 {
				return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.preview 参数无效", false)
			}
		case "delete":
			if op.Content != "" {
				return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.preview 参数无效", false)
			}
		default:
			return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.preview 参数无效", false)
		}
		if op.Path == "" || len(op.Path) > 512 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.preview 参数无效", false)
		}
		ops[i] = agentrunapp.ChangeSetPreviewOp{Op: op.Op, Path: op.Path, Content: op.Content}
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "变更集数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.ChangesetPreview(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.RunID, p.LeaseID, p.FencingToken, ops)
	if err != nil {
		return changesetFailure(r, err)
	}
	return r.Ok(struct {
		ChangeSet  changeSetDTO                        `json:"changeSet"`
		Operations []agentrunapp.ChangeSetOpProjection `json:"operations"`
	}{newChangeSetDTO(result.ChangeSet), result.Operations})
}

// changesetMutationPayload is the shared apply/revert payload: the set under
// mutation, its expected version, the approval digest binding the reviewed
// plan, and the fenced write lease.
type changesetMutationPayload struct {
	ChangeSetID     string `json:"changeSetId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	ApprovalDigest  string `json:"approvalDigest"`
	LeaseID         string `json:"leaseId"`
	FencingToken    int64  `json:"fencingToken"`
}

func validChangesetMutation(p changesetMutationPayload) bool {
	return validCanonicalULID(p.ChangeSetID) && p.ExpectedVersion >= 1 &&
		validLowerHexDigest(p.ApprovalDigest) && validFsLease(p.LeaseID, p.FencingToken)
}

func handleChangesetApply(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p changesetMutationPayload
	if decodePayload(r.Payload, &p) != nil || !validChangesetMutation(p) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.apply 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "变更集数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.ChangesetApply(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.ChangeSetID, p.ExpectedVersion, p.ApprovalDigest, p.LeaseID, p.FencingToken)
	if err != nil {
		return changesetFailure(r, err)
	}
	return r.Ok(struct {
		ChangeSet  changeSetDTO `json:"changeSet"`
		AppliedOps int          `json:"appliedOps"`
	}{newChangeSetDTO(result.ChangeSet), result.AppliedOps})
}

func handleChangesetRevert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p changesetMutationPayload
	if decodePayload(r.Payload, &p) != nil || !validChangesetMutation(p) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "changeset.revert 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "变更集数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.ChangesetRevert(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.ChangeSetID, p.ExpectedVersion, p.ApprovalDigest, p.LeaseID, p.FencingToken)
	if err != nil {
		return changesetFailure(r, err)
	}
	return r.Ok(struct {
		ChangeSet   changeSetDTO `json:"changeSet"`
		RevertedOps int          `json:"revertedOps"`
	}{newChangeSetDTO(result.ChangeSet), result.RevertedOps})
}
