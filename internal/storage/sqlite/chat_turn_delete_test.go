package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestDeleteRemovesChatTurnJournalAndRejectsLateDraft(t *testing.T) {
	for _, kind := range []string{"session", "project"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "journal-delete.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			p, err := projectapp.New(store, store).Create(ctx, "project-key", "test", nil, project.Project{Name: "Delete"})
			if err != nil {
				t.Fatal(err)
			}
			s, err := sessionapp.New(store, store).Create(ctx, "session-key", "test", nil, session.Session{ProjectID: p.ID, Title: "Delete"})
			if err != nil {
				t.Fatal(err)
			}
			for _, pending := range []bool{false, true} {
				if err = store.PutChatTurn(ctx, s.ID, ulid.Make().String(), []byte(`{"persistDraft":"`+strings.Repeat("private answer", 30000)+`"}`), pending); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "session" {
				err = store.DeleteSession(ctx, s.ID)
			} else {
				err = store.DeleteProject(ctx, p.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			var parts int
			if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM chat_turn_checkpoint_parts`).Scan(&parts); err != nil || parts != 0 {
				t.Fatalf("deleted checkpoint parts retained: %d %v", parts, err)
			}
			raw, err := store.LatestChatTurn(ctx, s.ID)
			if err != nil || len(raw) != 0 {
				t.Fatalf("deleted data retained: %s %v", raw, err)
			}
			pending, err := store.PendingChatTurns(ctx, s.ID)
			if err != nil || len(pending) != 0 {
				t.Fatalf("deleted draft retained: %d %v", len(pending), err)
			}
			if err = store.PutChatTurn(ctx, s.ID, ulid.Make().String(), []byte(`{"persistDraft":"late"}`), true); err == nil {
				t.Fatal("late writer resurrected deleted data")
			}
		})
	}
}
