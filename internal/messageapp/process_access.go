package messageapp

import (
	"context"
	"github.com/lunitide/lunitide/internal/domain/message"
)

// GetInSession validates both membership and existence before reading optional
// process metadata. Deleted/rewound messages cannot expose retained sidecars.
func (s *Service) GetInSession(ctx context.Context, sessionID, messageID string) (message.Message, error) {
	if s == nil || !available(s.uow) {
		return message.Message{}, ErrMessageNotFound
	}
	var out message.Message
	err := s.uow.DoMessage(ctx, func(tx Tx) error {
		var err error
		out, err = tx.Message(ctx, messageID)
		if err != nil {
			return err
		}
		if out.SessionID != sessionID {
			return ErrMessageNotFound
		}
		return nil
	})
	return out, err
}
