package app

import (
	"context"
	"errors"
	"reflect"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
)

func messageServiceAvailable(service MessageService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}
func handleMessageAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "message.append 参数无效", false)
	}
	text, err := message.NormalizeText(p.Text)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "message.append 参数无效", false)
	}
	p.Text = text
	if !messageServiceAvailable(e.messages) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.messages.Append(ctx, r.IdempotencyKey, sessionMutationActor, p, message.Message{SessionID: p.SessionID, Text: text})
	if err != nil {
		return messageFailure(r, err)
	}
	return bridge.Success(r.ID, result)
}
func handleMessageList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID  string               `json:"sessionId"`
		Cursor     string               `json:"cursor"`
		Direction  messageapp.Direction `json:"direction"`
		Limit      int                  `json:"limit"`
		ByteBudget int                  `json:"byteBudget"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) || (p.Direction != "" && p.Direction != messageapp.Forward && p.Direction != messageapp.Backward) || (p.Limit != 0 && (p.Limit < 1 || p.Limit > 256)) || (p.ByteBudget != 0 && (p.ByteBudget < messageapp.MinByteBudget || p.ByteBudget > messageapp.MaxByteBudget)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "message.list 参数无效", false)
	}
	if !messageServiceAvailable(e.messages) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
	page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: p.SessionID, Cursor: p.Cursor, Direction: p.Direction, Limit: p.Limit, ByteBudget: p.ByteBudget, RequestID: r.ID})
	if err != nil {
		return messageFailure(r, err)
	}
	payload := enrichMessageListPage(page, e.loadSessionArtifactsByMessage(p.SessionID))
	return bridge.Success(r.ID, payload)
}
func handleMessageRewind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		MessageID string `json:"messageId"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) || !message.CanonicalULID(p.MessageID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "message.rewind 参数无效", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	rewinder, ok := e.messages.(messageRewindService)
	if !ok {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息回退暂时不可用", true)
	}
	result, err := rewinder.Rewind(ctx, r.IdempotencyKey, sessionMutationActor, p.SessionID, p.MessageID)
	if err != nil {
		return messageFailure(r, err)
	}
	return bridge.Success(r.ID, result)
}
func messageFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, messageapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, messageapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, messageapp.ErrSessionNotFound):
		return bridge.Failure(r.ID, r.TraceID, "SESSION_NOT_FOUND", "会话不存在", false)
	case errors.Is(err, messageapp.ErrMessageNotFound):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_NOT_FOUND", "消息不存在", false)
	case errors.Is(err, messageapp.ErrRewindRequiresUserMessage):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_REWIND_REQUIRES_USER", "只能从用户消息回退", false)
	case errors.Is(err, messageapp.ErrCursorInvalid):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_CURSOR_INVALID", "消息分页游标无效", false)
	case errors.Is(err, messageapp.ErrPageBudgetTooSmall):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_PAGE_BUDGET_TOO_SMALL", "消息分页字节预算不足", false)
	case errors.Is(err, messageapp.ErrMessageStorageQuotaReached):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_STORAGE_QUOTA_REACHED", "消息存储配额已达到上限", false)
	case errors.Is(err, messageapp.ErrDataInvariantViolation):
		return bridge.Failure(r.ID, r.TraceID, "MESSAGE_DATA_INVARIANT_VIOLATION", "消息数据不一致", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
}
