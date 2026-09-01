package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

// handleContextStatus returns the current context state for a session,
// including token counts, context window, active checkpoint version,
// budget usage, and compaction status (ADR-005 §4.2).
func handleContextStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "context.status 参数无效", false)
	}
	result, err := e.ContextStatus(ctx, p.SessionID)
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "上下文状态暂时不可用", true)
	}
	return r.Ok(result)
}

// handleContextCompactPreview generates a draft compaction checkpoint and
// returns the summary preview. The provider/model/contextWindow are derived
// from the latest checkpoint or the provider list (ADR-005 §4.2).
func handleContextCompactPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "context.compact.preview 参数无效", false)
	}

	// Derive provider, model, and contextWindow for the preview.
	providerID, modelID, contextWindow := deriveCompactionContext(e, ctx, p.SessionID)
	if providerID == "" || modelID == "" || contextWindow == 0 {
		return r.Fail("COMPACTION_CONTEXT_REQUIRED", "无法确定压缩上下文，需要先配置供应商和模型", false)
	}

	result, err := e.CompactPreview(ctx, p.SessionID, providerID, modelID, "", contextWindow)
	if err != nil {
		return compactionFailure(r, "COMPACTION_PREVIEW_FAILED", "压缩预览暂时不可用", err)
	}
	return r.Ok(map[string]any{
		"checkpointId":   result.CheckpointID,
		"version":        result.Version,
		"sourceStartSeq": result.SourceStartSeq,
		"sourceEndSeq":   result.SourceEndSeq,
		"sourceDigest":   result.SourceDigest,
		"summaryPreview": result.SummaryPreview,
		"humanSummary":   result.HumanSummary,
		"status":         result.Status,
	})
}

// handleContextCompactCommit activates a previewed checkpoint using CAS on
// baseVersion (ADR-005 §4.2).
func handleContextCompactCommit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CheckpointID string `json:"checkpointId"`
		BaseVersion  int64  `json:"baseVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CheckpointID) || p.BaseVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "context.compact.commit 参数无效", false)
	}
	result, err := e.CompactCommit(ctx, p.CheckpointID, p.BaseVersion)
	if err != nil {
		if errors.Is(err, compactionapp.ErrVersionConflict) {
			return r.Fail("COMPACTION_VERSION_CONFLICT", "baseVersion 与检查点版本不匹配", false)
		}
		if errors.Is(err, compactionapp.ErrCheckpointNotFound) {
			return r.Fail("COMPACTION_CHECKPOINT_NOT_FOUND", "检查点不存在", false)
		}
		if errors.Is(err, compactionapp.ErrCheckpointNotSucceeded) {
			return r.Fail("COMPACTION_CHECKPOINT_NOT_SUCCEEDED", "检查点不在 succeeded 状态", false)
		}
		return compactionFailure(r, "COMPACTION_COMMIT_FAILED", "压缩提交暂时不可用", err)
	}
	return r.Ok(map[string]any{
		"checkpointId": result.CheckpointID,
		"version":      result.Version,
		"status":       result.Status,
		"activated":    result.Activated,
	})
}

// handleContextCompactCancel cancels a pending compaction checkpoint
// (ADR-005 §4.2).
func handleContextCompactCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CheckpointID string `json:"checkpointId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CheckpointID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "context.compact.cancel 参数无效", false)
	}
	result, err := e.CompactCancel(ctx, p.CheckpointID)
	if err != nil {
		if errors.Is(err, compactionapp.ErrCheckpointNotFound) {
			return r.Fail("COMPACTION_CHECKPOINT_NOT_FOUND", "检查点不存在", false)
		}
		if errors.Is(err, compactionapp.ErrCheckpointNotPending) {
			return r.Fail("COMPACTION_CHECKPOINT_NOT_PENDING", "只能取消 pending 状态的检查点", false)
		}
		return compactionFailure(r, "COMPACTION_CANCEL_FAILED", "压缩取消暂时不可用", err)
	}
	return r.Ok(map[string]any{
		"checkpointId": result.CheckpointID,
		"status":       result.Status,
		"cancelled":    result.Cancelled,
	})
}

func compactionFailure(r bridge.Request, defaultCode, defaultMessage string, err error) bridge.Response {
	switch {
	case errors.Is(err, compactionapp.ErrCheckpointNotFound):
		return r.Fail("COMPACTION_CHECKPOINT_NOT_FOUND", "检查点不存在", false)
	case errors.Is(err, compactionapp.ErrCheckpointNotPending):
		return r.Fail("COMPACTION_CHECKPOINT_NOT_PENDING", "检查点不在 pending 状态", false)
	case errors.Is(err, compactionapp.ErrCheckpointNotSucceeded):
		return r.Fail("COMPACTION_CHECKPOINT_NOT_SUCCEEDED", "检查点不在 succeeded 状态", false)
	case errors.Is(err, compactionapp.ErrVersionConflict), errors.Is(err, compactionapp.ErrConcurrentModification):
		return r.Fail("COMPACTION_VERSION_CONFLICT", "检查点已被修改，请刷新后重试", false)
	case errors.Is(err, compactionapp.ErrNoMessagesToSummarize):
		return r.Fail("COMPACTION_SOURCE_EMPTY", "没有可压缩的消息", false)
	default:
		return internalBridgeFailure(r, defaultCode, defaultMessage, true, err)
	}
}

// deriveCompactionContext determines the provider, model, and context window
// for compaction operations. It first checks the latest checkpoint for the
// session, then falls back to the first available provider.
func deriveCompactionContext(e *Engine, ctx context.Context, sessionID string) (providerID, modelID string, contextWindow int64) {
	// Try to get provider/model from the latest checkpoint.
	if e.compactionTrigger != nil {
		latest, err := e.compactionTrigger.GetLatestCheckpoint(ctx, sessionID)
		if err == nil && latest != nil {
			providerID = latest.Provider
			modelID = latest.Model
		}
	}

	if e.providers == nil {
		return
	}
	providers, err := e.providers.List(ctx, provider.Filter{})
	if err != nil || len(providers) == 0 {
		return
	}
	// Resolve current IDs first, then legacy IDs, then old protocol values only
	// when the provider/model combination is unambiguous.
	var candidates []provider.Provider
	for _, p := range providers {
		if p.ID == providerID || (p.LegacyID != "" && p.LegacyID == providerID) {
			candidates = []provider.Provider{p}
			break
		}
	}
	if len(candidates) == 0 && providerID != "" {
		for _, p := range providers {
			if string(p.Protocol) == providerID {
				for _, m := range p.Models {
					if modelID == "" || m.ModelID == modelID {
						candidates = append(candidates, p)
						break
					}
				}
			}
		}
	}
	if len(candidates) != 1 {
		// An unresolved or ambiguous historical identity must not poison the
		// fallback. Prefer the first configured provider as documented.
		candidates = []provider.Provider{providers[0]}
	}
	selected := candidates[0]
	providerID = selected.ID
	for _, m := range selected.Models {
		if (modelID == "" || m.ModelID == modelID) && m.ContextWindow > 0 {
			modelID = m.ModelID
			contextWindow = m.ContextWindow
			return
		}
	}
	// The historical model no longer exists; fall back to the first usable
	// model on the resolved provider rather than returning a half-resolved tuple.
	for _, m := range selected.Models {
		if m.ContextWindow > 0 {
			modelID = m.ModelID
			contextWindow = m.ContextWindow
			return
		}
	}

	return
}
