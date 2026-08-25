package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/attachment"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestCreateAttachmentRejectsCrossProjectSessionAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects := projectapp.New(store, store)
	p1, _ := projects.Create(ctx, "p1", "test", nil, project.Project{Name: "One"})
	p2, _ := projects.Create(ctx, "p2", "test", nil, project.Project{Name: "Two"})
	s, _ := sessionapp.New(store, store).Create(ctx, "s1", "test", nil, session.Session{ProjectID: p2.ID, Title: "Other"})
	a := attachment.Attachment{ID: ulid.Make().String(), ProjectID: p1.ID, SessionID: s.ID, FileRef: "ref", OriginalName: "a.txt", MIME: "text/plain", Size: 1, SHA256: strings.Repeat("a", 64), ParseStatus: attachment.StatusPending, CreatedAt: time.Now().UTC()}
	if err := store.CreateAttachment(ctx, a); !errors.Is(err, attachmentapp.ErrScopeMismatch) {
		t.Fatalf("error = %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE id=?`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial insert count=%d err=%v", count, err)
	}
}

func TestDeleteSessionRemovesOnlySessionAttachmentsAndSchedulesCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "delete-scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, _ := projectapp.New(store, store).Create(ctx, "p", "test", nil, project.Project{Name: "Project"})
	s, _ := sessionapp.New(store, store).Create(ctx, "s", "test", nil, session.Session{ProjectID: p.ID, Title: "Session"})
	for _, a := range []attachment.Attachment{
		{ID: ulid.Make().String(), ProjectID: p.ID, SessionID: s.ID, FileRef: "session-ref", OriginalName: "s.txt", MIME: "text/plain", SHA256: strings.Repeat("a", 64), ParseStatus: attachment.StatusPending, CreatedAt: time.Now().UTC()},
		{ID: ulid.Make().String(), ProjectID: p.ID, FileRef: "project-ref", OriginalName: "p.txt", MIME: "text/plain", SHA256: strings.Repeat("b", 64), ParseStatus: attachment.StatusPending, CreatedAt: time.Now().UTC()},
	} {
		if err := store.CreateAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DeleteSession(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	var sessionScoped, projectScoped int
	_ = store.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE file_ref='session-ref'`).Scan(&sessionScoped)
	_ = store.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE file_ref='project-ref' AND session_id IS NULL`).Scan(&projectScoped)
	if sessionScoped != 0 || projectScoped != 1 {
		t.Fatalf("session=%d project=%d", sessionScoped, projectScoped)
	}
	refs, err := store.ListPendingAttachmentFileCleanup(ctx, 10)
	if err != nil || len(refs) != 1 || refs[0] != "session-ref" {
		t.Fatalf("cleanup=%v err=%v", refs, err)
	}
}

func TestGetAttachmentForDeletionHidesParsedTextAndFindsTombstone(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "attachment-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, _ := projectapp.New(store, store).Create(ctx, "p", "test", nil, project.Project{Name: "Project"})
	a := attachment.Attachment{
		ID: ulid.Make().String(), ProjectID: p.ID, FileRef: "secret-ref", OriginalName: "secret.txt",
		MIME: "text/plain", SHA256: strings.Repeat("a", 64), ParseStatus: attachment.StatusSucceeded,
		ParsedText: "sensitive parsed text", ParsedTextBytes: 21, CreatedAt: time.Now().UTC(),
	}
	if err := store.CreateAttachment(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAttachment(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAttachmentForDeletion(ctx, a.ID)
	if err != nil || got == nil || got.DeletedAt == nil {
		t.Fatalf("deleted lookup = %#v, %v", got, err)
	}
	if got.ParsedText != "" {
		t.Fatalf("administrative deletion lookup exposed parsed text %q", got.ParsedText)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM attachments WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetAttachmentForDeletion(ctx, a.ID)
	if err != nil || got == nil || got.DeletedAt == nil {
		t.Fatalf("tombstone lookup = %#v, %v", got, err)
	}
	missing, err := store.GetAttachmentForDeletion(ctx, ulid.Make().String())
	if err != nil || missing != nil {
		t.Fatalf("never-existing lookup = %#v, %v; want nil, nil", missing, err)
	}
}
