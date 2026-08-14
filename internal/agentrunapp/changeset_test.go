package agentrunapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

func TestChangeSetFaultInjectionPhaseEdges(t *testing.T) {
	boom := errors.New("simulated crash")
	changeSetFault = func(point string) error {
		if point == "apply.after_effect" {
			return boom
		}
		return nil
	}
	t.Cleanup(func() { changeSetFault = nil })
	if err := injectChangeSetFault("apply.after_prepare"); err != nil {
		t.Fatalf("unexpected prepare fault: %v", err)
	}
	if err := injectChangeSetFault("apply.after_effect"); !errors.Is(err, boom) {
		t.Fatalf("fault=%v", err)
	}
}

func TestAtomicChangeSetFileOperationsAndReceiptReconcile(t *testing.T) {
	root := t.TempDir()
	access := fsAccess{root: root}
	updatePath := filepath.Join(root, "update.txt")
	deletePath := filepath.Join(root, "delete.txt")
	if err := os.WriteFile(updatePath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deletePath, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated := "new"
	ops := []agentrun.ChangeSetOperation{
		{Op: agentrun.ChangeSetOpUpdate, Path: "update.txt", Content: &updated, ContentDigest: digestText(updated), OriginalDigest: digestText("old")},
		{Op: agentrun.ChangeSetOpDelete, Path: "delete.txt", OriginalDigest: digestText("gone")},
	}
	if err := writeOp(updatePath, ops[0]); err != nil {
		t.Fatal(err)
	}
	if err := writeOp(deletePath, ops[1]); err != nil {
		t.Fatal(err)
	}
	allApplied, allOriginal := reconcilePreparedApply(changeSetMutation{ops: ops, access: access})
	if !allApplied || allOriginal {
		t.Fatalf("applied=%v original=%v", allApplied, allOriginal)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".lunitide-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("atomic temp files leaked: %v", matches)
	}
}

func TestPreparedRevertReceiptReconcile(t *testing.T) {
	root := t.TempDir()
	access := fsAccess{root: root}
	updatePath := filepath.Join(root, "update.txt")
	deletePath := filepath.Join(root, "delete.txt")
	createPath := filepath.Join(root, "create.txt")
	updated, created := "new", "created"
	ops := []agentrun.ChangeSetOperation{
		{Op: agentrun.ChangeSetOpUpdate, Path: "update.txt", Content: &updated, OriginalContent: textPtr("old"), AppliedDigest: digestText(updated), OriginalDigest: digestText("old")},
		{Op: agentrun.ChangeSetOpDelete, Path: "delete.txt", OriginalContent: textPtr("gone"), OriginalDigest: digestText("gone")},
		{Op: agentrun.ChangeSetOpCreate, Path: "create.txt", Content: &created, AppliedDigest: digestText(created)},
	}
	if err := os.WriteFile(updatePath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(createPath, []byte(created), 0o644); err != nil {
		t.Fatal(err)
	}
	allReverted, allApplied := reconcilePreparedRevert(changeSetMutation{ops: ops, access: access})
	if allReverted || !allApplied {
		t.Fatalf("before revert: reverted=%v applied=%v", allReverted, allApplied)
	}
	if err := os.WriteFile(updatePath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deletePath, []byte("gone"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(createPath); err != nil {
		t.Fatal(err)
	}
	allReverted, allApplied = reconcilePreparedRevert(changeSetMutation{ops: ops, access: access})
	if !allReverted || allApplied {
		t.Fatalf("after revert: reverted=%v applied=%v", allReverted, allApplied)
	}
}

func textPtr(s string) *string { return &s }

func TestAtomicReplaceOverwritesAndCreateDoesNot(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicReplace(p, []byte("new")); err != nil {
		t.Fatalf("replace existing: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != "new" {
		t.Fatalf("content=%q err=%v", got, err)
	}
	if err := atomicCreate(p, []byte("clobber")); err == nil {
		t.Fatal("exclusive create overwrote existing file")
	}
}

func TestUndoWriteRestoresDeletedOriginal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "deleted.txt")
	original := "original"
	op := agentrun.ChangeSetOperation{Op: agentrun.ChangeSetOpDelete, Path: "deleted.txt", OriginalContent: &original, OriginalDigest: digestText(original)}
	if err := undoWrite(p, op); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil || string(got) != original {
		t.Fatalf("restored=%q err=%v", got, err)
	}
}

func TestApplyCompensatesFirstFileWhenSecondCASDrifts(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.txt")
	secondPath := filepath.Join(root, "second.txt")
	for path, content := range map[string]string{firstPath: "first-old", secondPath: "second-old"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	firstNew, secondNew := "first-new", "second-new"
	ops := []agentrun.ChangeSetOperation{
		{Op: agentrun.ChangeSetOpUpdate, Path: "first.txt", Content: &firstNew, ContentDigest: digestText(firstNew), OriginalContent: textPtr("first-old"), OriginalDigest: digestText("first-old")},
		{Op: agentrun.ChangeSetOpUpdate, Path: "second.txt", Content: &secondNew, ContentDigest: digestText(secondNew), OriginalContent: textPtr("second-old"), OriginalDigest: digestText("second-old")},
	}
	mutation := changeSetMutation{ops: ops, access: fsAccess{root: root, patterns: []string{"**"}}}
	if err := writeOp(firstPath, ops[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, conflict := casCheckOriginal(mutation.access, ops[1]); conflict == "" {
		t.Fatal("second operation unexpectedly passed CAS")
	}
	if err := compensateChangeSet(mutation, []int{0}, "apply"); err != nil {
		t.Fatal(err)
	}
	assertFileText(t, firstPath, "first-old")
}

func TestRevertCompensatesFirstReverseFileWhenNextCASDrifts(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.txt")
	secondPath := filepath.Join(root, "second.txt")
	firstNew, secondNew := "first-new", "second-new"
	for path, content := range map[string]string{firstPath: firstNew, secondPath: secondNew} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ops := []agentrun.ChangeSetOperation{
		{Op: agentrun.ChangeSetOpUpdate, Path: "first.txt", Content: &firstNew, ContentDigest: digestText(firstNew), OriginalContent: textPtr("first-old"), OriginalDigest: digestText("first-old"), AppliedDigest: digestText(firstNew)},
		{Op: agentrun.ChangeSetOpUpdate, Path: "second.txt", Content: &secondNew, ContentDigest: digestText(secondNew), OriginalContent: textPtr("second-old"), OriginalDigest: digestText("second-old"), AppliedDigest: digestText(secondNew)},
	}
	mutation := changeSetMutation{ops: ops, access: fsAccess{root: root, patterns: []string{"**"}}}
	if err := revertOp(secondPath, ops[1]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstPath, []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, conflict := casCheckApplied(mutation.access, ops[0]); conflict == "" {
		t.Fatal("next reverse operation unexpectedly passed CAS")
	}
	if err := compensateChangeSet(mutation, []int{1}, "revert"); err != nil {
		t.Fatal(err)
	}
	assertFileText(t, secondPath, secondNew)
}

func assertFileText(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s content=%q want=%q err=%v", path, got, want, err)
	}
}
