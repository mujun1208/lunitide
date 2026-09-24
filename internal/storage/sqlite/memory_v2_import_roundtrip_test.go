package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestMemoryImportRoundTripCopiesAFact(t *testing.T) {
	dir := t.TempDir()
	src, err := OpenTemplated(context.Background(), filepath.Join(dir, "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	ctx := context.Background()
	if _, _, err := src.SealMemoryArchive(ctx, "", "user", "", []byte(`{}`)); err == nil {
		t.Fatal("empty subject must fail")
	}
	if _, _, err := src.SealMemoryArchive(ctx, "local-user", "user", "", nil); err == nil {
		t.Fatal("empty archive must fail")
	}
	if _, err := src.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "local-user", ScopeKind: "user", Text: "我喜欢绿茶",
		OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	exported, err := src.ExportCanonicalMemoryArchive(ctx, "local-user")
	if err != nil || exported.ArtifactID == "" || exported.Counts.Current != 1 {
		t.Fatalf("export=%+v err=%v", exported, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "memory-cas", exported.ArtifactID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := OpenTemplated(context.Background(), filepath.Join(dir, "dst.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dst.Close() })
	badID, _, err := dst.SealMemoryArchive(ctx, "local-user", "user", "", []byte(`{"schema":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dst.PreviewMemoryImport(ctx, "local-user", badID, "bad-op", "bad-op"); !errors.Is(err, m8core.ErrImportSchemaUnsupported) {
		t.Fatalf("bad schema: %v", err)
	}
	artifactID, _, err := dst.SealMemoryArchive(ctx, "local-user", "user", "", raw)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := dst.PreviewMemoryImport(ctx, "local-user", artifactID, "imp-1", "imp-1")
	if err != nil || preview.Counts.Accepted != 1 || preview.DatabaseRevision < 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	replay, err := dst.PreviewMemoryImport(ctx, "local-user", artifactID, "imp-1", "imp-1")
	if err != nil || !replay.Replay || replay.PreviewID != preview.PreviewID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := dst.CommitMemoryImport(ctx, "local-user", preview.PreviewID, preview.ArchiveDigest, preview.ManifestDigest, "commit-1", "commit-1", preview.DatabaseRevision+9); !errors.Is(err, m8core.ErrRevisionConflict) {
		t.Fatalf("stale commit revision: %v", err)
	}
	committed, err := dst.CommitMemoryImport(ctx, "local-user", preview.PreviewID, preview.ArchiveDigest, preview.ManifestDigest, "commit-1", "commit-1", preview.DatabaseRevision)
	if err != nil || committed.ImportedCount != 1 {
		t.Fatalf("commit=%+v err=%v", committed, err)
	}
	again, err := dst.CommitMemoryImport(ctx, "local-user", preview.PreviewID, preview.ArchiveDigest, preview.ManifestDigest, "commit-1", "commit-1", preview.DatabaseRevision)
	if err != nil || !again.Replay {
		t.Fatalf("commit replay=%+v err=%v", again, err)
	}
	rows, err := dst.ListCanonicalMemoryRecords(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(rows) != 1 || rows[0].Text != "我喜欢绿茶" {
		t.Fatalf("copied=%+v err=%v", rows, err)
	}
}
