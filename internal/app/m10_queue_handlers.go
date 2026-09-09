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
	OfficeTaskID string `json:"officeTaskId,omitempty"`
	QueuedID     string `json:"queuedId"`
	Seq          int64  `json:"seq"`
	Text         string `json:"text"`
	Status       string `json:"status"`
	Mark         string `json:"mark"`
	CreatedAt    string `json:"createdAt"`
}

func handleRunQueueInput(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OfficeTaskID string `json:"officeTaskId"`
		SessionID    string `json:"sessionId"`
		Text         string `json:"text"`
		Mark         string `json:"mark"`
		RequestID    string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 ||
		(p.Mark != "" && !queueinput.ValidMark(p.Mark)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.queueInput 参数无效", false)
	}
	if e.queue == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	if err := e.validateOfficeChatTask(ctx, p.SessionID, p.OfficeTaskID); err != nil {
		return officeFailure(r, err)
	}
	ctx = withOfficeTask(ctx, p.OfficeTaskID)
	m, err := e.queue.Enqueue(ctx, p.SessionID, "", p.Text, p.Mark, p.RequestID)
	if err != nil {
		return queueFailure(r, err)
	}
	return r.Ok(struct {
		QueuedID string `json:"queuedId"`
		Seq      int64  `json:"seq"`
		Status   string `json:"status"`
		Mark     string `json:"mark"`
	}{QueuedID: m.ID, Seq: m.Seq, Status: m.Status, Mark: m.Mark})
}

func handleRunQueueList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OfficeTaskID string `json:"officeTaskId"`
		SessionID    string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.queueList 参数无效", false)
	}
	if e.queue == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	if err := e.validateOfficeChatTask(ctx, p.SessionID, p.OfficeTaskID); err != nil {
		return officeFailure(r, err)
	}
	ctx = withOfficeTask(ctx, p.OfficeTaskID)
	items, err := e.queue.List(ctx, p.SessionID)
	if err != nil {
		return queueFailure(r, err)
	}
	var delivery *queueDeliveryDTO
	if store := e.queue.Deliveries(); store != nil {
		d, err := store.PendingQueueDelivery(ctx, p.SessionID)
		if err != nil {
			return queueFailure(r, err)
		}
		d, err = e.reconcileQueueDelivery(ctx, d)
		if err != nil {
			return queueFailure(r, err)
		}
		delivery = deliveryDTO(d)
	}
	return r.Ok(struct {
		Items    []queuedItemDTO   `json:"items"`
		Delivery *queueDeliveryDTO `json:"delivery,omitempty"`
	}{Items: queueItemDTOs(items), Delivery: delivery})
}

func handleRunQueueWithdraw(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OfficeTaskID string `json:"officeTaskId"`
		SessionID    string `json:"sessionId"`
		QueuedID     string `json:"queuedId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) || !validCanonicalULID(p.QueuedID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.queueWithdraw 参数无效", false)
	}
	if e.queue == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	if err := e.validateOfficeChatTask(ctx, p.SessionID, p.OfficeTaskID); err != nil {
		return officeFailure(r, err)
	}
	ctx = withOfficeTask(ctx, p.OfficeTaskID)
	m, err := e.queue.Withdraw(ctx, p.SessionID, p.QueuedID)
	if err != nil {
		return queueFailure(r, err)
	}
	return r.Ok(struct {
		QueuedID string `json:"queuedId"`
		Status   string `json:"status"`
	}{QueuedID: m.ID, Status: m.Status})
}

func handleRunQueueConsume(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OfficeTaskID string `json:"officeTaskId"`
		SessionID    string `json:"sessionId"`
		DeliveryID   string `json:"deliveryId"`
		Action       string `json:"action"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) || (p.DeliveryID != "" && !validCanonicalULID(p.DeliveryID)) || (p.Action != "" && p.Action != "resume" && p.Action != "dismiss") || ((p.Action == "") != (p.DeliveryID == "")) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "run.queueConsume 参数无效", false)
	}
	if e.queue == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "排队输入服务暂时不可用", true)
	}
	if err := e.validateOfficeChatTask(ctx, p.SessionID, p.OfficeTaskID); err != nil {
		return officeFailure(r, err)
	}
	ctx = withOfficeTask(ctx, p.OfficeTaskID)
	if store := e.queue.Deliveries(); store != nil {
		if !e.reserveChatSession(p.SessionID) {
			return queueFailure(r, queueapp.ErrDeliveryBusy)
		}
		defer e.releaseChatSession(p.SessionID)
		var d queueapp.Delivery
		var err error
		if p.Action != "" {
			d, err = store.GetQueueDelivery(ctx, p.SessionID, p.DeliveryID)
			if err != nil {
				return queueFailure(r, err)
			}
			if e.queueOwnerActive(d.StreamID) || e.queueOwnerActive(d.Consumer) {
				return queueFailure(r, queueapp.ErrDeliveryBusy)
			}
			d, err = store.RecoverQueueDelivery(ctx, p.SessionID, p.DeliveryID, p.Action)
		} else {
			d, err = store.ClaimQueueDelivery(ctx, p.SessionID, "renderer")
		}
		if err != nil {
			return queueFailure(r, err)
		}
		d, err = e.reconcileQueueDelivery(ctx, d)
		if err != nil {
			return queueFailure(r, err)
		}
		d, err = e.prepareQueueDelivery(ctx, d)
		if err != nil {
			return queueFailure(r, err)
		}
		return r.Ok(struct {
			Count    int               `json:"count"`
			Items    []queuedItemDTO   `json:"items"`
			Delivery *queueDeliveryDTO `json:"delivery,omitempty"`
		}{len(d.Items), queueItemDTOs(d.Items), deliveryDTO(d)})
	}
	items, err := e.queue.Consume(ctx, p.SessionID)
	if err != nil {
		return queueFailure(r, err)
	}
	return r.Ok(struct {
		Count int             `json:"count"`
		Items []queuedItemDTO `json:"items"`
	}{Count: len(items), Items: queueItemDTOs(items)})
}

func queueItemDTOs(items []queueinput.Message) []queuedItemDTO {
	result := make([]queuedItemDTO, 0, len(items))
	for _, m := range items {
		result = append(result, queuedItemDTO{
			OfficeTaskID: m.OfficeTaskID,
			QueuedID:     m.ID, Seq: m.Seq, Text: m.Payload,
			Status: m.Status, Mark: m.Mark, CreatedAt: m.CreatedAt,
		})
	}
	return result
}

// queueFailure maps queueapp errors onto M10-QI-001~007.
func queueFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, queueapp.ErrDeliveryBusy):
		return r.Fail("QUEUE_DELIVERY_BUSY", "补充输入已交付或仍在处理中，请读取实际状态后核对", true)
	case errors.Is(err, queueapp.ErrDeliveryUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "补充输入的持久交付服务不可用", true)
	case errors.Is(err, queueapp.ErrPayloadInvalid):
		return r.Fail("M10-QI-001", "补充文本须为有效文字，最多 32768 字符、131072 字节", false)
	case errors.Is(err, queueapp.ErrSessionNotFound):
		return r.Fail("M10-QI-002", "会话不存在或不可用", false)
	case errors.Is(err, queueapp.ErrNotFound):
		return r.Fail("M10-QI-003", "排队消息不存在", false)
	case errors.Is(err, queueapp.ErrRequestReused):
		return r.Fail("M10-QI-004", "请求已处理或同一请求标识的内容发生变化，请核对队列", false)
	case errors.Is(err, queueapp.ErrTerminalState):
		return r.Fail("M10-QI-004", "排队消息已注入或已撤回", false)
	case errors.Is(err, queueapp.ErrQueueFull):
		return r.Fail("M10-QI-005", "队列已满（5 条），请先撤回或等待注入", false)
	case errors.Is(err, queueapp.ErrRateLimited):
		return r.Fail("M10-QI-007", "排队频率超限，请稍后重试", false)
	}
	return r.Fail("STORAGE_UNAVAILABLE", "排队输入存储暂时不可用", true)
}
