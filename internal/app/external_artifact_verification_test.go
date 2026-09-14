package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/doctext"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/migrations"
)

func TestExternalExecutorArtifactVerification(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "ext-verify")
	workspace := t.TempDir()
	policy := domain.DeliveryPolicy{Revision: "office-basic-v2"}

	t.Run("path_escapes_workspace", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "secret.docx")
		if err := os.WriteFile(outside, []byte("PK"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := e.VerifyExternalExecutorArtifact(context.Background(), ExternalArtifactClaim{
			Workspace: workspace,
			Path:      filepath.Join(workspace, "..", filepath.Base(outside)),
			ExitCode:  0,
			Kind:      "docx",
		})
		if err == nil && got.Delivered {
			t.Fatalf("escaped path must not deliver: %+v", got)
		}
		if got.Reason != "path_escape" {
			t.Fatalf("reason=%q, want path_escape (err=%v)", got.Reason, err)
		}
	})

	t.Run("file_does_not_exist", func(t *testing.T) {
		got, err := e.VerifyExternalExecutorArtifact(context.Background(), ExternalArtifactClaim{
			Workspace: workspace,
			Path:      filepath.Join(workspace, "missing.docx"),
			ExitCode:  0,
			Kind:      "docx",
		})
		if err == nil && got.Delivered {
			t.Fatalf("missing file must not deliver: %+v", got)
		}
		if got.Reason != "missing_file" {
			t.Fatalf("reason=%q, want missing_file (err=%v)", got.Reason, err)
		}
	})

	t.Run("exit_0_damaged_document", func(t *testing.T) {
		broken := filepath.Join(workspace, "broken.docx")
		if err := os.WriteFile(broken, []byte("not-a-package"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := e.VerifyExternalExecutorArtifact(context.Background(), ExternalArtifactClaim{
			Workspace: workspace,
			Path:      broken,
			ExitCode:  0,
			Kind:      "docx",
		})
		if err == nil && got.Delivered {
			t.Fatalf("exit 0 damaged document must not deliver: %+v", got)
		}
		if got.Reason != "inspect_failed" {
			t.Fatalf("reason=%q, want inspect_failed (err=%v)", got.Reason, err)
		}
	})

	t.Run("usage_missing_is_unknown", func(t *testing.T) {
		v, err := e.officeStudio.Generate(context.Background(), task.ID, "用量未知.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "用量未知", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}},
		}, "ext-usage-unknown-doc")
		if err != nil {
			t.Fatal(err)
		}
		_, blob, err := e.officeStudio.ReadVersion(context.Background(), task.ID, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(workspace, "unused.docx")
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := e.VerifyExternalExecutorArtifact(context.Background(), ExternalArtifactClaim{
			Workspace: workspace,
			Path:      path,
			ExitCode:  0,
			Kind:      "docx",
		})
		if got.UsageIntegrity != "unknown" {
			t.Fatalf("missing usage must be unknown, not zero-success: %+v err=%v", got, err)
		}
		if got.Delivered {
			t.Fatal("unknown usage must not count as delivered")
		}
		if got.Reason != "usage_unknown" {
			t.Fatalf("reason=%q, want usage_unknown (err=%v)", got.Reason, err)
		}
	})

	t.Run("valid_artifact_requires_same_formal_decision", func(t *testing.T) {
		v, err := e.officeStudio.Generate(context.Background(), task.ID, "外部.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "外部", Blocks: []content.Block{{Type: "paragraph", Text: "外部正文"}},
		}, "ext-valid-doc")
		if err != nil {
			t.Fatal(err)
		}
		_, blob, err := e.officeStudio.ReadVersion(context.Background(), task.ID, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(workspace, "valid.docx")
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		rawSum := sha256.Sum256(blob)
		rawSHA := hex.EncodeToString(rawSum[:])
		extracted, err := doctext.Extract("valid.docx", blob, "")
		if err != nil {
			t.Fatal(err)
		}
		extractSum := sha256.Sum256([]byte(extracted.Text))
		if rawSHA == hex.EncodeToString(extractSum[:]) {
			t.Fatal("file-exists snapshot must not reuse extract SHA")
		}
		tokens := int64(12)
		got, err := e.VerifyExternalExecutorArtifact(context.Background(), ExternalArtifactClaim{
			Workspace:   workspace,
			Path:        path,
			ExitCode:    0,
			Kind:        "docx",
			UsageTokens: &tokens,
			TaskID:      task.ID,
			VersionID:   v.ID,
			Policy:      policy,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.FileSHA256 != rawSHA {
			t.Fatalf("existing file SHA %s want raw blob %s", got.FileSHA256, rawSHA)
		}
		if got.Delivered {
			t.Fatalf("valid file still needs FormalDecision: %+v", got)
		}
		want, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v.ID, policy)
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision.DecisionID != want.DecisionID || got.Decision.Allowed != want.Allowed || got.Decision.State != want.State {
			t.Fatalf("external FormalDecision drifted: %+v vs %+v", got.Decision, want)
		}
		export := officeCall(t, e, "office.artifact.export", "ext-formal-export", map[string]any{
			"taskId": task.ID, "versionId": v.ID, "deliveryMode": "formal",
		})
		if export.OK {
			t.Fatal("unseeded formal export must fail")
		}
		exportDec := formalDecisionFromError(t, export)
		if exportDec.DecisionID != got.Decision.DecisionID || exportDec.Allowed != got.Decision.Allowed || exportDec.State != got.Decision.State {
			t.Fatalf("external vs office export decision: %+v vs %+v", got.Decision, exportDec)
		}
		_ = store
	})

	t.Run("hub_copy_error_is_not_silent", func(t *testing.T) {
		threads := openHubThreadStore(t)
		exportDir := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(exportDir, []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		thread := agenthub.ThreadRecord{
			ID:            "01ARZ3NDEKTSV4RRFFQ69G5FAE",
			HarnessID:     "loopback",
			Title:         "Export",
			WorkspaceRoot: workspace,
			ExportDir:     exportDir,
			Scene:         "write_project",
			Status:        "idle",
			AccessMode:    "approval",
			CreatedAt:     "2026-09-13T01:00:00Z",
			UpdatedAt:     "2026-09-13T01:00:00Z",
		}
		if err := threads.Insert(thread); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "deck.pptx"), []byte("pptx-body"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := agenthub.MarkThreadSuccess(threads, thread.ID); err != nil {
			t.Fatal(err)
		}
		got, err := threads.Get(thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "success" {
			t.Fatalf("exit/status success may stay, but copy must not be silent: %q", got.Status)
		}
		events, err := threads.ListEvents(thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range events {
			if ev.Type == "export_error" && ev.Detail != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("hub copy error must be visible: %#v", events)
		}
	})

	t.Run("hub_copied_damaged_document_is_not_delivered", func(t *testing.T) {
		threads := openHubThreadStore(t)
		exportDir := t.TempDir()
		thread := agenthub.ThreadRecord{
			ID:            "01ARZ3NDEKTSV4RRFFQ69G5FAF",
			HarnessID:     "loopback",
			Title:         "Damaged",
			WorkspaceRoot: workspace,
			ExportDir:     exportDir,
			Scene:         "write_project",
			Status:        "idle",
			AccessMode:    "approval",
			CreatedAt:     "2026-09-13T01:00:00Z",
			UpdatedAt:     "2026-09-13T01:00:00Z",
		}
		if err := threads.Insert(thread); err != nil {
			t.Fatal(err)
		}
		broken := filepath.Join(workspace, "broken-hub.pptx")
		if err := os.WriteFile(broken, []byte("not-a-package"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := agenthub.MarkThreadSuccess(threads, thread.ID); err != nil {
			t.Fatal(err)
		}
		got, err := threads.Get(thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "success" {
			t.Fatalf("status may stay success: %q", got.Status)
		}
		if _, err = os.Stat(filepath.Join(exportDir, "broken-hub.pptx")); err != nil {
			t.Fatalf("damaged file still copies: %v", err)
		}
		events, err := threads.ListEvents(thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range events {
			if ev.Type == "artifact_unverified" && strings.Contains(ev.Detail, "inspect_failed") {
				found = true
			}
		}
		if !found {
			t.Fatalf("copied damaged file must not look delivered: %#v", events)
		}
	})
}

func openHubThreadStore(t *testing.T) *agenthub.ThreadStore {
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
	return agenthub.NewThreadStore(db)
}
