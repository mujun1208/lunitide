package agenthub

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestThreadPathAllowedAcceptsWorkspaceChild(t *testing.T) {
	workspace := t.TempDir()
	candidate := filepath.Join(workspace, "src", "main.go")
	if !PathAllowed(workspace, "", candidate) {
		t.Fatalf("workspace child should be allowed: %s", candidate)
	}
}

func TestThreadPathAllowedRejectsWindowsIniEscape(t *testing.T) {
	workspace := t.TempDir()
	if PathAllowed(workspace, "", `..\Windows\win.ini`) {
		t.Fatal(`..\Windows\win.ini should be rejected`)
	}
}

func TestThreadPathAllowedAcceptsExportChildOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	export := t.TempDir()
	candidate := filepath.Join(export, "deck.pptx")
	if !PathAllowed(workspace, export, candidate) {
		t.Fatalf("export child should be allowed: %s", candidate)
	}
}

func TestThreadPathAllowedEmptyExportDoesNotWiden(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if PathAllowed(workspace, "", outside) {
		t.Fatalf("empty export must not allow %s", outside)
	}
}

func TestThreadPathAllowedRejectsOutsideBothRoots(t *testing.T) {
	workspace := t.TempDir()
	export := t.TempDir()
	outside := filepath.Join(t.TempDir(), "other.txt")
	if PathAllowed(workspace, export, outside) {
		t.Fatalf("path outside workspace and export should be rejected: %s", outside)
	}
}

func TestDefaultThreadDirJoinsThreadsID(t *testing.T) {
	root := t.TempDir()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAE"
	got := DefaultThreadDir(root, id)
	want := root + string(filepath.Separator) + "threads" + string(filepath.Separator) + id
	if got != want {
		t.Fatalf("DefaultThreadDir = %q, want %q", got, want)
	}
}

func TestThreadStoreInsertGetListUpdate(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	cursor := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "cursor", "Draft", false)
	codex := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAF", "codex", "Fix build", false)
	if err := store.Insert(cursor); err != nil {
		t.Fatal(err)
	}
	if err := store.Insert(codex); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(cursor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != cursor {
		t.Fatalf("Get = %#v, want %#v", got, cursor)
	}

	all, err := store.List(ThreadFilter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("List all = %d %v", len(all), err)
	}
	filtered, err := store.List(ThreadFilter{HarnessID: "cursor"})
	if err != nil || len(filtered) != 1 || filtered[0].ID != cursor.ID {
		t.Fatalf("List harness = %#v %v", filtered, err)
	}

	if err = store.Update(cursor.ID, "Pinned deck", true); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(cursor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Pinned deck" || !updated.Pinned {
		t.Fatalf("title/pinned = %q %v", updated.Title, updated.Pinned)
	}
	if updated.Status != cursor.Status || updated.Scene != cursor.Scene || updated.WorkspaceRoot != cursor.WorkspaceRoot {
		t.Fatalf("Update changed more than title/pinned: %#v", updated)
	}
}

func TestThreadStoreDeleteCascadesMessages(t *testing.T) {
	db := openThreadDB(t)
	store := NewThreadStore(db)
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "cursor", "Cascade", false)
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO agent_hub_messages(id, thread_id, seq, role, content, created_at)
VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAF', ?, 1, 'user', 'hello', '2026-09-13T01:00:00Z')`, thread.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err = store.Delete(thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(thread.ID); err != ErrNotFound {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM agent_hub_messages WHERE thread_id=?`, thread.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("cascaded messages remaining = %d", n)
	}
}

func openThreadDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "threads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("0154_agent_hub_threads.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	return db
}

func sampleThread(id, harness, title string, pinned bool) ThreadRecord {
	return ThreadRecord{
		ID:            id,
		HarnessID:     harness,
		Title:         title,
		Pinned:        pinned,
		WorkspaceRoot: `C:\proj\demo`,
		Scene:         "write_project",
		Status:        "idle",
		AccessMode:    "approval",
		CreatedAt:     "2026-09-13T01:00:00Z",
		UpdatedAt:     "2026-09-13T01:00:00Z",
	}
}
