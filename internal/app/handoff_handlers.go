package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/lunitide/lunitide/internal/handoffapp"
)

// handoffCapsuleDTO is the JSON-serializable view of a handoff capsule.
// Sensitive internal fields (active_tasks_json, recent_message_ids) are not
// exposed to the Renderer; only summary-bearing fields needed for display
// and provenance are included.
type handoffCapsuleDTO struct {
	CapsuleID      string     `json:"capsuleId"`
	SourceSessionID string    `json:"sourceSessionId"`
	CheckpointID   string     `json:"checkpointId"`
	Status         string     `json:"status"`
	Digest         string     `json:"digest"`
	CreatedAt      time.Time  `json:"createdAt"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
}

func newHandoffCapsuleDTO(c handoff.Capsule) handoffCapsuleDTO {
	return handoffCapsuleDTO{
		CapsuleID:       c.ID,
		SourceSessionID: c.SourceSessionID,
		CheckpointID:    c.CheckpointID,
		Status:          string(c.Status),
		Digest:          c.Digest,
		CreatedAt:       c.CreatedAt,
		ExpiresAt:       c.ExpiresAt,
	}
}

// handleContextHandoffCreate creates a cross-window handoff capsule from a
// succeeded compaction checkpoint (ADR-005 §5). The capsule carries the
// checkpoint's structured summary plus active task state for cross-window
// continuation.
func handleContextHandoffCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SourceSessionID  string   `json:"sourceSessionId"`
		CheckpointID     string   `json:"checkpointId"`
		RecentMessageIDs []string `json:"recentMessageIds"`
		ActiveTasksJSON  string   `json:"activeTasksJson"`
		ExpiresAt        *time.Time `json:"expiresAt"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SourceSessionID) || !validCanonicalULID(p.CheckpointID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.create 参数无效", false)
	}
	capsule, err := e.CreateHandoffCapsule(ctx, handoffapp.CreateCapsuleRequest{
		SourceSessionID:  p.SourceSessionID,
		CheckpointID:     p.CheckpointID,
		RecentMessageIDs: p.RecentMessageIDs,
		ActiveTasksJSON:  p.ActiveTasksJSON,
		ExpiresAt:        p.ExpiresAt,
	})
	if err != nil {
		return handoffFailure(r, err)
	}
	dto := newHandoffCapsuleDTO(capsule)
	return bridge.Success(r.ID, map[string]any{
		"capsuleId":      dto.CapsuleID,
		"sourceSessionId": dto.SourceSessionID,
		"checkpointId":   dto.CheckpointID,
		"status":         dto.Status,
		"digest":         dto.Digest,
		"createdAt":      dto.CreatedAt,
		"expiresAt":      dto.ExpiresAt,
	})
}

// handleContextHandoffInspect returns a capsule by ID together with its source
// checkpoint summary (ADR-005 §5: "allows the user to inspect the summary and
// jump to source Messages").
func handleContextHandoffInspect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CapsuleID string `json:"capsuleId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CapsuleID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.inspect 参数无效", false)
	}
	result, err := e.InspectHandoffCapsule(ctx, p.CapsuleID)
	if err != nil {
		return handoffFailure(r, err)
	}
	dto := newHandoffCapsuleDTO(result.Capsule)
	resp := map[string]any{
		"capsuleId":       dto.CapsuleID,
		"sourceSessionId": dto.SourceSessionID,
		"checkpointId":    dto.CheckpointID,
		"status":          dto.Status,
		"digest":          dto.Digest,
		"createdAt":       dto.CreatedAt,
		"expiresAt":       dto.ExpiresAt,
		"sourceDeleted":   result.Checkpoint == nil,
	}
	if result.Checkpoint != nil {
		resp["summaryPreview"] = result.Checkpoint.SummaryJSON
		resp["humanSummary"] = result.Checkpoint.HumanSummary
		resp["sourceStartSeq"] = result.Checkpoint.SourceStartSeq
		resp["sourceEndSeq"] = result.Checkpoint.SourceEndSeq
	}
	return bridge.Success(r.ID, resp)
}

// handleContextHandoffImport imports a capsule into a target session as
// provenance-linked untrusted prior context (ADR-005 §5). Repeat import of the
// same capsule into the same session is idempotent.
func handleContextHandoffImport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CapsuleID      string `json:"capsuleId"`
		TargetSessionID string `json:"targetSessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CapsuleID) || !validCanonicalULID(p.TargetSessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.import 参数无效", false)
	}
	result, err := e.ImportHandoffCapsule(ctx, p.CapsuleID, p.TargetSessionID)
	if err != nil {
		return handoffFailure(r, err)
	}
	dto := newHandoffCapsuleDTO(result.Capsule)
	resp := map[string]any{
		"capsuleId":       dto.CapsuleID,
		"sourceSessionId": dto.SourceSessionID,
		"checkpointId":    dto.CheckpointID,
		"status":          dto.Status,
		"digestValid":     result.DigestValid,
		"expiredCheck":    result.ExpiredCheck,
		"alreadyImported": result.AlreadyImported,
		"importedAt":      result.ImportedAt,
	}
	if result.Checkpoint != nil {
		resp["summaryPreview"] = result.Checkpoint.SummaryJSON
		resp["humanSummary"] = result.Checkpoint.HumanSummary
	}
	return bridge.Success(r.ID, resp)
}

// handleContextHandoffRevoke revokes an active capsule. Once revoked, a
// capsule cannot be imported or activated (terminal state).
func handleContextHandoffRevoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CapsuleID string `json:"capsuleId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CapsuleID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.revoke 参数无效", false)
	}
	if err := e.RevokeHandoffCapsule(ctx, p.CapsuleID); err != nil {
		return handoffFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{
		"capsuleId": p.CapsuleID,
		"status":    string(handoff.StatusRevoked),
		"revoked":   true,
	})
}

// handleContextHandoffList returns capsules for a source session, ordered by
// creation time descending (ADR-005 §5).
func handleContextHandoffList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SourceSessionID string `json:"sourceSessionId"`
		Limit           int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SourceSessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.list 参数无效", false)
	}
	capsules, err := e.ListHandoffCapsules(ctx, p.SourceSessionID, p.Limit)
	if err != nil {
		return handoffFailure(r, err)
	}
	items := make([]handoffCapsuleDTO, 0, len(capsules))
	for _, c := range capsules {
		items = append(items, newHandoffCapsuleDTO(c))
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

// handleContextHandoffListImports returns all capsules imported into the
// target session, ordered by imported_at descending (ADR-005 §5).
func handleContextHandoffListImports(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TargetSessionID string `json:"targetSessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TargetSessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "context.handoff.list-imports 参数无效", false)
	}
	capsules, err := e.ListImportedHandoffCapsules(ctx, p.TargetSessionID)
	if err != nil {
		return handoffFailure(r, err)
	}
	items := make([]handoffCapsuleDTO, 0, len(capsules))
	for _, c := range capsules {
		items = append(items, newHandoffCapsuleDTO(c))
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

// handoffFailure maps handoff service errors to stable Bridge error codes.
func handoffFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, handoffapp.ErrCapsuleNotFound):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_CAPSULE_NOT_FOUND", "胶囊不存在", false)
	case errors.Is(err, handoffapp.ErrCapsuleNotActive):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_CAPSULE_NOT_ACTIVE", "胶囊不在 active 状态", false)
	case errors.Is(err, handoffapp.ErrCapsuleExpired):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_CAPSULE_EXPIRED", "胶囊已过期", false)
	case errors.Is(err, handoffapp.ErrCheckpointNotFound):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_CHECKPOINT_NOT_FOUND", "源检查点不存在", false)
	case errors.Is(err, handoffapp.ErrCheckpointNotSucceeded):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_CHECKPOINT_NOT_SUCCEEDED", "源检查点不在 succeeded 状态", false)
	case errors.Is(err, handoffapp.ErrSourceDeleted):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_SOURCE_DELETED", "胶囊源已被删除", false)
	case errors.Is(err, handoffapp.ErrDigestMismatch):
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_DIGEST_MISMATCH", "胶囊摘要校验失败", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "HANDOFF_OPERATION_FAILED", err.Error(), true)
	}
}
