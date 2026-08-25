package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

func TestSessionExpertMountsReplaceAndList(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "session-experts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := projectapp.New(store, store).Create(ctx, "proj-experts", "test", nil, project.Project{Name: "Experts"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessionapp.New(store, store).Create(ctx, "sess-experts", "test", nil, session.Session{ProjectID: p.ID, Title: "Chat"})
	if err != nil {
		t.Fatal(err)
	}
	a, b := ulid.Make().String(), ulid.Make().String()
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListSessionExpertIDs(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("got %#v", got)
	}
	if err := store.ReplaceSessionExpertIDs(ctx, s.ID, []string{b}); err != nil {
		t.Fatal(err)
	}
	got, err = store.ListSessionExpertIDs(ctx, s.ID)
	if err != nil || len(got) != 1 || got[0] != b {
		t.Fatalf("replaced %#v %v", got, err)
	}
	if err := store.DeleteSession(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	got, err = store.ListSessionExpertIDs(ctx, s.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("after delete %#v %v", got, err)
	}
}
