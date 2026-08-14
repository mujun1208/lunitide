package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// csFixture builds a workspace tree for change set tests:
//
//	src/existing.txt ("original\n"), src/to-delete.txt ("bye\n")
//	outside.txt (outside the granted scope)
func csFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/existing.txt", "original\n")
	write("src/to-delete.txt", "bye\n")
	write("outside.txt", "secret\n")
	return root
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func fileExists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

type changeSetOut struct {
	ID             string `json:"id"`
	RunID          string `json:"runId"`
	BaseDigest     string `json:"baseDigest"`
	ApprovalDigest string `json:"approvalDigest"`
	Status         string `json:"status"`
	Version        int64  `json:"version"`
}

type changeSetPreviewOut struct {
	ChangeSet  changeSetOut `json:"changeSet"`
	Operations []struct {
		Ordinal        int64  `json:"ordinal"`
		Op             string `json:"op"`
		Path           string `json:"path"`
		ContentDigest  string `json:"contentDigest"`
		OriginalDigest string `json:"originalDigest"`
	} `json:"operations"`
}

func csCall(e *Engine, method bridge.Method, payload map[string]any, key string) bridge.Response {
	body, _ := json.Marshal(payload)
	return e.Handle(context.Background(), agentRunRequest(method, string(body), key))
}

func csPreview(t *testing.T, e *Engine, runID string, lease fsLeaseOut, ops []map[string]any, key string) changeSetPreviewOut {
	t.Helper()
	res := csCall(e, bridge.MethodChangesetPreview, map[string]any{
		"runId": runID, "leaseId": lease.ID, "fencingToken": lease.FencingToken, "ops": ops,
	}, key)
	return decodePayloadInto[changeSetPreviewOut](t, res)
}

func csMutate(e *Engine, method bridge.Method, set changeSetOut, lease fsLeaseOut, version int64, digest, key string) bridge.Response {
	return csCall(e, method, map[string]any{
		"changeSetId": set.ID, "expectedVersion": version, "approvalDigest": digest,
		"leaseId": lease.ID, "fencingToken": lease.FencingToken,
	}, key)
}

func csStandardOps() []map[string]any {
	return []map[string]any{
		{"op": "create", "path": "src/new.txt", "content": "hello\n"},
		{"op": "update", "path": "src/existing.txt", "content": "updated\n"},
		{"op": "delete", "path": "src/to-delete.txt"},
	}
}

func approveChangesetReview(t *testing.T, e *Engine, runID, digest, key string) {
	t.Helper()
	status, version := getRunStatus(t, e, runID)
	if status != agentrun.RunPausedReview {
		t.Fatalf("run must await review before approval: status=%s version=%d", status, version)
	}
	res := decideReview(t, e, runID, version, digest, "approved", key)
	if !res.OK {
		t.Fatalf("approve changeset review: code=%s msg=%s", res.Error.Code, res.Error.Message)
	}
}

func TestChangesetPreviewApplyRevertLifecycle(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	root := csFixture(t)
	lease := fsLease(t, e, root, []string{"read", "write"}, []string{"src/**"}, "cs-life")
	run := startAgentRun(t, e, sessionID, "cs-life-run")

	preview := csPreview(t, e, run.ID, lease, csStandardOps(), "cs-life-preview")
	set := preview.ChangeSet
	if set.Status != "previewed" || set.Version != 2 || set.RunID != run.ID {
		t.Fatalf("previewed set=%+v", set)
	}
	if len(set.BaseDigest) != 64 || len(set.ApprovalDigest) != 64 {
		t.Fatalf("digests wrong: %+v", set)
	}
	if len(preview.Operations) != 3 {
		t.Fatalf("operations=%+v", preview.Operations)
	}
	for i, op := range preview.Operations {
		if op.Ordinal != int64(i)+1 {
			t.Fatalf("ordinal=%+v", op)
		}
	}
	if preview.Operations[0].ContentDigest == "" || preview.Operations[0].OriginalDigest != "" {
		t.Fatalf("create projection=%+v", preview.Operations[0])
	}
	if preview.Operations[2].OriginalDigest == "" || preview.Operations[2].ContentDigest != "" {
		t.Fatalf("delete projection=%+v", preview.Operations[2])
	}

	// Idempotent replay returns the same set.
	replay := csPreview(t, e, run.ID, lease, csStandardOps(), "cs-life-preview")
	if replay.ChangeSet.ID != set.ID || replay.ChangeSet.ApprovalDigest != set.ApprovalDigest {
		t.Fatalf("replay=%+v vs %+v", replay.ChangeSet, set)
	}

	// Wrong approval digest and version are refused before any write.
	badDigest := csMutate(e, bridge.MethodChangesetApply, set, lease, set.Version, "0000000000000000000000000000000000000000000000000000000000000000", "cs-life-apply-bad-digest")
	if badDigest.OK || badDigest.Error.Code != "REVIEW_DIGEST_MISMATCH" {
		t.Fatalf("badDigest=%#v", badDigest)
	}
	badVersion := csMutate(e, bridge.MethodChangesetApply, set, lease, set.Version+1, set.ApprovalDigest, "cs-life-apply-bad-version")
	if badVersion.OK || badVersion.Error.Code != "CHANGESET_VERSION_CONFLICT" {
		t.Fatalf("badVersion=%#v", badVersion)
	}
	approveChangesetReview(t, e, run.ID, set.ApprovalDigest, "cs-life-approve-apply")

	// Apply succeeds and writes hit the disk in order.
	applied := decodePayloadInto[struct {
		ChangeSet  changeSetOut `json:"changeSet"`
		AppliedOps int          `json:"appliedOps"`
	}](t, csMutate(e, bridge.MethodChangesetApply, set, lease, set.Version, set.ApprovalDigest, "cs-life-apply"))
	if applied.ChangeSet.Status != "applied" || applied.AppliedOps != 3 {
		t.Fatalf("applied=%+v", applied)
	}
	if got := readFile(t, root, "src/new.txt"); got != "hello\n" {
		t.Fatalf("new.txt=%q", got)
	}
	if got := readFile(t, root, "src/existing.txt"); got != "updated\n" {
		t.Fatalf("existing.txt=%q", got)
	}
	if fileExists(root, "src/to-delete.txt") {
		t.Fatal("to-delete.txt should be gone")
	}

	// A second apply is an invalid transition, not a silent no-op.
	again := csMutate(e, bridge.MethodChangesetApply, applied.ChangeSet, lease, applied.ChangeSet.Version, set.ApprovalDigest, "cs-life-apply-again")
	if again.OK || again.Error.Code != "CHANGESET_TRANSITION_INVALID" {
		t.Fatalf("again=%#v", again)
	}
	approveChangesetReview(t, e, run.ID, set.ApprovalDigest, "cs-life-approve-revert")

	// Revert restores the original workspace state.
	reverted := decodePayloadInto[struct {
		ChangeSet   changeSetOut `json:"changeSet"`
		RevertedOps int          `json:"revertedOps"`
	}](t, csMutate(e, bridge.MethodChangesetRevert, applied.ChangeSet, lease, applied.ChangeSet.Version, set.ApprovalDigest, "cs-life-revert"))
	if reverted.ChangeSet.Status != "reverted" || reverted.RevertedOps != 3 {
		t.Fatalf("reverted=%+v", reverted)
	}
	if fileExists(root, "src/new.txt") {
		t.Fatal("new.txt should be removed by revert")
	}
	if got := readFile(t, root, "src/existing.txt"); got != "original\n" {
		t.Fatalf("existing.txt after revert=%q", got)
	}
	if got := readFile(t, root, "src/to-delete.txt"); got != "bye\n" {
		t.Fatalf("to-delete.txt after revert=%q", got)
	}

	// A reverted set refuses any further revert.
	terminal := csMutate(e, bridge.MethodChangesetRevert, reverted.ChangeSet, lease, reverted.ChangeSet.Version, set.ApprovalDigest, "cs-life-revert-again")
	if terminal.OK || terminal.Error.Code != "CHANGESET_TRANSITION_INVALID" {
		t.Fatalf("terminal=%#v", terminal)
	}
}

func TestChangesetPreviewScopeAndValidation(t *testing.T) {
	e, sessionID, _ := agentRunEngine(t)
	root := csFixture(t)
	lease := fsLease(t, e, root, []string{"read", "write"}, []string{"src/**"}, "cs-val")
	run := startAgentRun(t, e, sessionID, "cs-val-run")

	preview := func(ops []map[string]any, key string) bridge.Response {
		return csCall(e, bridge.MethodChangesetPreview, map[string]any{
			"runId": run.ID, "leaseId": lease.ID, "fencingToken": lease.FencingToken, "ops": ops,
		}, key)
	}

	// Out-of-scope and escaping paths are refused closed.
	scope := preview([]map[string]any{{"op": "update", "path": "outside.txt", "content": "x"}}, "cs-val-scope")
	if scope.OK || scope.Error.Code != "FS_SCOPE_DENIED" {
		t.Fatalf("scope=%#v", scope)
	}
	escape := preview([]map[string]any{{"op": "create", "path": "../evil.txt", "content": "x"}}, "cs-val-escape")
	if escape.OK || escape.Error.Code != "FS_PATH_INVALID" {
		t.Fatalf("escape=%#v", escape)
	}

	// A read-only lease cannot mutate.
	readOnly := fsLease(t, e, root, []string{"read"}, []string{"src/**"}, "cs-val-ro")
	denied := csCall(e, bridge.MethodChangesetPreview, map[string]any{
		"runId": run.ID, "leaseId": readOnly.ID, "fencingToken": readOnly.FencingToken,
		"ops": []map[string]any{{"op": "create", "path": "src/nope.txt", "content": "x"}},
	}, "cs-val-ro-preview")
	if denied.OK || denied.Error.Code != "FS_SCOPE_DENIED" {
		t.Fatalf("denied=%#v", denied)
	}

	// Schema-level validation: unknown op, delete with content, create
	// targeting an existing path.
	badOp := preview([]map[string]any{{"op": "rename", "path": "src/a.txt"}}, "cs-val-bad-op")
	if badOp.OK || badOp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("badOp=%#v", badOp)
	}
	deleteWithContent := preview([]map[string]any{{"op": "delete", "path": "src/existing.txt", "content": "x"}}, "cs-val-del-content")
	if deleteWithContent.OK || deleteWithContent.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("deleteWithContent=%#v", deleteWithContent)
	}
	exists := preview([]map[string]any{{"op": "create", "path": "src/existing.txt", "content": "x"}}, "cs-val-exists")
	if exists.OK || exists.Error.Code != "FS_PATH_EXISTS" {
		t.Fatalf("exists=%#v", exists)
	}

	// A non-running run cannot preview.
	cancelled := decodePayloadInto[agentRunDTO](t, csCall(e, bridge.MethodAgentRunCancel, map[string]any{
		"runId": run.ID, "expectedVersion": run.Version,
	}, "cs-val-cancel"))
	stopped := csCall(e, bridge.MethodChangesetPreview, map[string]any{
		"runId": cancelled.ID, "leaseId": lease.ID, "fencingToken": lease.FencingToken,
		"ops": []map[string]any{{"op": "create", "path": "src/late.txt", "content": "x"}},
	}, "cs-val-late")
	if stopped.OK || stopped.Error.Code != "CHANGESET_TRANSITION_INVALID" {
		t.Fatalf("stopped=%#v", stopped)
	}
}

func TestChangesetBaseConflictMarksSetConflicted(t *testing.T) {
	e, sessionID, store := agentRunEngine(t)
	root := csFixture(t)
	lease := fsLease(t, e, root, []string{"read", "write"}, []string{"src/**"}, "cs-conflict")
	run := startAgentRun(t, e, sessionID, "cs-conflict-run")

	preview := csPreview(t, e, run.ID, lease, []map[string]any{
		{"op": "update", "path": "src/existing.txt", "content": "updated\n"},
	}, "cs-conflict-preview")
	set := preview.ChangeSet

	// The workspace drifts after preview: apply must refuse and conflict
	// the set instead of writing onto the new base.
	if err := os.WriteFile(filepath.Join(root, "src", "existing.txt"), []byte("drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approveChangesetReview(t, e, run.ID, set.ApprovalDigest, "cs-conflict-approve-apply")
	res := csMutate(e, bridge.MethodChangesetApply, set, lease, set.Version, set.ApprovalDigest, "cs-conflict-apply")
	if res.OK || res.Error.Code != "CHANGESET_BASE_CONFLICT" {
		t.Fatalf("res=%#v", res)
	}
	if got := readFile(t, root, "src/existing.txt"); got != "drifted\n" {
		t.Fatalf("apply must not write on drift, got %q", got)
	}

	// The set is durably conflicted (terminal).
	var stored agentrun.ChangeSet
	if err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		var err error
		stored, err = tx.GetChangeSet(set.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentrun.ChangeSetConflicted {
		t.Fatalf("status=%s", stored.Status)
	}
	terminal := csMutate(e, bridge.MethodChangesetApply, set, lease, stored.Version, set.ApprovalDigest, "cs-conflict-again")
	if terminal.OK || terminal.Error.Code != "CHANGESET_TERMINAL" {
		t.Fatalf("terminal=%#v", terminal)
	}
}
