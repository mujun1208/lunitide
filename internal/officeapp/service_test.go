package officeapp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func studioServiceFixture(t *testing.T) (*Service, *sqlite.Store, domain.Task) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "studio-test.db")
	store, err := sqlite.OpenTemplated(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Every fixture owns this temporary database. No production profile,
	// desktop file, installed application or user session is accessed.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	projectID, sessionID := ulid.Make().String(), ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = raw.Exec(`INSERT INTO projects(id,name,project_code,status,created_at,updated_at,version) VALUES(?,?,?,'active',?,?,1)`, projectID, "Test", "ITM00001", now, now); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if _, err = raw.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,'active',?,?,1)`, sessionID, projectID, "Office integration", now, now); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if _, err = raw.Exec(`INSERT INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, projectID); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if _, err = raw.Exec(`INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES(?,0,0,0)`, sessionID); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	svc, err := New(store, filepath.Join(dir, "office"))
	if err != nil {
		t.Fatal(err)
	}
	// Never invoke a real converter while testing a missing component.
	svc.Renderer.Executable = filepath.Join(dir, "missing-soffice.exe")
	svc.Renderer.Run = func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error) {
		return commandworker.Outcome{}, os.ErrNotExist
	}
	task, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: sessionID, Title: "报告任务", Goal: "验证内容版本与导出"}, "fixture-task")
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, task
}

func shortWordSpec() content.Spec {
	return content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "工作报告", Blocks: []content.Block{{Type: "heading", Text: "进度"}, {Type: "paragraph", Text: "初始状态"}}}
}

func generatedWord(t *testing.T, s *Service, task domain.Task, key string) domain.Version {
	t.Helper()
	v, err := s.Generate(context.Background(), task.ID, "工作报告.docx", shortWordSpec(), key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func headOf(t *testing.T, s *sqlite.Store, taskID string) domain.Head {
	t.Helper()
	h, err := s.ListOfficeHeads(context.Background(), taskID)
	if err != nil || len(h) != 1 {
		t.Fatalf("head: %#v %v", h, err)
	}
	return h[0]
}

func textPatchFor(t *testing.T, v domain.Version, text string) content.PatchRequest {
	t.Helper()
	var inspection content.Inspection
	if err := json.Unmarshal(v.Index, &inspection); err != nil {
		t.Fatal(err)
	}
	for _, n := range inspection.Nodes {
		if n.Text == "初始状态" {
			return content.PatchRequest{Kind: content.DOCX, BaseSHA256: v.SHA256, Operations: []content.TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: text}}}
		}
	}
	t.Fatal("expected mapped source node missing")
	return content.PatchRequest{}
}

func TestServiceGeneratePatchRestoreAcceptanceKeepsImmutableVersions(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v1 := generatedWord(t, svc, task, "generate-1")
	if v1.ContentMode != "managed" || v1.Quality != "partial" {
		t.Fatalf("generation overstated quality: %#v", v1)
	}
	originalVersion, original, err := svc.ReadVersion(ctx, task.ID, v1.ID)
	if err != nil || originalVersion.SHA256 != digest(original) {
		t.Fatal(err)
	}
	h := headOf(t, store, task.ID)
	accepted, err := store.AcceptOfficeVersion(ctx, task.ID, v1.ArtifactID, v1.ID, h.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := svc.Patch(ctx, task.ID, v1.ID, accepted.Revision, textPatchFor(t, v1, "已完成复核"), "patch-1")
	if err != nil {
		t.Fatal(err)
	}
	var patchMetadata map[string]json.RawMessage
	if err := json.Unmarshal(v2.Spec, &patchMetadata); err != nil {
		t.Fatal(err)
	}
	if v2.ID == v1.ID || v2.VersionNo != 2 || v2.ContentMode != "imported" || patchMetadata["schemaVersion"] != nil || patchMetadata["blocks"] != nil || patchMetadata["changedParts"] == nil {
		t.Fatalf("patch retained stale managed spec or version: %#v", v2)
	}
	h = headOf(t, store, task.ID)
	if h.LatestVersionID != v2.ID || h.AcceptedVersionID != v1.ID {
		t.Fatalf("head and acceptance conflated: %#v", h)
	}
	preview, err := svc.Preview(ctx, task.ID, v2.ID)
	if err != nil || !strings.Contains(preview.Content, "已完成复核") || preview.PreviewBasis != "structure" {
		t.Fatalf("preview: %#v %v", preview, err)
	}
	v3, err := svc.Restore(ctx, task.ID, v1.ID, h.Revision, "restore-1")
	if err != nil {
		t.Fatal(err)
	}
	_, restored, err := svc.ReadVersion(ctx, task.ID, v3.ID)
	if err != nil || !bytes.Equal(restored, original) || v3.VersionNo != 3 {
		t.Fatal("restore did not create matching immutable snapshot", err)
	}
	_, unchanged, err := svc.ReadVersion(ctx, task.ID, v1.ID)
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("historical original changed", err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, v1.ArtifactID)
	if err != nil || len(versions) != 3 {
		t.Fatalf("history=%d %v", len(versions), err)
	}
	if headOf(t, store, task.ID).AcceptedVersionID != v1.ID {
		t.Fatal("restore silently accepted its new version")
	}
}

func TestServiceGenerateAndPatchRetryIdempotentAndConcurrentCAS(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	first := generatedWord(t, svc, task, "same-key")
	retry := generatedWord(t, svc, task, "same-key")
	if retry.ID != first.ID {
		t.Fatal("retry created a duplicate artifact")
	}
	changed := shortWordSpec()
	changed.Blocks[1].Text = "不同输入"
	if _, err := svc.Generate(ctx, task.ID, "工作报告.docx", changed, "same-key"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("idempotency key accepted changed input: %v", err)
	}
	h := headOf(t, store, task.ID)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, label := range []string{"writer-one", "writer-two"} {
		req := textPatchFor(t, first, label)
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			_, err := svc.Patch(ctx, task.ID, first.ID, h.Revision, req, label)
			results <- err
		}(label)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS success=%d conflict=%d", success, conflict)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, first.ArtifactID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("version count: %d %v", len(versions), err)
	}
}

func TestServiceImportScopeAndCorruptBlobAreRejected(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	source := generatedWord(t, svc, task, "source")
	_, data, err := svc.ReadVersion(ctx, task.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := svc.Import(ctx, task.ID, "", "导入.docx", data, "", 0, "import")
	if err != nil || imported.ContentMode != "imported" || imported.SHA256 != source.SHA256 {
		t.Fatalf("import: %#v %v", imported, err)
	}
	other, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: task.SessionID, Title: "另一任务"}, "another-task")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.ReadVersion(ctx, other.ID, source.ID); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-task read allowed: %v", err)
	}
	if _, err = svc.Import(ctx, task.ID, "", "../outside.docx", data, "", 0, "unsafe-name"); err == nil {
		t.Fatal("unsafe import name accepted")
	}
	if err = os.WriteFile(filepath.Join(svc.Root, "blobs", source.ContentRef), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.ReadVersion(ctx, task.ID, source.ID); err == nil {
		t.Fatal("corrupt blob read silently")
	}
	if _, err = svc.Import(ctx, task.ID, "", "再次导入.docx", data, "", 0, "corrupt-reuse"); err == nil {
		t.Fatal("corrupt content-addressed blob reused")
	}
}

func TestServiceMissingRendererIsPartialAndDoesNotInventPDF(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "source")
	q, err := svc.Check(ctx, task.ID, v.ID, true)
	if err != nil || q.Quality != "partial" {
		t.Fatalf("missing renderer: %#v %v", q, err)
	}
	if _, err = svc.ReadPDF(ctx, task.ID, v.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("nonexistent actual PDF returned: %v", err)
	}
	p, err := svc.Preview(ctx, task.ID, v.ID)
	if err != nil || p.PDFReady || p.PreviewBasis != "structure" {
		t.Fatalf("preview overstated evidence: %#v %v", p, err)
	}
	h := headOf(t, store, task.ID)
	if _, err = store.AcceptOfficeVersion(ctx, task.ID, v.ArtifactID, v.ID, h.Revision); err != nil {
		t.Fatal(err)
	}
	accepted, err := store.GetOfficeVersion(ctx, v.ID)
	if err != nil || accepted.Quality != "partial" {
		t.Fatal("acceptance incorrectly upgraded quality", err)
	}
}

func TestServiceExportNeverOverwritesExternalEditsAndPersistsReceipt(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "source")
	dir := t.TempDir()
	exported, err := svc.Export(ctx, task.ID, v.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exported)
	if err != nil || digest(data) != v.SHA256 {
		t.Fatal("export differs from selected version", err)
	}
	retry, err := svc.Export(ctx, task.ID, v.ID, dir)
	if err != nil || retry != exported {
		t.Fatal("export retry lost existing matching file", err)
	}
	receipts, err := store.ListOfficeStepReceipts(ctx, task.ID, "")
	if err != nil || len(receipts) != 1 || receipts[0].InputDigest != v.SHA256 {
		t.Fatalf("export receipt: %#v %v", receipts, err)
	}
	if err = os.WriteFile(exported, []byte("用户在导出副本中编辑"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Export(ctx, task.ID, v.ID, dir); err == nil {
		t.Fatal("external edit overwritten")
	}
	after, err := os.ReadFile(exported)
	if err != nil || string(after) != "用户在导出副本中编辑" {
		t.Fatal("external edit changed", err)
	}
	if _, err = svc.Export(ctx, task.ID, v.ID, "relative"); err == nil {
		t.Fatal("relative export path accepted")
	}
}

type onceFailingReceiptStore struct {
	domain.Store
	failed bool
}

func (s *onceFailingReceiptStore) AppendOfficeStepReceipt(ctx context.Context, r domain.StepReceipt) error {
	if !s.failed {
		s.failed = true
		return errors.New("injected receipt failure")
	}
	return s.Store.AppendOfficeStepReceipt(ctx, r)
}

func TestServiceExportRetryRepairsMissingReceipt(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "source")
	dir := t.TempDir()
	svc.Store = &onceFailingReceiptStore{Store: store}
	if _, err := svc.Export(ctx, task.ID, v.ID, dir); err == nil {
		t.Fatal("receipt failure was hidden")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("export file not retained after receipt failure: %#v %v", files, err)
	}
	exported, err := svc.Export(ctx, task.ID, v.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exported) != files[0].Name() {
		t.Fatal("retry created another export")
	}
	receipts, err := store.ListOfficeStepReceipts(ctx, task.ID, "")
	if err != nil || len(receipts) != 1 {
		t.Fatalf("retry did not repair receipt: %#v %v", receipts, err)
	}
}

func TestServicePDFGenerationRetriesSameImmutableVersion(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	spec := content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "可重试PDF", Body: "中文测试内容。"}
	v1, err := svc.Generate(ctx, task.ID, "内容.pdf", spec, "same-pdf")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := svc.Generate(ctx, task.ID, "内容.pdf", spec, "same-pdf")
	if err != nil || v1.ID != v2.ID {
		t.Fatalf("PDF idempotency failed: %s %s %v", v1.ID, v2.ID, err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, v1.ArtifactID)
	if err != nil || len(versions) != 1 {
		t.Fatal("duplicate PDF versions", err)
	}
	data, err := svc.ReadPDF(ctx, task.ID, v1.ID)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("PDF snapshot unavailable", err)
	}
}

func TestServiceRenderedPreviewBindsEvidenceToSelectedVersion(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v1 := generatedWord(t, svc, task, "source")
	pdf, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "排版测试夹具", Body: "这份文件是渲染器接口的受控测试输出，不代表本机安装了排版组件。"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.Renderer.Run = func(_ context.Context, spec commandworker.Spec, _ commandworker.StartGuard, onChunk func([]byte)) (commandworker.Outcome, error) {
		calls++
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			if onChunk != nil {
				onChunk([]byte("LibreOffice test-fixture"))
			}
			return commandworker.Outcome{ExitCode: 0}, nil
		}
		output := ""
		for i, arg := range spec.Args {
			if arg == "--outdir" && i+1 < len(spec.Args) {
				output = spec.Args[i+1]
			}
		}
		if output == "" || !strings.HasPrefix(filepath.Clean(output), filepath.Clean(svc.Renderer.Root)+string(filepath.Separator)) {
			return commandworker.Outcome{}, errors.New("fixture output escaped private root")
		}
		if err := os.WriteFile(filepath.Join(output, "source.pdf"), pdf, 0600); err != nil {
			return commandworker.Outcome{}, err
		}
		return commandworker.Outcome{ExitCode: 0}, nil
	}
	q, err := svc.Check(ctx, task.ID, v1.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if q.Quality != "partial" || q.SHA256 != v1.SHA256 || calls != 2 {
		t.Fatalf("rendering overclaimed overall target compatibility or wrong binding: %#v calls=%d", q, calls)
	}
	native := false
	for _, check := range q.Checks {
		if check.ID == "native_render" {
			native = true
			if check.Status != "passed" {
				t.Fatalf("actual render did not replace the missing-render check: %#v", check)
			}
		}
	}
	if !native {
		t.Fatal("missing native-render check")
	}
	actual, err := svc.ReadPDF(ctx, task.ID, v1.ID)
	if err != nil || !bytes.Equal(actual, pdf) {
		t.Fatal("rendered bytes not recoverable by selected immutable version", err)
	}
	p, err := svc.Preview(ctx, task.ID, v1.ID)
	if err != nil || !p.PDFReady {
		t.Fatal("actual PDF availability missing", err)
	}
	h := headOf(t, store, task.ID)
	v2, err := svc.Patch(ctx, task.ID, v1.ID, h.Revision, textPatchFor(t, v1, "新版本内容"), "patch")
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Preview(ctx, task.ID, v2.ID)
	if err != nil || p.PDFReady {
		t.Fatal("new version reused old rendering evidence", err)
	}
	if _, err = svc.ReadPDF(ctx, task.ID, v2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("old rendered PDF leaked to new version: %v", err)
	}
}
