package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func messageServiceAvailable(service MessageService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Pointer || !v.IsNil()
}

type sessionProjectResolver interface {
	Get(context.Context, string) (session.Session, error)
}

func projectIDForSession(e *Engine, ctx context.Context, sessionID string) (string, bool, error) {
	resolver, ok := e.sessions.(sessionProjectResolver)
	if !ok {
		return "", false, nil
	}
	item, err := resolver.Get(ctx, sessionID)
	if err != nil {
		return "", true, err
	}
	return item.ProjectID, true, nil
}

func handleMessageAppend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.append 参数无效", false)
	}
	text, err := message.NormalizeText(p.Text)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.append 参数无效", false)
	}
	p.Text = text
	if !messageServiceAvailable(e.messages) {
		return r.Fail("STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if projectID, ok, err := projectIDForSession(e, ctx, p.SessionID); err != nil {
		return messageFailure(r, err)
	} else if ok {
		if failure := rejectIfProjectReadOnly(e, ctx, r, projectID); failure != nil {
			return *failure
		}
	}
	result, err := e.messages.Append(ctx, r.IdempotencyKey, sessionMutationActor, p, message.Message{SessionID: p.SessionID, Text: text})
	if err != nil {
		return messageFailure(r, err)
	}
	return r.Ok(result)
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.list 参数无效", false)
	}
	if !messageServiceAvailable(e.messages) {
		return r.Fail("STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
	page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: p.SessionID, Cursor: p.Cursor, Direction: p.Direction, Limit: p.Limit, ByteBudget: p.ByteBudget, RequestID: r.ID})
	if err != nil {
		return messageFailure(r, err)
	}
	payload := enrichMessageListPage(page, e.loadSessionArtifactsByMessage(p.SessionID))
	return r.Ok(payload)
}
func handleMessageRewind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		MessageID string `json:"messageId"`
	}
	if decodePayload(r.Payload, &p) != nil || !message.CanonicalULID(p.SessionID) || !message.CanonicalULID(p.MessageID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.rewind 参数无效", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	rewinder, ok := e.messages.(messageRewindService)
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "消息回退暂时不可用", true)
	}
	result, err := rewinder.Rewind(ctx, r.IdempotencyKey, sessionMutationActor, p.SessionID, p.MessageID)
	if err != nil {
		return messageFailure(r, err)
	}
	return r.Ok(result)
}
func handleMessageSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query     string `json:"query"`
		ProjectID string `json:"projectId"`
		SessionID string `json:"sessionId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	query := strings.TrimSpace(p.Query)
	if query == "" || utf8.RuneCountInString(query) > messageapp.MaxSearchQueryRunes {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	if p.ProjectID == "" && p.SessionID == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	if p.ProjectID != "" && !message.CanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	if p.SessionID != "" && !message.CanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	if p.Limit != 0 && (p.Limit < 1 || p.Limit > messageapp.MaxSearchLimit) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "message.search 参数无效", false)
	}
	if !messageServiceAvailable(e.messages) {
		return r.Fail("STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
	searcher, ok := e.messages.(messageSearchService)
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "消息搜索暂时不可用", true)
	}
	result, err := searcher.Search(ctx, messageapp.SearchRequest{Query: query, ProjectID: p.ProjectID, SessionID: p.SessionID, Limit: p.Limit})
	if err != nil {
		return messageFailure(r, err)
	}
	if result.Items == nil {
		result.Items = []messageapp.SearchHit{}
	}
	result.Items = e.ordinaryChatSearchHits(ctx, result.Items)
	return r.Ok(result)
}

func (e *Engine) ordinaryChatSearchHits(ctx context.Context, items []messageapp.SearchHit) []messageapp.SearchHit {
	bound := e.peopleBoundSessionSet(ctx)
	out := make([]messageapp.SearchHit, 0, len(items))
	for _, item := range items {
		if _, ok := bound[item.SessionID]; ok {
			continue
		}
		if isColleagueChatTitle(item.SessionTitle) {
			continue
		}
		out = append(out, item)
	}
	return out
}
func messageFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, messageapp.ErrIdempotencyKeyRequired):
		return r.Fail("IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, messageapp.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, messageapp.ErrSessionNotFound), errors.Is(err, sessionapp.ErrSessionNotFound):
		return r.Fail("SESSION_NOT_FOUND", "会话不存在", false)
	case errors.Is(err, messageapp.ErrMessageNotFound):
		return r.Fail("MESSAGE_NOT_FOUND", "消息不存在", false)
	case errors.Is(err, messageapp.ErrRewindRequiresUserMessage):
		return r.Fail("MESSAGE_REWIND_REQUIRES_USER", "只能从用户消息回退", false)
	case errors.Is(err, messageapp.ErrCursorInvalid):
		return r.Fail("MESSAGE_CURSOR_INVALID", "消息分页游标无效", false)
	case errors.Is(err, messageapp.ErrPageBudgetTooSmall):
		return r.Fail("MESSAGE_PAGE_BUDGET_TOO_SMALL", "消息分页字节预算不足", false)
	case errors.Is(err, messageapp.ErrMessageStorageQuotaReached):
		return r.Fail("MESSAGE_STORAGE_QUOTA_REACHED", "消息存储配额已达到上限", false)
	case errors.Is(err, messageapp.ErrDataInvariantViolation):
		return r.Fail("MESSAGE_DATA_INVARIANT_VIOLATION", "消息数据不一致", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "消息数据暂时不可用", true)
	}
}
