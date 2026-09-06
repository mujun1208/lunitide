package queueapp

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
)

var ErrDeliveryUnavailable = errors.New("durable queue delivery unavailable")
var ErrDeliveryBusy = errors.New("queue delivery is already owned or started")

type Delivery struct {
	ID, SessionID, Consumer, State, StreamID string
	MessageIDs                               []string
	Items                                    []queueinput.Message
	CreatedAt, UpdatedAt                     string
}

type DeliveryStore interface {
	ClaimQueueDelivery(context.Context, string, string) (Delivery, error)
	GetQueueDelivery(context.Context, string, string) (Delivery, error)
	PendingQueueDelivery(context.Context, string) (Delivery, error)
	PrepareQueueDelivery(context.Context, string, string, []string) (Delivery, error)
	StartQueueDelivery(context.Context, string, string, string) error
	FinishQueueDeliveries(context.Context, string, string, bool) error
	RecoverQueueDelivery(context.Context, string, string, string) (Delivery, error)
}

func (s *Service) Deliveries() DeliveryStore {
	if s == nil {
		return nil
	}
	d, _ := s.store.(DeliveryStore)
	return d
}

// DeliveryParts uses the ordinary message contract. Keys are tied to immutable
// queue row IDs and offsets, so partial appends survive process restarts safely.
type DeliveryPart struct{ Key, Text string }

func DeliveryParts(d Delivery) []DeliveryPart {
	var out []DeliveryPart
	for _, item := range d.Items {
		text := strings.ReplaceAll(strings.ReplaceAll(item.Payload, "\r\n", "\n"), "\r", "\n")
		runes := []rune(text)
		for offset := 0; offset < len(runes); offset += message.MaxRunes {
			end := min(offset+message.MaxRunes, len(runes))
			out = append(out, DeliveryPart{Key: item.ID + ":" + string(rune('0'+offset/message.MaxRunes)), Text: string(runes[offset:end])})
		}
	}
	return out
}
