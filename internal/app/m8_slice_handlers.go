package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "kb.upsertDocument 参数无效", false)
	}
	if e.m8kb == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "知识库服务暂时不可用", true)
	}
	res, err := e.m8kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: p.CollectionID, DocumentID: p.DocumentID,
		ExpectedVersion: p.ExpectedVersion, MediaType: p.MediaType,
		ContentRef: p.ContentRef, SHA256: p.SHA256,
		SourceLocator: p.SourceLocator, RequestID: p.RequestID, Actor: p.Actor,
		Projector: e.kbDocumentProjector,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

func handleHandoffAccept(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		HandoffID string `json:"handoffId"`
		RequestID string `json:"requestId"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.HandoffID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "handoff.accept 参数无效", false)
	}
	if e.m8handoff == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "交接服务暂时不可用", true)
	}
	res, err := e.m8handoff.AcceptHandoff(ctx, m8app.HandoffAcceptInput{
		HandoffID: p.HandoffID, RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

func handleTombstoneDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootRef           string `json:"rootRef"`
		ScopeID           string `json:"scopeId"`
		ConfirmationToken string `json:"confirmationToken"`
		Actor             string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RootRef) < 1 || len(p.RootRef) > 128 ||
		len(p.ScopeID) < 1 || len(p.ScopeID) > 128 ||
		!m8core.ValidHexDigest(p.ConfirmationToken) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tombstone.delete 参数无效", false)
	}
	if e.m8handoff == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "墓碑服务暂时不可用", true)
	}
	res, err := e.m8handoff.DeleteWithTombstone(ctx, m8app.TombstoneDeleteInput{
		RootRef: p.RootRef, ScopeID: p.ScopeID,
		ConfirmationToken: p.ConfirmationToken, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "automation.dispatch 参数无效", false)
	}
	if e.m8automation == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "自动化服务暂时不可用", true)
	}
	res, err := e.m8automation.Dispatch(ctx, m8app.DispatchInput{
		BundleID: p.BundleID, BundleVersion: p.BundleVersion,
		Trigger: p.Trigger, Budget: p.Budget,
		RequestID: p.RequestID, Actor: p.Actor,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

func handleSyncPush(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DeviceID    string            `json:"deviceId"`
		VectorClock map[string]int64  `json:"vectorClock"`
		Edits       []m8core.SyncEdit `json:"edits"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.DeviceID) ||
		p.VectorClock == nil || len(p.VectorClock) > m8core.MaxVectorClock ||
		len(p.Edits) > m8core.MaxSyncEdits {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "sync.push 参数无效", false)
	}
	if e.m8handoff == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "同步服务暂时不可用", true)
	}
	res, err := e.m8handoff.Push(ctx, m8app.SyncPushInput{
		DeviceID: p.DeviceID, VectorClock: p.VectorClock, Edits: p.Edits,
	})
	if err != nil {
		return m8SliceFailure(r, err)
	}
	return r.Ok(res)
}

// m8SliceFailure maps the slice-2/3/4 error family onto the M8 code
// matrix (M8-011~026 plus the shared family).
func m8SliceFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrKBVersionConflict):
		return r.Fail("M8-011", "并发重索引冲突，须新建版本", false)
	case errors.Is(err, m8app.ErrKBIndexFailed):
		return r.Fail("M8-012", kbIndexFailMessage(err), true)
	case errors.Is(err, m8app.ErrHandoffRedacted):
		return r.Fail("M8-013", "交接被裁剪后需重新确认", false)
	case errors.Is(err, m8app.ErrHandoffExpired):
		return r.Fail("M8-014", "交接已过期", false)
	case errors.Is(err, m8app.ErrHandoffNotAccepted):
		return r.Fail("M8-015", "交接未接受前不可读", false)
	case errors.Is(err, m8app.ErrTombstoneInProgress):
		return r.Fail("M8-016", "墓碑传播进行中", false)
	case errors.Is(err, m8app.ErrTombstoneCascadeFailed):
		return r.Fail("M8-017", "墓碑级联失败，维持不可读续跑", true)
	case errors.Is(err, m8app.ErrSyncVectorConflict):
		return r.Fail("M8-018", "同叶并发冲突已进冲突箱", false)
	case errors.Is(err, m8app.ErrDeviceRevoked):
		return r.Fail("M8-019", "设备已吊销", false)
	case errors.Is(err, m8app.ErrSyncAckStale):
		return r.Fail("M8-020", "ACK 水位过旧", false)
	case errors.Is(err, m8app.ErrBundleChecksumInvalid):
		return r.Fail("M8-021", "Bundle 校验失败，零派发", false)
	case errors.Is(err, m8app.ErrBundlePermissionDenied):
		return r.Fail("M8-022", "Bundle 权限拒绝，零派发", false)
	case errors.Is(err, m8app.ErrAutomationConfirmationRequired):
		return r.Fail("M8-023", "高风险动作需即时确认", false)
	case errors.Is(err, m8app.ErrAutomationIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_KEY_CONFLICT", "请求标识已用于其他输入，请核对后发起新请求", false)
	case errors.Is(err, m8app.ErrAutomationBudgetExceeded):
		return r.Fail("M8-024", "预算超限", false)
	case errors.Is(err, m8app.ErrRunQuarantined):
		return r.Fail("M8-026", "运行已隔离", false)
	case errors.Is(err, m8app.ErrTombstoneConfirmInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "墓碑确认令牌非法", false)
	case errors.Is(err, m8app.ErrHandoffNotFound),
		errors.Is(err, m8app.ErrKBCollectionNotFound),
		errors.Is(err, m8app.ErrBundleNotFound),
		errors.Is(err, m8app.ErrDeviceNotFound):
		return r.Fail("BRIDGE_NOT_FOUND", "资源不存在", false)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "载荷非法", false)
	case errors.Is(err, m8app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "服务暂时不可用", true)
	}
	return r.Fail("INTERNAL_ERROR", "M8 切片执行失败", false)
}

func kbIndexFailMessage(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = strings.TrimSpace(msg[i+2:])
	}
	msg = localizeKBIndexSuffix(msg)
	if msg == "" || msg == m8app.ErrKBIndexFailed.Error() {
		return "无法抽出正文：索引失败，未产出可检索投影"
	}
	return "无法抽出正文：" + msg
}

func localizeKBIndexSuffix(msg string) string {
	switch {
	case msg == "content_ref must be an absolute path" || strings.Contains(msg, "content_ref must"):
		return "内容路径必须是绝对路径"
	case msg == "source digest changed" || strings.Contains(msg, "source changed during parsing"):
		return "源文件在入库后已被修改"
	case msg == "no non-empty chunks":
		return "没有可检索的正文"
	case strings.HasPrefix(msg, "chunk count ") && strings.Contains(msg, "exceeds cap"):
		return "分块数量超过上限"
	case msg == "parse function not configured":
		return "未配置正文解析"
	case msg == "tombstone:deleted" || strings.Contains(msg, "tombstone:deleted"):
		return "知识来源已删除"
	case msg == "empty body":
		return "没有可检索的正文"
	case msg == "chunk body exceeds budget":
		return "分块正文超过上限"
	case strings.Contains(msg, "document parser is busy"):
		return "文档解析正忙，请稍后重试"
	case strings.Contains(msg, "document parser exceeded"):
		return "文档解析超过上限或已中止"
	case strings.Contains(msg, "local drive file") || strings.Contains(msg, "document source must"):
		return "文档必须是本地磁盘文件"
	case strings.Contains(msg, "unavailable document drive") || strings.Contains(msg, "network or unavailable"):
		return "不支持网络盘或不可用的文档磁盘"
	case strings.Contains(msg, "parsing budget exceeded"):
		return "文档超过解析上限"
	default:
		return msg
	}
}

func localizeStoredKBFailReason(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	const prefix = "无法抽出正文："
	if strings.HasPrefix(msg, prefix) {
		return prefix + localizeKBIndexSuffix(strings.TrimPrefix(msg, prefix))
	}
	return localizeKBIndexSuffix(msg)
}
