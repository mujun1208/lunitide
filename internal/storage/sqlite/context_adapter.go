package sqlite

import (
	"context"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/messageapp"
)

// contextAdapter adapts the Store's ListMessages to contextapp.Reader,
// bridging the messageapp pagination interface to the context assembly
// interface used by the chat engine.
type contextAdapter struct {
	store     *Store
	tokenRepo token.Repository
}

func newContextAdapter(store *Store) *contextAdapter {
	return &contextAdapter{store: store, tokenRepo: store}
}

func (a *contextAdapter) ListMessages(ctx context.Context, sessionID string, direction string, limit int) ([]contextapp.Message, error) {
	dir := messageapp.Forward
	if direction == "backward" {
		dir = messageapp.Backward
	}
	msgs, _, _, err := a.store.ListMessages(ctx, messageapp.PageQuery{
		SessionID: sessionID,
		Direction: dir,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]contextapp.Message, len(msgs))
	for i, m := range msgs {
		result[i] = contextapp.Message{
			ID:       m.ID,
			Role:     string(m.Role),
			Content:  m.Text,
			Sequence: m.Sequence,
		}
	}
	return result, nil
}

func (a *contextAdapter) SumTokens(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error) {
	return a.tokenRepo.SumTokenLedgerBySession(ctx, sessionID, provider, model, tokenizerRevision)
}