package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/queueapp"
)

type queueDeliveryDTO struct {
	OfficeTaskID string          `json:"officeTaskId,omitempty"`
	ID           string          `json:"id"`
	State        string          `json:"state"`
	StreamID     string          `json:"streamId,omitempty"`
	MessageIDs   []string        `json:"messageIds"`
	Items        []queuedItemDTO `json:"items"`
}

type queueDeliveryStartKey struct{}

func deliveryDTO(d queueapp.Delivery) *queueDeliveryDTO {
	if d.ID == "" {
		return nil
	}
	officeTaskID := ""
	if len(d.Items) > 0 {
		officeTaskID = d.Items[0].OfficeTaskID
	}
	return &queueDeliveryDTO{ID: d.ID, OfficeTaskID: officeTaskID, State: d.State, StreamID: d.StreamID, MessageIDs: append([]string{}, d.MessageIDs...), Items: queueItemDTOs(d.Items)}
}

func (e *Engine) queueOwnerActive(id string) bool {
	if id == "" || id == "renderer" {
		return false
	}
	e.streamsMu.Lock()
	defer e.streamsMu.Unlock()
	state, ok := e.streams[id]
	return ok && state.state != streamTerminal
}

func (e *Engine) reconcileQueueDelivery(ctx context.Context, d queueapp.Delivery) (queueapp.Delivery, error) {
	if d.ID == "" || d.State == "confirmed" || d.State == "unknown" {
		return d, nil
	}
	owner := d.StreamID
	if owner == "" {
		owner = d.Consumer
	}
	if owner == "renderer" || e.queueOwnerActive(owner) {
		return d, nil
	}
	return e.queue.Deliveries().RecoverQueueDelivery(ctx, d.SessionID, d.ID, "mark-unknown")
}

func (e *Engine) prepareQueueDelivery(ctx context.Context, d queueapp.Delivery) (queueapp.Delivery, error) {
	if d.ID == "" || (d.State != "claimed" && d.State != "prepared") {
		return d, nil
	}
	if !messageServiceAvailable(e.messages) {
		return d, queueapp.ErrDeliveryUnavailable
	}
	if projectID, known, err := projectIDForSession(e, ctx, d.SessionID); err != nil {
		return d, err
	} else if known {
		if !projectServiceAvailable(e.projects) {
			return d, queueapp.ErrDeliveryUnavailable
		}
		project, err := e.projects.Get(ctx, projectID)
		if err != nil {
			return d, err
		}
		if !project.CanEditMutableFields() {
			return d, projectapp.ErrInvalidTransition
		}
	}
	ids := make([]string, 0)
	for _, part := range queueapp.DeliveryParts(d) {
		request := struct{ SessionID, Text string }{d.SessionID, part.Text}
		m, err := e.messages.Append(ctx, "queue:"+part.Key, "queue", request, message.Message{SessionID: d.SessionID, Text: part.Text})
		if err != nil {
			if errors.Is(err, messageapp.ErrDataInvariantViolation) {
				if _, markErr := e.queue.Deliveries().RecoverQueueDelivery(ctx, d.SessionID, d.ID, "mark-unknown"); markErr != nil {
					return d, errors.Join(err, markErr)
				}
			}
			return d, err
		}
		ids = append(ids, m.ID)
	}
	return e.queue.Deliveries().PrepareQueueDelivery(ctx, d.SessionID, d.ID, ids)
}

func (e *Engine) validateQueueChatStart(ctx context.Context, sessionID, id string) error {
	if id == "" {
		return nil
	}
	if !validCanonicalULID(id) || e.queue == nil || e.queue.Deliveries() == nil {
		return queueapp.ErrDeliveryUnavailable
	}
	d, err := e.queue.Deliveries().GetQueueDelivery(ctx, sessionID, id)
	if err != nil {
		return err
	}
	if d.Consumer != "renderer" || d.State != "prepared" {
		return queueapp.ErrDeliveryBusy
	}
	// Recheck permanent message receipts so a user rewind cannot resurrect input.
	d, err = e.prepareQueueDelivery(ctx, d)
	if err != nil {
		return err
	}
	if len(d.MessageIDs) == 0 {
		return errors.New("queue delivery has no persisted messages")
	}
	return nil
}
