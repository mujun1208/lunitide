package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/migrations"
)

func TestProjectRootTreeMigrationLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0155_project_root_tree.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0155_project_root_tree.sql must be LF")
	}
}

func TestProjectFactoryMigrationLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0156_project_factory.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0156_project_factory.sql must be LF")
	}
}

func TestProjectRootTreeSchemaDump(t *testing.T) {
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "project-root.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var sqlText string
	if err = store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='table' AND name='projects'`).Scan(&sqlText); err != nil {
		t.Fatal(err)
	}
	t.Logf("table:projects %q", sqlText)
	if err = store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='index' AND name='idx_projects_root_path'`).Scan(&sqlText); err != nil {
		t.Fatal(err)
	}
	t.Logf("index:idx_projects_root_path %q", sqlText)
}
