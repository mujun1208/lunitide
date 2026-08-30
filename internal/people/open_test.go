package people_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestOpenFileStaysInInbox(t *testing.T) {
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "open.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	recv := filepath.Join(t.TempDir(), "recv")
	stage := filepath.Join(t.TempDir(), "stage")
	if err := os.MkdirAll(recv, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(recv, "note.txt")
	if err := os.WriteFile(inside, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	var opened string
	restore := people.ReplaceOpenPathForTest(func(path string) error { opened = path; return nil })
	t.Cleanup(restore)

	s := people.New(store, ident, recv, stage)
	if _, err := s.OpenFile(outside); err != people.ErrInvalid {
		t.Fatalf("outside = %v", err)
	}
	if opened != "" {
		t.Fatal("must not shell-open a path outside the inbox")
	}
	got, err := s.OpenFile(inside)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(inside) {
		t.Fatalf("opened %q want %q", got, inside)
	}
	if opened == "" {
		t.Fatal("expected open callback")
	}
}
