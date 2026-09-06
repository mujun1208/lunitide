package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/messageapp"
)

type ChatTurnJournal interface {
	PutChatTurn(context.Context, string, string, []byte, bool) error
	LatestChatTurn(context.Context, string) ([]byte, error)
	PendingChatTurns(context.Context, string) ([][]byte, error)
}

func (e *Engine) SetChatTurnJournal(store ChatTurnJournal) { e.turnJournal = store }

type turnSessionReader interface {
	ChatTurnSessionExists(context.Context, string) (bool, error)
}

func (e *Engine) turnCheckpointSessionExists(ctx context.Context, sessionID string) (bool, error) {
	if e == nil {
		return false, nil
	}
	payload, _ := json.Marshal(map[string]string{"sessionId": sessionID})
	release, scopeErr := e.authorizeDataRequest(ctx, "chat.checkpoint", payload)
	if scopeErr != nil {
		return false, scopeErr
	}
	defer release()
	if store, ok := e.turnJournal.(turnSessionReader); ok {
		return store.ChatTurnSessionExists(ctx, sessionID)
	}
	_, _, err := projectIDForSession(e, ctx, sessionID)
	if err != nil {
		return false, err
	}
	// Legacy/test engines without any session reader retain their historical
	// behavior. Production SQLite journals always verify the actual owner.
	return true, nil
}

func (e *Engine) pendingTurnCheckpoints(ctx context.Context, sessionID string) ([]chatTurnCheckpoint, error) {
	if exists, err := e.turnCheckpointSessionExists(ctx, sessionID); err != nil {
		return nil, err
	} else if !exists {
		return nil, messageapp.ErrSessionNotFound
	}
	if e.turnJournal == nil {
		cp := e.loadTurnCheckpoint(sessionID)
		if cp.PersistDraft == "" {
			return nil, nil
		}
		return []chatTurnCheckpoint{cp}, nil
	}
	rows, err := e.turnJournal.PendingChatTurns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var out []chatTurnCheckpoint
	for _, raw := range rows {
		var cp chatTurnCheckpoint
		if err := json.Unmarshal(raw, &cp); err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	if len(out) == 0 {
		latest, err := e.turnJournal.LatestChatTurn(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if len(latest) > 0 {
			return out, nil
		}
		// One-time import of the pre-journal recovery file. Leave the original
		// intact until the durable write succeeds.
		cp := e.loadLegacyTurnCheckpoint(sessionID)
		if cp.PersistDraft != "" {
			if err := e.saveTurnCheckpoint(sessionID, cp); err != nil {
				return nil, err
			}
			return e.pendingTurnCheckpoints(ctx, sessionID)
		}
	}
	return out, nil
}

func turnJournalContext() (context.Context, context.CancelFunc) {
	// Persist cancellation/failure receipts even after the stream context ends.
	return context.WithTimeout(context.Background(), 5*time.Second)
}
