package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M8 slice-2/3/4 handlers (T-8.2.x/T-8.3.x/T-8.4.x/T-8.5.x):
// kb.upsertDocument / handoff.accept / tombstone.delete /
// automation.dispatch / sync.push.
//
// Error mapping follows the M8 wire contract (04 错误矩阵 M8-011~026).

func handleKBUpsertDocument(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CollectionID    string `json:"collectionId"`
		DocumentID      string `json:"documentId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		MediaType       string `json:"mediaType"`
		ContentRef      string `json:"contentRef"`
		SHA256          string `json:"sha256"`
		SourceLocator   string `json:"sourceLocator"`
		RequestID       string `json:"requestId"`
		Actor           string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CollectionID) ||
		!validCanonicalULID(p.DocumentID) || p.ExpectedVersion < 0 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "kb.upsertDocument 参数无效", false)
	}
	if e.m8kb == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	res, err := e.m8kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: p.CollectionID, DocumentID: p.DocumentID,
		ExpectedVersion: p.ExpectedVersion, MediaType: p.MediaType,
		ContentRef: p.ContentRef, SHA256: p.SHA256,
		SourceLocator: p.SourceLocator, RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleHandoffAccept(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		HandoffID string `json:"handoffId"`
		RequestID string `json:"requestId"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.HandoffID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "handoff.accept 参数无效", false)
	}
	if e.m8handoff == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "交接服务暂时不可用", true)
	}
	res, err := e.m8handoff.AcceptHandoff(ctx, m8app.HandoffAcceptInput{
		HandoffID: p.HandoffID, RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleTombstoneDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootRef            string `json:"rootRef"`
		ScopeID            string `json:"scopeId"`
		ConfirmationToken  string `json:"confirmationToken"`
		Actor              string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RootRef) < 1 || len(p.RootRef) > 128 ||
		len(p.ScopeID) < 1 || len(p.ScopeID) > 128 ||
		!m8core.ValidHexDigest(p.ConfirmationToken) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tombstone.delete 参数无效", false)
	}
	if e.m8handoff == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "墓碑服务暂时不可用", true)
	}
	res, err := e.m8handoff.DeleteWithTombstone(ctx, m8app.TombstoneDeleteInput{
		RootRef: p.RootRef, ScopeID: p.ScopeID,
		ConfirmationToken: p.ConfirmationToken, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleAutomationDispatch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		BundleID      string          `json:"bundleId"`
		BundleVersion int64           `json:"bundleVersion"`
		Trigger       json.RawMessage `json:"trigger"`
		Budget        json.RawMessage `json:"budget"`
		RequestID     string          `json:"requestId"`
		Actor         string          `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.BundleID) ||
		p.BundleVersion < 1 || len(p.Trigger) < 2 || len(p.Budget) < 2 ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "automation.dispatch 参数无效", false)
	}
	if e.m8automation == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "自动化服务暂时不可用", true)
	}
	res, err := e.m8automation.Dispatch(ctx, m8app.DispatchInput{
		BundleID: p.BundleID, BundleVersion: p.BundleVersion,
		Trigger: p.Trigger, Budget: p.Budget,
		RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

func handleSyncPush(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DeviceID    string                     `json:"deviceId"`
		VectorClock map[string]int64           `json:"vectorClock"`
		Edits       []m8core.SyncEdit          `json:"edits"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.DeviceID) ||
		p.VectorClock == nil || len(p.VectorClock) > m8core.MaxVectorClock ||
		len(p.Edits) > m8core.MaxSyncEdits {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "sync.push 参数无效", false)
	}
	if e.m8handoff == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "同步服务暂时不可用", true)
	}
	res, err := e.m8handoff.Push(ctx, m8app.SyncPushInput{
		DeviceID: p.DeviceID, VectorClock: p.VectorClock, Edits: p.Edits,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return bridge.Success(r.ID, res)
}

// m8SliceFailure maps the slice-2/3/4 error family onto the M8 code
// matrix (M8-011~026 plus the shared family).
func m8SliceFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrKBVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "M8-011", "并发重索引冲突，须新建版本", false)
	case errors.Is(err, m8app.ErrKBIndexFailed):
		return bridge.Failure(r.ID, r.TraceID, "M8-012", "索引失败，未产出可检索投影", true)
	case errors.Is(err, m8app.ErrHandoffRedacted):
		return bridge.Failure(r.ID, r.TraceID, "M8-013", "交接被裁剪后需重新确认", false)
	case errors.Is(err, m8app.ErrHandoffExpired):
		return bridge.Failure(r.ID, r.TraceID, "M8-014", "交接已过期", false)
	case errors.Is(err, m8app.ErrHandoffNotAccepted):
		return bridge.Failure(r.ID, r.TraceID, "M8-015", "交接未接受前不可读", false)
	case errors.Is(err, m8app.ErrTombstoneInProgress):
		return bridge.Failure(r.ID, r.TraceID, "M8-016", "墓碑传播进行中", false)
	case errors.Is(err, m8app.ErrTombstoneCascadeFailed):
		return bridge.Failure(r.ID, r.TraceID, "M8-017", "墓碑级联失败，维持不可读续跑", true)
	case errors.Is(err, m8app.ErrSyncVectorConflict):
		return bridge.Failure(r.ID, r.TraceID, "M8-018", "同叶并发冲突已进冲突箱", false)
	case errors.Is(err, m8app.ErrDeviceRevoked):
		return bridge.Failure(r.ID, r.TraceID, "M8-019", "设备已吊销", false)
	case errors.Is(err, m8app.ErrSyncAckStale):
		return bridge.Failure(r.ID, r.TraceID, "M8-020", "ACK 水位过旧", false)
	case errors.Is(err, m8app.ErrBundleChecksumInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M8-021", "Bundle 校验失败，零派发", false)
	case errors.Is(err, m8app.ErrBundlePermissionDenied):
		return bridge.Failure(r.ID, r.TraceID, "M8-022", "Bundle 权限拒绝，零派发", false)
	case errors.Is(err, m8app.ErrAutomationConfirmationRequired):
		return bridge.Failure(r.ID, r.TraceID, "M8-023", "高风险动作需即时确认", false)
	case errors.Is(err, m8app.ErrAutomationBudgetExceeded):
		return bridge.Failure(r.ID, r.TraceID, "M8-024", "预算超限", false)
	case errors.Is(err, m8app.ErrRunQuarantined):
		return bridge.Failure(r.ID, r.TraceID, "M8-026", "运行已隔离", false)
	case errors.Is(err, m8app.ErrTombstoneConfirmInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "墓碑确认令牌非法", false)
	case errors.Is(err, m8app.ErrHandoffNotFound),
		errors.Is(err, m8app.ErrKBCollectionNotFound),
		errors.Is(err, m8app.ErrBundleNotFound),
		errors.Is(err, m8app.ErrDeviceNotFound):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_NOT_FOUND", "资源不存在", false)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "载荷非法", false)
	case errors.Is(err, m8app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "服务暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", "M8 切片执行失败", false)
}
