package app

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/media"
	"github.com/lunitide/lunitide/internal/mediaapp"
	"github.com/oklog/ulid/v2"
)

func mediaUnavailable(r bridge.Request, method string) bridge.Response {
	return r.Fail("STORAGE_UNAVAILABLE", method+" 暂不可用", true)
}

func failMedia(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, media.ErrSessionV2Disabled):
		return r.Fail("MEDIA_SESSION_V2_DISABLED", "媒体会话未启用", false)
	case errors.Is(err, media.ErrScopeDenied):
		return r.Fail("MEDIA_SCOPE_DENIED", "当前范围不能访问该媒体资源", false)
	case errors.Is(err, media.ErrQueueConflict):
		return r.Fail("MEDIA_QUEUE_REVISION_CONFLICT", "队列已变更，请刷新后重试", false)
	case errors.Is(err, media.ErrSessionNotFound):
		return r.Fail("MEDIA_SESSION_NOT_FOUND", "媒体会话不存在", false)
	case errors.Is(err, media.ErrAssetNotFound):
		return r.Fail("MEDIA_ASSET_NOT_FOUND", "媒体资源不存在", false)
	case errors.Is(err, media.ErrRevisionConflict):
		return r.Fail("MEDIA_REVISION_CONFLICT", "会话已变更，请刷新后重试", false)
	case errors.Is(err, media.ErrIdempotencyConflict):
		return r.Fail("MEDIA_IDEMPOTENCY_CONFLICT", "相同幂等键的请求内容不一致", false)
	case errors.Is(err, media.ErrAssetChanged):
		return r.Fail("MEDIA_ASSET_CHANGED", "媒体文件已变化，请重新选择", false)
	case errors.Is(err, media.ErrPlayerLeaseConflict):
		return r.Fail("MEDIA_PLAYER_LEASE_CONFLICT", "播放器租约无效或已过期", false)
	case errors.Is(err, mediaapp.ErrTicketExpired):
		return r.Fail("MEDIA_TICKET_EXPIRED", "播放票已过期或不存在", false)
	case errors.Is(err, mediaapp.ErrProtectedRemoteMedia):
		return r.Fail("MEDIA_PROTECTED_CONTENT", "不受支持的远程或受保护媒体", false)
	case errors.Is(err, mediaapp.ErrMediaAssetUnauthorized):
		return r.Fail("MEDIA_ASSET_UNAUTHORIZED", "媒体路径未授权", false)
	default:
		return r.Fail("MEDIA_UNAVAILABLE", "媒体服务暂时不可用", true)
	}
}

func requireMediaDispatch(e *Engine, ctx context.Context, r bridge.Request) *bridge.Response {
	if e == nil || e.ccctrl == nil {
		return nil
	}
	cfg, err := e.ccctrl.GetConfig(ctx)
	if err != nil || !cfg.EmergencyStopped {
		return nil
	}
	resp := r.Fail("CC_EMERGENCY_STOPPED", "急停已开启，媒体命令不会派出", false)
	return &resp
}

func handleMediaSessionCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.session.create")
	}
	if failure := requireMediaDispatch(e, ctx, r); failure != nil {
		return *failure
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.create 参数无效", false)
	}
	var p struct {
		AssetID       string   `json:"assetId"`
		QueueAssetIDs []string `json:"queueAssetIds"`
		ScopeKind     string   `json:"scopeKind"`
		ScopeID       string   `json:"scopeId"`
		OperationID   string   `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AssetID) || !validCanonicalULID(p.OperationID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.create 参数无效", false)
	}
	owner := e.memorySubjectID()
	snap, op, err := e.media.CreateSession(ctx, owner, kind, scopeID, p.AssetID, p.OperationID)
	if err != nil {
		return failMedia(r, err)
	}
	if len(p.QueueAssetIDs) > 0 {
		if _, err := e.media.ReplaceQueue(ctx, owner, kind, scopeID, snap.MediaSessionID, snap.QueueRevision, p.QueueAssetIDs); err != nil {
			return failMedia(r, err)
		}
		if snap, err = e.media.GetSession(ctx, owner, snap.MediaSessionID); err != nil {
			return failMedia(r, err)
		}
	}
	return r.Ok(map[string]any{"snapshot": mediaSnapshotDTO(snap), "operation": mediaOperationDTO(op)})
}

func handleMediaSessionGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.session.get")
	}
	var p struct {
		MediaSessionID string `json:"mediaSessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.get 参数无效", false)
	}
	snap, err := e.media.GetSession(ctx, e.memorySubjectID(), p.MediaSessionID)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(mediaSnapshotDTO(snap))
}

func handleMediaSessionList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.session.list")
	}
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.list 参数无效", false)
	}
	var p struct {
		ScopeKind string `json:"scopeKind"`
		ScopeID   string `json:"scopeId"`
		Cursor    string `json:"cursor"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.list 参数无效", false)
	}
	items, err := e.media.ListSessions(ctx, e.memorySubjectID(), kind, scopeID, p.Limit)
	if err != nil {
		return failMedia(r, err)
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, mediaSnapshotDTO(item))
	}
	return r.Ok(map[string]any{"items": out, "nextCursor": nil})
}

func handleMediaSessionCommand(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.session.command")
	}
	if failure := requireMediaDispatch(e, ctx, r); failure != nil {
		return *failure
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	var p struct {
		MediaSessionID   string `json:"mediaSessionId"`
		Action           string `json:"action"`
		PositionMs       *int   `json:"positionMs"`
		Volume           *int   `json:"volume"`
		ExpectedRevision int64  `json:"expectedRevision"`
		OperationID      string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) || !validCanonicalULID(p.OperationID) || p.ExpectedRevision < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.command 参数无效", false)
	}
	position, volume := 0, 0
	if p.PositionMs != nil {
		position = *p.PositionMs
	}
	if p.Volume != nil {
		volume = *p.Volume
	}
	snap, op, err := e.media.Command(ctx, e.memorySubjectID(), p.MediaSessionID, p.Action, p.OperationID, p.ExpectedRevision, position, volume)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"snapshot": mediaSnapshotDTO(snap), "operation": mediaOperationDTO(op)})
}

func handleMediaSessionWatch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.watch 参数无效", false)
	}
	var p struct {
		ScopeKind      string `json:"scopeKind"`
		ScopeID        string `json:"scopeId"`
		MediaSessionID string `json:"mediaSessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.watch 参数无效", false)
	}
	if p.MediaSessionID != "" && !validCanonicalULID(p.MediaSessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.session.watch 参数无效", false)
	}
	streamID := ulid.Make().String()
	emit, _ := ctx.Value(eventEmitterKey{}).(EventEmitter)
	if e != nil && e.media != nil && emit != nil {
		parent, _ := ctx.Value(streamParentKey{}).(context.Context)
		if parent == nil {
			parent = ctx
		}
		e.streamsMu.Lock()
		if e.maxStreams > 0 && len(e.streams) >= e.maxStreams {
			e.streamsMu.Unlock()
			return r.Fail("STREAM_LIMIT_REACHED", "并发流数量已达上限", true)
		}
		scoped, cancel := context.WithCancel(parent)
		state := &streamState{cancel: cancel, state: streamRunning}
		e.streams[streamID] = state
		e.streamsMu.Unlock()
		owner := e.memorySubjectID()
		internalScope := mediaInternalScope(owner, kind, scopeID)
		var seq uint64
		var seqMu sync.Mutex
		unsub := e.media.SubscribeWatch(streamID, owner, kind, internalScope, p.MediaSessionID, func(sessionID string, revision int64) {
			seqMu.Lock()
			seq++
			n := seq
			seqMu.Unlock()
			_ = emit(bridge.Event{
				Version:  bridge.Version,
				Kind:     "event",
				ID:       ulid.Make().String(),
				StreamID: streamID,
				Sequence: n,
				Type:     bridge.EventMediaSnapshot,
				Media:    &bridge.MediaEvent{Kind: "invalidate", MediaSessionID: sessionID, Revision: revision},
			})
		})
		go func() {
			<-scoped.Done()
			unsub()
			e.finishTerminal(streamID, state)
		}()
	}
	return r.Ok(map[string]any{"streamId": streamID})
}

func handleMediaQueueCommand(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.queue.command")
	}
	if failure := requireMediaDispatch(e, ctx, r); failure != nil {
		return *failure
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	var p struct {
		MediaSessionID        string  `json:"mediaSessionId"`
		Action                string  `json:"action"`
		ItemID                string  `json:"itemId"`
		BeforeItemID          *string `json:"beforeItemId"`
		ExpectedQueueRevision int64   `json:"expectedQueueRevision"`
		OperationID           string  `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) || !validCanonicalULID(p.OperationID) || p.ExpectedQueueRevision < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.queue.command 参数无效", false)
	}
	snap, op, err := e.media.QueueCommand(ctx, e.memorySubjectID(), p.MediaSessionID, p.Action, p.ItemID, p.OperationID, p.BeforeItemID, p.ExpectedQueueRevision)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"snapshot": mediaSnapshotDTO(snap), "operation": mediaOperationDTO(op)})
}

func handleMediaAssetList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.asset.list")
	}
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.asset.list 参数无效", false)
	}
	var p struct {
		ScopeKind      string `json:"scopeKind"`
		ScopeID        string `json:"scopeId"`
		MediaSessionID string `json:"mediaSessionId"`
		SourceKind     string `json:"sourceKind"`
		Cursor         string `json:"cursor"`
		Limit          int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.asset.list 参数无效", false)
	}
	var (
		items []media.Asset
		err   error
	)
	if p.MediaSessionID != "" {
		if !validCanonicalULID(p.MediaSessionID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "media.asset.list 参数无效", false)
		}
		items, err = e.media.ListQueueAssets(ctx, e.memorySubjectID(), p.MediaSessionID)
	} else {
		items, err = e.media.ListAssets(ctx, e.memorySubjectID(), kind, scopeID, p.SourceKind, p.Limit)
	}
	if err != nil {
		return failMedia(r, err)
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, mediaAssetDTO(item))
	}
	return r.Ok(map[string]any{"items": out, "nextCursor": nil})
}

func handleMediaAssetOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.asset.open")
	}
	var p struct {
		AssetID        string `json:"assetId"`
		MediaSessionID string `json:"mediaSessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AssetID) || !validCanonicalULID(p.MediaSessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.asset.open 参数无效", false)
	}
	url, expires, err := e.media.OpenAsset(ctx, e.memorySubjectID(), p.AssetID, p.MediaSessionID)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"playbackUrl": url, "expiresAt": expires})
}

func handleMediaOperationGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.operation.get")
	}
	var p struct {
		OperationID string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.OperationID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.operation.get 参数无效", false)
	}
	op, err := e.media.GetOperation(ctx, e.memorySubjectID(), p.OperationID)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(mediaOperationDTO(op))
}

func handleMediaOperationList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "media.operation.list")
	}
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.operation.list 参数无效", false)
	}
	var p struct {
		ScopeKind      string `json:"scopeKind"`
		ScopeID        string `json:"scopeId"`
		MediaSessionID string `json:"mediaSessionId"`
		Cursor         string `json:"cursor"`
		Limit          int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "media.operation.list 参数无效", false)
	}
	items, err := e.media.ListOperations(ctx, e.memorySubjectID(), kind, scopeID, p.Limit)
	if err != nil {
		return failMedia(r, err)
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, mediaOperationDTO(item))
	}
	return r.Ok(map[string]any{"items": out, "nextCursor": nil})
}

func mediaInternalScope(owner, kind, scopeID string) string {
	if kind == "user" {
		return owner
	}
	return scopeID
}

func handleInternalMediaAssetRegister(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "internal.media.asset.register")
	}
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.asset.register 参数无效", false)
	}
	var p struct {
		ScopeKind  string `json:"scopeKind"`
		ScopeID    string `json:"scopeId"`
		SourceKind string `json:"sourceKind"`
		Path       string `json:"path"`
		MIME       string `json:"mime"`
		Kind       string `json:"kind"`
		Title      string `json:"title"`
		Size       int64  `json:"size"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Path) == "" || (p.Kind != "audio" && p.Kind != "video") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.asset.register 参数无效", false)
	}
	if p.SourceKind == "" {
		p.SourceKind = "user_selected"
	}
	if p.MIME == "" {
		p.MIME = "application/octet-stream"
	}
	if p.Title == "" {
		p.Title = "media"
	}
	asset, err := e.media.RegisterAsset(ctx, e.memorySubjectID(), kind, scopeID, p.SourceKind, p.Path, p.MIME, p.Kind, p.Title, p.Size)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(mediaAssetDTO(asset))
}

func handleInternalMediaAssetResolve(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	_ = ctx
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "internal.media.asset.resolve")
	}
	var p struct {
		Token string `json:"token"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Token == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.asset.resolve 参数无效", false)
	}
	path, mime, err := e.media.ResolveTicket(p.Token, e.memorySubjectID())
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"path": path, "mime": mime})
}

func handleInternalMediaPlayerAttach(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "internal.media.player.attach")
	}
	var p struct {
		MediaSessionID   string `json:"mediaSessionId"`
		OperationID      string `json:"operationId"`
		WindowInstanceID string `json:"windowInstanceId"`
		NavigationEpoch  int64  `json:"navigationEpoch"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) || strings.TrimSpace(p.WindowInstanceID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.player.attach 参数无效", false)
	}
	token, generation, expires, err := e.media.AttachPlayer(ctx, e.memorySubjectID(), p.MediaSessionID, p.WindowInstanceID, p.NavigationEpoch)
	if err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"leaseToken": token, "generation": generation, "expiresAt": expires})
}

func handleInternalMediaPlayerNext(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "internal.media.player.next")
	}
	var p struct {
		MediaSessionID   string `json:"mediaSessionId"`
		LeaseToken       string `json:"leaseToken"`
		Generation       int64  `json:"generation"`
		WindowInstanceID string `json:"windowInstanceId"`
		NavigationEpoch  int64  `json:"navigationEpoch"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) || strings.TrimSpace(p.LeaseToken) == "" || strings.TrimSpace(p.WindowInstanceID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.player.next 参数无效", false)
	}
	opID, desired, err := e.media.NextPlayerCommand(ctx, e.memorySubjectID(), p.MediaSessionID, p.WindowInstanceID, p.LeaseToken, p.Generation)
	if err != nil {
		return failMedia(r, err)
	}
	if opID == "" {
		return r.Ok(map[string]any{"operationId": nil, "desiredState": nil})
	}
	return r.Ok(map[string]any{"operationId": opID, "desiredState": desired})
}

func handleInternalMediaPlayerReport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e == nil || e.media == nil {
		return mediaUnavailable(r, "internal.media.player.report")
	}
	var p struct {
		MediaSessionID   string `json:"mediaSessionId"`
		LeaseToken       string `json:"leaseToken"`
		Generation       int64  `json:"generation"`
		WindowInstanceID string `json:"windowInstanceId"`
		NavigationEpoch  int64  `json:"navigationEpoch"`
		OperationID      string `json:"operationId"`
		Event            string `json:"event"`
		PositionMs       int64  `json:"positionMs"`
		DurationMs       int64  `json:"durationMs"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.MediaSessionID) || !validCanonicalULID(p.OperationID) || strings.TrimSpace(p.LeaseToken) == "" || strings.TrimSpace(p.WindowInstanceID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "internal.media.player.report 参数无效", false)
	}
	if err := e.media.ReportPlayer(ctx, e.memorySubjectID(), p.MediaSessionID, p.WindowInstanceID, p.LeaseToken, p.OperationID, p.Event, p.Generation, p.PositionMs, p.DurationMs); err != nil {
		return failMedia(r, err)
	}
	return r.Ok(map[string]any{"accepted": true})
}
