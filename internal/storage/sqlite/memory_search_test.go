package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func TestMemoryFTSMigrationIsLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0121_memory_fts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0121 must be LF; CRLF changes the checksum")
	}
}

func TestSearchMemoriesFTSRanksAndExceedsOldLimit(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "memory-search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projectID := ulid.Make().String()
	now := "2026-01-01T00:00:00Z"
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?, 'ITM00001', ?,?)`,
		projectID, "p", now, now); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	mk := func(key, content string, conf memory.Confidence) {
		if _, err := store.CreateMemory(ctx, memory.Memory{
			ProjectID: projectID, Layer: memory.LayerSemantic, Scope: memory.ScopeProject,
			Key: key, Content: content, Confidence: conf,
		}); err != nil {
			t.Fatalf("create memory: %v", err)
		}
	}

	// 150 matching memories to exceed the old hard-coded 100-row ceiling.
	for i := 0; i < 150; i++ {
		mk("fact", "the sky is blue today", 0.5)
	}
	// One high-confidence match to verify confidence ordering.
	mk("weather", "the sky is very blue and clear", 0.99)

	hits, err := store.SearchMemoriesFTS(ctx, projectID, "sky blue", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) <= 100 {
		t.Fatalf("expected >100 hits (old ceiling removed), got %d", len(hits))
	}
	if hits[0].Confidence != 0.99 {
		t.Fatalf("expected highest confidence first, got %v", hits[0].Confidence)
	}
}