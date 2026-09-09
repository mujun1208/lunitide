package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/queueapp"
)

func TestOfficeQueueBridgeAndChatStartBindOriginalTask(t *testing.T) {
	e, store := officeEngineFixture(t)
	e.SetQueueService(queueapp.New(store))
	a := officeCreatedTask(t, e, "queue-office-a")
	created := officeCall(t, e, "office.task.create", "queue-office-b", map[string]any{"title": "同会话第二任务", "sessionId": a.SessionID})
	if !created.OK {
		t.Fatal(created.Error)
	}
	var b struct{ Task struct{ ID string } }
	if err := decodeResponsePayload(created.Payload, &b); err != nil {
		t.Fatal(err)
	}
	input := officeCall(t, e, "run.queueInput", "", map[string]any{"sessionId": a.SessionID, "officeTaskId": a.ID, "text": "只修改任务 A", "requestId": "office-input-a"})
	if !input.OK {
		t.Fatal(input.Error)
	}
	var queued struct{ QueuedID string }
	if err := decodeResponsePayload(input.Payload, &queued); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"", b.Task.ID} {
		payload := map[string]any{"sessionId": a.SessionID}
		if scope != "" {
			payload["officeTaskId"] = scope
		}
		for _, method := range []string{"run.queueList", "run.queueConsume"} {
			r := officeCall(t, e, method, "", payload)
			if !r.OK {
				t.Fatal(r.Error)
			}
			var out struct {
				Items    []queuedItemDTO
				Delivery *queueDeliveryDTO
			}
			if err := decodeResponsePayload(r.Payload, &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Items) != 0 || out.Delivery != nil {
				t.Fatalf("%s exposed A to %s: %+v", method, scope, out)
			}
		}
		payload["queuedId"] = queued.QueuedID
		if r := officeCall(t, e, "run.queueWithdraw", "", payload); r.OK || r.Error.Code != "M10-QI-003" {
			t.Fatalf("wrong task withdrawal: %+v", r)
		}
	}
	consumed := officeCall(t, e, "run.queueConsume", "", map[string]any{"sessionId": a.SessionID, "officeTaskId": a.ID})
	if !consumed.OK {
		t.Fatal(consumed.Error)
	}
	var out struct {
		Items    []queuedItemDTO
		Delivery *queueDeliveryDTO
	}
	if err := decodeResponsePayload(consumed.Payload, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].OfficeTaskID != a.ID || out.Delivery == nil || out.Delivery.OfficeTaskID != a.ID || out.Delivery.State != "prepared" {
		t.Fatalf("lost task metadata: %+v", out)
	}
	if err := e.validateQueueChatStart(withOfficeTask(context.Background(), a.ID), a.SessionID, out.Delivery.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.validateQueueChatStart(withOfficeTask(context.Background(), b.Task.ID), a.SessionID, out.Delivery.ID); !errors.Is(err, queueapp.ErrNotFound) {
		t.Fatalf("B started A delivery: %v", err)
	}
	if r := officeCall(t, e, "run.queueConsume", "", map[string]any{"sessionId": a.SessionID, "officeTaskId": b.Task.ID, "deliveryId": out.Delivery.ID, "action": "dismiss"}); r.OK || r.Error.Code != "M10-QI-003" {
		t.Fatalf("B dismissed A: %+v", r)
	}
	other := officeCreatedTask(t, e, "queue-office-other-session")
	if r := officeCall(t, e, "run.queueInput", "", map[string]any{"sessionId": other.SessionID, "officeTaskId": a.ID, "text": "不应落库", "requestId": "cross-session"}); r.OK {
		t.Fatal("Office task accepted another session")
	}
	if rows, err := e.queue.List(context.Background(), other.SessionID); err != nil || len(rows) != 0 {
		t.Fatalf("invalid scope wrote input: %+v %v", rows, err)
	}
}
