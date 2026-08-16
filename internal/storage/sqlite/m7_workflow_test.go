package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

func newM7WorkflowService(t *testing.T) (*m7app.WorkflowService, *Store, string) {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "m7.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := m7app.NewWorkflowService(store.AgentRuntimeRepository())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	pid := "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	if _, err := store.db.Exec(`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?, 'ITM00001', ?,?)`,
		pid, "m7-project", now, now); err != nil {
		t.Fatal(err)
	}
	return svc, store, pid
}

func TestM7CreateVersionSeedsNineStages(t *testing.T) {
	svc, _, pid := newM7WorkflowService(t)
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != 1 || v.Status != m7flow.WVDraft || len(v.DefinitionDigest) != 64 {
		t.Fatalf("unexpected version: %#v", v)
	}
	v2, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Fatalf("second version must be 2, got %d", v2.Version)
	}
	if v2.DefinitionDigest != v.DefinitionDigest {
		t.Fatal("identical definitions must share the digest")
	}
	if _, err := svc.CreateVersion(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FZZ"); !errors.Is(err, m7app.ErrProjectNotFound) {
		t.Fatalf("missing project must fail with ErrProjectNotFound, got %v", err)
	}
}

func TestM7PublishFreezesVersion(t *testing.T) {
	svc, store, pid := newM7WorkflowService(t)
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != m7flow.WVPublished || published.PublishedAt == nil {
		t.Fatalf("publish failed: %#v", published)
	}
	again, err := svc.Publish(ctx, v.ID)
	if err != nil {
		t.Fatalf("republish must be idempotent: %v", err)
	}
	if again.PublishedAt == nil || !again.PublishedAt.Equal(*published.PublishedAt) {
		t.Fatal("idempotent republish must keep the original timestamp")
	}
	// WF-001 at the DB layer: mutating the definition digest of a published
	// version trips trigger M7-WF-001.
	if _, err := store.db.Exec(`UPDATE workflow_versions SET definition_digest=? WHERE id=?`,
		strings.Repeat("0", 64), v.ID); err == nil {
		t.Fatal("published version digest mutation must trip M7-WF-001")
	} else if !strings.Contains(err.Error(), "M7-WF-001") {
		t.Fatalf("expected M7-WF-001 trigger, got %v", err)
	}
}

func TestM7StartStageGuardsDAGAndAttempts(t *testing.T) {
	svc, _, pid := newM7WorkflowService(t)
	ctx := context.Background()
	if _, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation)); !errors.Is(err, m7app.ErrNotPublished) {
		t.Fatalf("start before publish must fail with ErrNotPublished, got %v", err)
	}
	v, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	first, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewRun || first.Run.State != m7flow.RunReady || first.Run.AttemptNo != 1 {
		t.Fatalf("unexpected first run: %#v", first)
	}
	again, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if again.NewRun || again.Run.ID != first.Run.ID {
		t.Fatal("second startStage must answer the active attempt idempotently")
	}
	// Upstream not completed: research stays draft with Dependent=true.
	research, err := svc.StartStage(ctx, pid, string(m7flow.StageResearch))
	if err != nil {
		t.Fatal(err)
	}
	if research.Run.State != m7flow.RunDraft || !research.Dependent {
		t.Fatalf("research must stay draft until initiation completes: %#v", research)
	}
	if _, err := svc.StartStage(ctx, pid, "security"); err == nil {
		t.Fatal("subprocess key must be rejected as a stage")
	}
}

func TestM7TransitionAndAttemptChaining(t *testing.T) {
	svc, _, pid := newM7WorkflowService(t)
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionStage(ctx, run.Run.ID, m7flow.RunCompleted); !errors.Is(err, m7app.ErrIllegalTransition) {
		t.Fatalf("ready -> completed must be illegal, got %v", err)
	}
	if _, err := svc.TransitionStageChecked(ctx, run.Run.ID, m7flow.RunRunning, 99); !errors.Is(err, m7app.ErrVersionConflict) {
		t.Fatalf("stale lock must fail with ErrVersionConflict, got %v", err)
	}
	running, err := svc.TransitionStage(ctx, run.Run.ID, m7flow.RunRunning)
	if err != nil {
		t.Fatal(err)
	}
	if running.LockVersion != run.Run.LockVersion+1 || running.StartedAt == nil {
		t.Fatalf("running must bump lock and stamp started_at: %#v", running)
	}
	if _, err := svc.TransitionStage(ctx, running.ID, m7flow.RunWaitingReview); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionStage(ctx, running.ID, m7flow.RunApproved); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.TransitionStage(ctx, running.ID, m7flow.RunCompleted)
	if err != nil {
		t.Fatal(err)
	}
	if completed.CompletedAt == nil {
		t.Fatal("terminal transition must stamp completed_at")
	}
	if !m7flow.IsTerminalRun(completed.State) {
		t.Fatal("completed is terminal")
	}
	// After completion the downstream stage becomes ready and a fresh attempt
	// of the completed stage may start (attempt 2).
	research, err := svc.StartStage(ctx, pid, string(m7flow.StageResearch))
	if err != nil {
		t.Fatal(err)
	}
	if research.Run.State != m7flow.RunReady || research.Dependent {
		t.Fatalf("research must be ready after initiation completes: %#v", research)
	}
	retry, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if !retry.NewRun || retry.Run.AttemptNo != 2 {
		t.Fatalf("completed stage allows a fresh attempt: %#v", retry)
	}
}

func TestM7CaptureInputSnapshots(t *testing.T) {
	svc, _, pid := newM7WorkflowService(t)
	ctx := context.Background()
	v, err := svc.CreateVersion(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartStage(ctx, pid, string(m7flow.StageInitiation))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.CaptureInput(ctx, run.Run.ID, map[string]any{"brief": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Digest) != 64 || snap.InputsJSON != `{"brief":"v1"}` {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
	// Idempotent recapture on identical content.
	same, err := svc.CaptureInput(ctx, run.Run.ID, map[string]any{"brief": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if same.ID != snap.ID {
		t.Fatal("identical recapture must answer the existing snapshot")
	}
	// M6 final-tree adaptation (SNP-001): rootId/digest pair required.
	bad := map[string]any{"m6.finalTree": map[string]any{"rootId": "01ARZ3NDEKTSV4RRFFQ69G5FA9"}}
	if _, err := svc.CaptureInput(ctx, run.Run.ID, bad); !errors.Is(err, m7app.ErrM6TreeDigest) {
		t.Fatalf("m6.finalTree without digest must fail with ErrM6TreeDigest, got %v", err)
	}
	good := map[string]any{
		"m6.finalTree": map[string]any{
			"rootId": "01ARZ3NDEKTSV4RRFFQ69G5FA9",
			"digest": "e18fd15c7d72014d5b8d6cee758ec1a9f75618fab8ebfd2ebc3958c1d501924a",
		},
	}
	if _, err := svc.CaptureInput(ctx, run.Run.ID, good); err != nil {
		t.Fatalf("valid m6.finalTree must pass: %v", err)
	}
	// The M6-native merge.finalize payload (finalDigest + testsRef) must pass
	// unmodified through the same seam.
	m6Payload := map[string]any{
		"m6.finalTree": map[string]any{
			"rootId":      "01ARZ3NDEKTSV4RRFFQ69G5FA9",
			"finalDigest": "f71d916a8c5deea3cbcf5d29c3ec6b52d1a1d64b1f8d05f2f2b9a3c1e0d8b7f4",
			"testsRef":    "run-0042",
		},
	}
	if _, err := svc.CaptureInput(ctx, run.Run.ID, m6Payload); err != nil {
		t.Fatalf("M6-native finalDigest payload must pass: %v", err)
	}
	// Once running, a changed snapshot is stale (SNP-002).
	if _, err := svc.TransitionStage(ctx, run.Run.ID, m7flow.RunRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CaptureInput(ctx, run.Run.ID, map[string]any{"brief": "v2"}); !errors.Is(err, m7app.ErrSnapshotChanged) {
		t.Fatalf("changed input on running stage must fail with ErrSnapshotChanged, got %v", err)
	}
}

func TestM7ArtifactVersionsImmutable(t *testing.T) {
	svc, store, pid := newM7WorkflowService(t)
	ctx := context.Background()
	art, err := svc.PutArtifact(ctx, m7flow.ArtifactVersion{
		ArtifactID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", Kind: m7flow.KindDocument,
		ScopeType: "project", ScopeID: pid, ContentRef: "content/abc",
		SHA256: strings.Repeat("ab", 32), Size: 42, MediaType: "text/markdown",
		CreatedBy: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.VersionNo != 1 || art.State != m7flow.ArtifactActive {
		t.Fatalf("unexpected first artifact: %#v", art)
	}
	next, err := svc.PutArtifact(ctx, m7flow.ArtifactVersion{
		ArtifactID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", Kind: m7flow.KindDocument,
		ScopeType: "project", ScopeID: pid, ContentRef: "content/def",
		SHA256: strings.Repeat("cd", 32), Size: 43, MediaType: "text/markdown",
		CreatedBy: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.VersionNo != 2 {
		t.Fatalf("superseding bumps version_no, got %d", next.VersionNo)
	}
	// M7-ART-001: artifact rows are immutable at the DB layer.
	if _, err := store.db.Exec(`UPDATE artifact_versions SET size=999 WHERE id=?`, art.ID); err == nil {
		t.Fatal("artifact UPDATE must trip M7-ART-001")
	} else if !strings.Contains(err.Error(), "M7-ART-001") {
		t.Fatalf("expected M7-ART-001 trigger, got %v", err)
	}
	if _, err := store.db.Exec(`DELETE FROM artifact_versions WHERE id=?`, art.ID); err == nil {
		t.Fatal("artifact DELETE must trip M7-ART-001")
	} else if !strings.Contains(err.Error(), "M7-ART-001") {
		t.Fatalf("expected M7-ART-001 trigger, got %v", err)
	}
}
