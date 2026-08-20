package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/queueapp"
)

// M10 queued-input handlers: run.queueInput / run.queueList /
// run.queueWithdraw / run.queueConsume. Error mapping follows the M10
// wire contract (M10-QI-001~007); consume is the terminal routing path —
// consume is also used mid-turn by the chat tool loop so supplements
// join the current task instead of waiting for the stream to settle.

type queuedItemDTO struct {
	QueuedID  string `json:"queuedId"`
	Seq       int64  `json:"seq"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	Mark      string `json:"mark"`
	CreatedAt string `json:"createdAt"`
}

func handleRunQueueInput(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
		Mark      string `json:"mark"`
		RequestID string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 ||
		(p.Mark != "" && !queueinput.ValidMark(p.Mark)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "run.queueInput 参数无效", false)
	}
	if e.queue == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	m, err := e.queue.Enqueue(ctx, p.SessionID, "", p.Text, p.Mark, p.RequestID)
	if err != nil {
		return queueFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		QueuedID string `json:"queuedId"`
		Seq      int64  `json:"seq"`
		Status   string `json:"status"`
		Mark     string `json:"mark"`
	}{QueuedID: m.ID, Seq: m.Seq, Status: m.Status, Mark: m.Mark})
}

func handleRunQueueList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "run.queueList 参数无效", false)
	}
	if e.queue == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	items, err := e.queue.List(ctx, p.SessionID)
	if err != nil {
		return queueFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Items []queuedItemDTO `json:"items"`
	}{Items: queueItemDTOs(items)})
}

func handleRunQueueWithdraw(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		QueuedID  string `json:"queuedId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) || !validCanonicalULID(p.QueuedID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "run.queueWithdraw 参数无效", false)
	}
	if e.queue == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	m, err := e.queue.Withdraw(ctx, p.SessionID, p.QueuedID)
	if err != nil {
		return queueFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		QueuedID string `json:"queuedId"`
		Status   string `json:"status"`
	}{QueuedID: m.ID, Status: m.Status})
}

func handleRunQueueConsume(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "run.queueConsume 参数无效", false)
	}
	if e.queue == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	items, err := e.queue.Consume(ctx, p.SessionID)
	if err != nil {
		return queueFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Count int             `json:"count"`
		Items []queuedItemDTO `json:"items"`
	}{Count: len(items), Items: queueItemDTOs(items)})
}

func queueItemDTOs(items []queueinput.Message) []queuedItemDTO {
	result := make([]queuedItemDTO, 0, len(items))
	for _, m := range items {
		result = append(result, queuedItemDTO{
			QueuedID: m.ID, Seq: m.Seq, Text: m.Payload,
			Status: m.Status, Mark: m.Mark, CreatedAt: m.CreatedAt,
		})
	}
	return result
}

// queueFailure maps queueapp errors onto M10-QI-001~007.
func queueFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, queueapp.ErrPayloadInvalid):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-001", "补充文本为空或超过 8000 字符", false)
	case errors.Is(err, queueapp.ErrSessionNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-002", "会话不存在或不可用", false)
	case errors.Is(err, queueapp.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-003", "排队消息不存在", false)
	case errors.Is(err, queueapp.ErrTerminalState), errors.Is(err, queueapp.ErrRequestReused):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-004", "排队消息已注入或已撤回", false)
	case errors.Is(err, queueapp.ErrQueueFull):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-005", "队列已满（5 条），请先撤回或等待注入", false)
	case errors.Is(err, queueapp.ErrRateLimited):
		return bridge.Failure(r.ID, r.TraceID, "M10-QI-007", "排队频率超限，请稍后重试", false)
	}
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "排队输入存储暂时不可用", true)
}
