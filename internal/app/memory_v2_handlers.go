package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

type memoryItemDTO struct {
	FactID    string  `json:"factId"`
	Version   int64   `json:"version"`
	Revision  int64   `json:"revision"`
	ScopeKind string  `json:"scopeKind"`
	ScopeID   *string `json:"scopeId"`
	Kind      string  `json:"kind"`
	Text      *string `json:"text,omitempty"`
	Forgotten bool    `json:"forgotten"`
	UpdatedAt string  `json:"updatedAt"`
}

type memoryItemSourceRef struct {
	MessageID string `json:"messageId"`
	StartByte *int64 `json:"startByte"`
	EndByte   *int64 `json:"endByte"`
}

func publicMemoryItem(rec m8core.CanonicalMemoryRecord) memoryItemDTO {
	out := memoryItemDTO{
		FactID:    rec.FactID,
		Version:   rec.Version,
		Revision:  rec.Revision,
		ScopeKind: rec.ScopeKind,
		Kind:      rec.Kind,
		Forgotten: rec.Forgotten,
		UpdatedAt: rec.UpdatedAt,
	}
	if rec.ScopeKind == "project" && rec.ScopeID != "" {
		id := rec.ScopeID
		out.ScopeID = &id
	}
	if !rec.Forgotten {
		text := rec.Text
		out.Text = &text
	}
	return out
}

func handleMemoryItemCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind   string               `json:"scopeKind"`
		ScopeID     string               `json:"scopeId"`
		Text        string               `json:"text"`
		SourceRef   *memoryItemSourceRef `json:"sourceRef"`
		OperationID string               `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") ||
		!validCanonicalULID(p.OperationID) || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.create 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.create 参数无效", false)
	}
	if p.ScopeKind == "project" && !validCanonicalULID(p.ScopeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.create 参数无效", false)
	}
	text := strings.TrimSpace(p.Text)
	var sourceKind, sourceRef, quoteDigest string
	var startPtr, endPtr *int64
	if p.SourceRef != nil {
		if text != "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.create 参数无效", false)
		}
		if !validCanonicalULID(p.SourceRef.MessageID) {
			return r.Fail("MEMORY_SOURCE_INVALID", "来源无效", false)
		}
		if e.m8memory == nil {
			return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
		}
		src, err := e.m8memory.ResolveUserMessageText(ctx, p.SourceRef.MessageID)
		if err != nil {
			return memoryV2Failure(r, err)
		}
		raw := []byte(src)
		start, end := int64(0), int64(len(raw))
		if p.SourceRef.StartByte != nil || p.SourceRef.EndByte != nil {
			if p.SourceRef.StartByte == nil || p.SourceRef.EndByte == nil {
				return r.Fail("MEMORY_SOURCE_INVALID", "来源范围无效", false)
			}
			start, end = *p.SourceRef.StartByte, *p.SourceRef.EndByte
		}
		text, err = m8app.ExtractUTF8Span(src, start, end)
		if err != nil {
			return memoryV2Failure(r, err)
		}
		if start < 0 || end < start || end > int64(len(raw)) {
			return r.Fail("MEMORY_SOURCE_INVALID", "来源范围无效", false)
		}
		sum := sha256.Sum256(raw[start:end])
		quoteDigest = hex.EncodeToString(sum[:])
		sourceKind = m8core.MemorySourceUserMessage
		sourceRef = p.SourceRef.MessageID
		startPtr, endPtr = &start, &end
	}
	if text == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.create 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	behavior := m8core.ResolveMemoryBehavior(e.chatMemoryV2(ctx), p.ScopeKind, m8core.CurrentProductFlags())
	if !behavior.AllowExplicitSave {
		if e.chatMemoryV2(ctx).CaptureMode == "off" {
			return r.Fail("MEMORY_MODE_OFF", "记忆已关闭", false)
		}
		return r.Fail("MEMORY_SCOPE_DISABLED", "当前范围未启用记忆", false)
	}
	res, err := e.m8memory.CreateCanonicalItem(ctx, e.memorySubjectID(), m8app.CreateCanonicalItemInput{
		ScopeKind:      p.ScopeKind,
		ScopeID:        p.ScopeID,
		Text:           text,
		OperationID:    p.OperationID,
		IdempotencyKey: r.IdempotencyKey,
		SourceKind:     sourceKind,
		SourceRef:      sourceRef,
		StartByte:      startPtr,
		EndByte:        endPtr,
		QuoteDigest:    quoteDigest,
	})
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		Item             memoryItemDTO `json:"item"`
		DatabaseRevision int64         `json:"databaseRevision"`
		UndoOperationID  string        `json:"undoOperationId"`
		UndoExpiresAt    string        `json:"undoExpiresAt"`
	}{
		Item:             publicMemoryItem(res.Item),
		DatabaseRevision: res.DatabaseRevision,
		UndoOperationID:  res.UndoOperationID,
		UndoExpiresAt:    res.UndoExpiresAt.UTC().Format(time.RFC3339),
	})
}

func handleMemoryItemList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind string `json:"scopeKind"`
		ScopeID   string `json:"scopeId"`
		Kind      string `json:"kind"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.list 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.list 参数无效", false)
	}
	if p.ScopeKind == "project" && !validCanonicalULID(p.ScopeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.list 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	items, rev, err := e.m8memory.ListCanonicalItems(ctx, e.memorySubjectID(), p.ScopeKind, p.ScopeID, p.Kind, p.Limit)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := make([]memoryItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, publicMemoryItem(item))
	}
	return r.Ok(struct {
		Items            []memoryItemDTO `json:"items"`
		NextCursor       *string         `json:"nextCursor"`
		DatabaseRevision int64           `json:"databaseRevision"`
	}{Items: out, DatabaseRevision: rev})
}

func handleMemoryItemGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID  string `json:"factId"`
		Version int64  `json:"version"`
		AsOf    string `json:"asOf"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) || (p.Version > 0 && p.AsOf != "") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.get 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	item, err := e.m8memory.GetCanonicalItem(ctx, e.memorySubjectID(), p.FactID)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	if p.Version > 0 && item.Version != p.Version {
		return r.Fail("M8-001", "记忆不存在", false)
	}
	return r.Ok(publicMemoryItem(item))
}

func handleMemoryItemForget(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID           string `json:"factId"`
		TargetVersion    int64  `json:"targetVersion"`
		Mode             string `json:"mode"`
		RuleCategory     string `json:"ruleCategory"`
		ExpectedRevision int64  `json:"expectedRevision"`
		OperationID      string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) || !validCanonicalULID(p.OperationID) ||
		p.ExpectedRevision < 1 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.forget 参数无效", false)
	}
	switch p.Mode {
	case "fact_history":
		if p.TargetVersion != 0 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.forget 参数无效", false)
		}
	case "this_version":
		if p.TargetVersion < 1 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.forget 参数无效", false)
		}
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.forget 暂不支持该模式", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	item, err := e.m8memory.GetCanonicalItem(ctx, e.memorySubjectID(), p.FactID)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	if p.Mode == "this_version" && item.Version != p.TargetVersion {
		return r.Fail("REVISION_CONFLICT", "版本已变化", false)
	}
	if err := e.m8memory.ForgetCanonicalItem(ctx, e.memorySubjectID(), p.FactID, p.OperationID, p.ExpectedRevision); err != nil {
		return memoryV2Failure(r, err)
	}
	scopeID := ""
	if item.ScopeKind == "project" {
		scopeID = item.ScopeID
	}
	_, rev, err := e.m8memory.ListCanonicalItems(ctx, e.memorySubjectID(), item.ScopeKind, scopeID, "", 1)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		FactID           string `json:"factId"`
		Forgotten        bool   `json:"forgotten"`
		DatabaseRevision int64  `json:"databaseRevision"`
	}{FactID: p.FactID, Forgotten: true, DatabaseRevision: rev})
}

func handleMemoryItemHistory(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID string `json:"factId"`
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.history 参数无效", false)
	}
	var cursor int64
	if p.Cursor != "" {
		parsed, err := strconv.ParseInt(p.Cursor, 10, 64)
		if err != nil || parsed < 1 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.history 参数无效", false)
		}
		cursor = parsed
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	items, next, err := e.m8memory.HistoryCanonicalItem(ctx, e.memorySubjectID(), p.FactID, cursor, p.Limit)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := make([]memoryItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, publicMemoryItem(item))
	}
	var nextCursor *string
	if next != "" {
		nextCursor = &next
	}
	return r.Ok(struct {
		Items      []memoryItemDTO `json:"items"`
		NextCursor *string         `json:"nextCursor"`
	}{Items: out, NextCursor: nextCursor})
}

func handleMemoryItemCorrect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FactID           string `json:"factId"`
		ReplacementText  string `json:"replacementText"`
		ValidFrom        string `json:"validFrom"`
		Reason           string `json:"reason"`
		OperationID      string `json:"operationId"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.FactID) || !validCanonicalULID(p.OperationID) ||
		strings.TrimSpace(p.ReplacementText) == "" || strings.TrimSpace(p.Reason) == "" || p.ExpectedRevision < 1 ||
		strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.correct 参数无效", false)
	}
	var validFrom *time.Time
	if p.ValidFrom != "" {
		parsed, err := time.Parse(time.RFC3339, p.ValidFrom)
		if err != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.item.correct 参数无效", false)
		}
		validFrom = &parsed
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	item, rev, err := e.m8memory.CorrectCanonicalItem(ctx, e.memorySubjectID(), p.FactID, p.ReplacementText, p.Reason, p.OperationID, r.IdempotencyKey, p.ExpectedRevision, validFrom)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		Item             memoryItemDTO `json:"item"`
		DatabaseRevision int64         `json:"databaseRevision"`
	}{Item: publicMemoryItem(item), DatabaseRevision: rev})
}

func handleMemoryCaptureUndo(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UndoOperationID string `json:"undoOperationId"`
		OperationID     string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UndoOperationID) || !validCanonicalULID(p.OperationID) ||
		strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.capture.undo 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	res, err := e.m8memory.UndoCanonicalCapture(ctx, e.memorySubjectID(), p.UndoOperationID, p.OperationID, r.IdempotencyKey)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	ids := res.ForgottenFactIDs
	if ids == nil {
		ids = []string{}
	}
	return r.Ok(struct {
		Undone           bool     `json:"undone"`
		ForgottenFactIDs []string `json:"forgottenFactIds"`
		DatabaseRevision int64    `json:"databaseRevision"`
	}{Undone: true, ForgottenFactIDs: ids, DatabaseRevision: res.DatabaseRevision})
}

func handleMemoryReviewList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind string `json:"scopeKind"`
		ScopeID   string `json:"scopeId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.list 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.list 参数无效", false)
	}
	if p.ScopeKind == "project" && !validCanonicalULID(p.ScopeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.list 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	items, rev, err := e.m8memory.ListMemoryReviews(ctx, e.memorySubjectID(), p.ScopeKind, p.ScopeID, p.Limit)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := make([]memoryReviewDTO, 0, len(items))
	for _, item := range items {
		out = append(out, publicMemoryReview(item))
	}
	var next *string
	if p.Limit > 0 && len(items) == p.Limit {
		id := items[len(items)-1].ReviewID
		next = &id
	}
	return r.Ok(struct {
		Items            []memoryReviewDTO `json:"items"`
		NextCursor       *string           `json:"nextCursor"`
		DatabaseRevision int64             `json:"databaseRevision"`
	}{Items: out, NextCursor: next, DatabaseRevision: rev})
}

func handleMemoryReviewResolve(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ReviewID         string `json:"reviewId"`
		Decision         string `json:"decision"`
		Text             string `json:"text"`
		OperationID      string `json:"operationId"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ReviewID) || !validCanonicalULID(p.OperationID) ||
		p.ExpectedRevision < 1 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.resolve 参数无效", false)
	}
	switch p.Decision {
	case "accept", "reject":
		if p.Text != "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.resolve 参数无效", false)
		}
	case "correct":
		if strings.TrimSpace(p.Text) == "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.resolve 参数无效", false)
		}
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.review.resolve 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	item, rev, err := e.m8memory.ResolveMemoryReview(ctx, e.memorySubjectID(), p.ReviewID, p.Decision, p.Text, p.OperationID, r.IdempotencyKey, p.ExpectedRevision)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := struct {
		ReviewID         string         `json:"reviewId"`
		Decision         string         `json:"decision"`
		Item             *memoryItemDTO `json:"item,omitempty"`
		DatabaseRevision int64          `json:"databaseRevision"`
	}{ReviewID: p.ReviewID, Decision: p.Decision, DatabaseRevision: rev}
	if item.FactID != "" {
		dto := publicMemoryItem(item)
		out.Item = &dto
	}
	return r.Ok(out)
}

func handleMemoryPurgePrepare(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind                string `json:"scopeKind"`
		ScopeID                  string `json:"scopeId"`
		ExpectedDatabaseRevision int64  `json:"expectedDatabaseRevision"`
		OperationID              string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") ||
		!validCanonicalULID(p.OperationID) || p.ExpectedDatabaseRevision < 0 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.purge.prepare 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.purge.prepare 参数无效", false)
	}
	if p.ScopeKind == "project" && !validCanonicalULID(p.ScopeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.purge.prepare 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	res, err := e.m8memory.PrepareMemoryPurge(ctx, e.memorySubjectID(), p.ScopeKind, p.ScopeID, p.OperationID, r.IdempotencyKey, p.ExpectedDatabaseRevision)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		Counts struct {
			Facts           int64 `json:"facts"`
			Candidates      int64 `json:"candidates"`
			SearchDocuments int64 `json:"searchDocuments"`
			Embeddings      int64 `json:"embeddings"`
		} `json:"counts"`
		SnapshotDigest    string `json:"snapshotDigest"`
		ConfirmationToken string `json:"confirmationToken"`
		ExpiresAt         string `json:"expiresAt"`
		OperationID       string `json:"operationId"`
	}{
		Counts: struct {
			Facts           int64 `json:"facts"`
			Candidates      int64 `json:"candidates"`
			SearchDocuments int64 `json:"searchDocuments"`
			Embeddings      int64 `json:"embeddings"`
		}{Facts: res.Counts.Facts, Candidates: res.Counts.Candidates, SearchDocuments: res.Counts.SearchDocuments, Embeddings: res.Counts.Embeddings},
		SnapshotDigest: res.SnapshotDigest, ConfirmationToken: res.ConfirmationToken,
		ExpiresAt: res.ExpiresAt, OperationID: res.OperationID,
	})
}

type memoryReviewDTO struct {
	ReviewID       string   `json:"reviewId"`
	Kind           string   `json:"kind"`
	Novelty        string   `json:"novelty"`
	ReasonCodes    []string `json:"reasonCodes"`
	ConflictFactID *string  `json:"conflictFactId"`
	Text           *string  `json:"text,omitempty"`
	CreatedAt      string   `json:"createdAt"`
}

func publicMemoryReview(rec m8core.MemoryReviewRecord) memoryReviewDTO {
	out := memoryReviewDTO{
		ReviewID:    rec.ReviewID,
		Kind:        rec.Kind,
		Novelty:     rec.Novelty,
		ReasonCodes: rec.ReasonCodes,
		CreatedAt:   rec.CreatedAt,
	}
	if out.ReasonCodes == nil {
		out.ReasonCodes = []string{}
	}
	if rec.ConflictFactID != "" {
		id := rec.ConflictFactID
		out.ConflictFactID = &id
	}
	if rec.Text != "" {
		text := rec.Text
		out.Text = &text
	}
	return out
}

func memoryV2Failure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrMemoryModeOff):
		return r.Fail("MEMORY_MODE_OFF", "记忆已关闭", false)
	case errors.Is(err, m8app.ErrMemoryScopeDisabled):
		return r.Fail("MEMORY_SCOPE_DISABLED", "当前范围未启用记忆", false)
	case errors.Is(err, m8app.ErrMemorySourceInvalid):
		return r.Fail("MEMORY_SOURCE_INVALID", "来源无效", false)
	case errors.Is(err, m8core.ErrNotFound):
		return r.Fail("M8-001", "记忆不存在", false)
	case errors.Is(err, m8core.ErrRevisionConflict):
		return r.Fail("REVISION_CONFLICT", "版本已变化", false)
	case errors.Is(err, m8core.ErrOperationReplayMismatch):
		return r.Fail("OPERATION_REPLAY_MISMATCH", "幂等键与内容不一致", false)
	case errors.Is(err, m8core.ErrUndoConflict), errors.Is(err, m8app.ErrMemoryUndoConflict):
		return r.Fail("MEMORY_UNDO_CONFLICT", "捕获已无法撤销", false)
	case errors.Is(err, m8core.ErrGenerationNotFound):
		return r.Fail("MEMORY_GENERATION_NOT_FOUND", "整理版本不存在", false)
	case errors.Is(err, m8core.ErrGenerationNotReady):
		return r.Fail("MEMORY_GENERATION_NOT_READY", "整理版本尚未就绪", false)
	case errors.Is(err, m8core.ErrGenerationActive):
		return r.Fail("MEMORY_GENERATION_ACTIVE", "整理版本正在使用", false)
	case errors.Is(err, m8core.ErrGenerationStateInvalid):
		return r.Fail("MEMORY_GENERATION_STATE_INVALID", "整理版本状态不允许该操作", false)
	case errors.Is(err, m8core.ErrImportSourceMissing):
		return r.Fail("MEMORY_IMPORT_SOURCE_MISSING", "导入源不存在", false)
	case errors.Is(err, m8core.ErrImportTooLarge):
		return r.Fail("MEMORY_IMPORT_TOO_LARGE", "导入包超过限制", false)
	case errors.Is(err, m8core.ErrImportSchemaUnsupported):
		return r.Fail("MEMORY_IMPORT_SCHEMA_UNSUPPORTED", "导入格式不受支持", false)
	case errors.Is(err, m8core.ErrImportDigestMismatch):
		return r.Fail("MEMORY_IMPORT_DIGEST_MISMATCH", "导入摘要不匹配", false)
	case errors.Is(err, m8core.ErrImportPreviewExpired):
		return r.Fail("MEMORY_IMPORT_PREVIEW_EXPIRED", "导入预演已过期", false)
	case errors.Is(err, m8core.ErrImportScopeDenied):
		return r.Fail("MEMORY_IMPORT_SCOPE_DENIED", "导入范围未授权", false)
	case errors.Is(err, m8core.ErrPurgeGrantInvalid), errors.Is(err, m8app.ErrPurgeGrantInvalid):
		return r.Fail("PURGE_GRANT_INVALID", "清除确认令牌无效", false)
	case errors.Is(err, m8app.ErrPurgeConfirmationRequired):
		return r.Fail("PURGE_CONFIRMATION_REQUIRED", "需要先准备清除确认令牌", false)
	case errors.Is(err, m8app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	return m8MemoryFailure(r, err)
}
