package m8app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryImportExport(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	src, err := storage.OpenTemplated(context.Background(), srcPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	svc := m8app.NewMemoryService(src.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	created, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢绿茶", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := svc.ExportCanonicalMemoryArchive(ctx, "local-user")
	if err != nil || exported.Format != "fabric_v2" || exported.ArtifactID == "" || exported.Counts.Current != 1 || exported.Counts.Tombstones != 0 {
		t.Fatalf("export %+v err=%v", exported, err)
	}
	before := created.DatabaseRevision
	preview, err := svc.PreviewMemoryImport(ctx, "local-user", exported.ArtifactID, ulid.Make().String(), ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	listed, _, err := svc.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(listed) != 1 || listed[0].Text != "我喜欢绿茶" {
		t.Fatalf("preview must not write active facts %+v err=%v", listed, err)
	}
	if preview.Counts.Conflicts != 1 {
		t.Fatalf("active conflict counts %+v", preview.Counts)
	}

	dstPath := filepath.Join(dir, "dst.db")
	dst, err := storage.OpenTemplated(context.Background(), dstPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dst.Close() })
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(srcPath), "memory-cas", exported.ArtifactID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Schema  string `json:"schema"`
		Records []struct {
			Text      string `json:"text"`
			Forgotten bool   `json:"forgotten"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &archive); err != nil || archive.Schema != "lunitide.memory.fabric_v2" || len(archive.Records) != 1 || archive.Records[0].Text != "我喜欢绿茶" {
		t.Fatalf("archive %+v err=%v", archive, err)
	}
	other := m8app.NewMemoryService(dst.AgentRuntimeRepository(), "local-user")
	artifactID, _, err := other.SealMemoryArchive(ctx, "local-user", "user", "", raw)
	if err != nil {
		t.Fatal(err)
	}
	otherPreview, err := other.PreviewMemoryImport(ctx, "local-user", artifactID, ulid.Make().String(), ulid.Make().String())
	if err != nil || otherPreview.Counts.Accepted != 1 {
		t.Fatalf("other preview %+v err=%v", otherPreview, err)
	}
	committed, err := other.CommitMemoryImport(ctx, "local-user", otherPreview.PreviewID, otherPreview.ArchiveDigest, otherPreview.ManifestDigest, ulid.Make().String(), ulid.Make().String(), otherPreview.DatabaseRevision)
	if err != nil || committed.ImportedCount != 1 {
		t.Fatalf("commit %+v err=%v", committed, err)
	}
	copied, _, err := other.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(copied) != 1 || copied[0].Text != "我喜欢绿茶" {
		t.Fatalf("round-trip %+v err=%v", copied, err)
	}
	if listed[0].FactID == "" || before < 1 {
		t.Fatal("source fact missing")
	}
}

func TestMemoryRollback(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ops := m8app.NewMemoryOpsService(store)
	ctx := context.Background()
	if _, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{SubjectID: "local-user", MemoryEnabled: true, CaptureMode: "off", GrowthDays: 14}); err != nil {
		t.Fatal(err)
	}
	listed, _, err := svc.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(listed) != 1 || listed[0].Text != "我喜欢简洁的回答" {
		t.Fatalf("off must keep canonical reader %+v err=%v", listed, err)
	}
}
