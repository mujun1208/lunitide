package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

type frozenDraftCandidates struct {
	*Store
	ids []string
}

func (r frozenDraftCandidates) ListEmptyDraftSessionIDs(context.Context, string, []string, int) ([]string, error) {
	return append([]string{}, r.ids...), nil
}

func TestReclaimEmptyDraftRechecksEligibilityAfterCandidateSelection(t *testing.T) {
	for _, kind := range []string{"message-arrived", "office-bound", "attachment-arrived", "pinned", "renamed", "different-project", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "reclaim.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			projects := projectapp.New(store, store)
			p, err := projects.Create(ctx, "project", "test", nil, project.Project{Name: "Project"})
			if err != nil {
				t.Fatal(err)
			}
			sessions := sessionapp.New(store, store)
			draft, err := sessions.Create(ctx, "draft", "desktop-host", nil, session.Session{ProjectID: p.ID, Title: "新对话"})
			if err != nil {
				t.Fatal(err)
			}
			ids, err := store.ListEmptyDraftSessionIDs(ctx, p.ID, nil, 100)
			if err != nil || len(ids) != 1 || ids[0] != draft.ID {
				t.Fatalf("candidate: %v %v", ids, err)
			}
			reclaimProjectID := p.ID
			switch kind {
			case "message-arrived":
				messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := messages.Append(ctx, "new-message", "desktop-host", nil, message.Message{SessionID: draft.ID, Text: "这条消息必须保留"}); err != nil {
					t.Fatal(err)
				}
			case "office-bound":
				if _, err := store.CreateOfficeTask(ctx, officestudio.Task{SessionID: draft.ID, Title: "Office task", Goal: "Keep bound session"}, "office-bind"); err != nil {
					t.Fatal(err)
				}
			case "attachment-arrived":
				err := store.CreateAttachment(ctx, attachment.Attachment{ID: ulid.Make().String(), ProjectID: p.ID, SessionID: draft.ID, FileRef: "temp-file", OriginalName: "user.txt", MIME: "text/plain", SHA256: strings.Repeat("a", 64), ParseStatus: attachment.StatusPending, CreatedAt: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
			case "pinned", "renamed":
				title := draft.Title
				if kind == "renamed" {
					title = "Important conversation"
				}
				if _, err := sessions.Update(ctx, "protect-session", "desktop-host", nil, draft.ID, draft.Version, title, kind == "pinned"); err != nil {
					t.Fatal(err)
				}
			case "different-project":
				reclaimProjectID = ulid.Make().String()
			}
			// Freeze the stale result from before the real write. This reliably
			// reproduces the race window without timing-sensitive goroutines.
			reclaimer := sessionapp.New(frozenDraftCandidates{Store: store, ids: ids}, store)
			reclaimer.SetDeleter(store)
			freed, err := reclaimer.ReclaimEmptyDrafts(ctx, reclaimProjectID, 100)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unchanged" {
				if freed != 1 {
					t.Fatalf("ordinary empty draft not reclaimed: %d", freed)
				}
				if _, err := sessions.Get(ctx, draft.ID); err == nil {
					t.Fatal("deleted draft still exists")
				}
				var count int
				if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE aggregate_id=? AND action='session.deleted' AND actor='draft-reclaimer'`, draft.ID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("deletion lost original audit flow: %d %v", count, err)
				}
			} else {
				if freed != 0 {
					t.Fatalf("stale candidate erased occupied session: %d", freed)
				}
				if _, err := sessions.Get(ctx, draft.ID); err != nil {
					t.Fatalf("protected session lost: %v", err)
				}
				if deleted, err := store.HasTombstone(ctx, "session", draft.ID); err != nil || deleted {
					t.Fatalf("skipped candidate got deletion marker: %t %v", deleted, err)
				}
				if kind == "message-arrived" {
					var count int
					if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id=?`, draft.ID).Scan(&count); err != nil || count != 1 {
						t.Fatalf("new message lost: %d %v", count, err)
					}
				}
			}
		})
	}
}
