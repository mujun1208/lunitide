package fileops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func crossVolumeTestErrno() error {
	if runtime.GOOS == "windows" {
		return syscall.Errno(17)
	}
	return syscall.Errno(18)
}

func TestPlanApplyUndoRenameAndWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("classify", []Item{
		{Action: ActionMkdir, To: "txt"},
		{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"},
		{Action: ActionWrite, To: "reports/weekly-report.md", Body: "# 周报\n\nhello\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if err != nil || st.State != "applied" {
		t.Fatalf("apply %+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(root, "txt", "notes.txt")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "reports", "weekly-report.md"))
	if err != nil || !strings.Contains(string(body), "周报") {
		t.Fatalf("report %s %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Fatal("source should have moved")
	}
	undone, err := svc.Undo(context.Background(), plan.ID)
	if err != nil || undone.State != "undone" {
		t.Fatalf("undo %+v %v", undone, err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal("undo must restore the file")
	}
	if _, err := os.Stat(filepath.Join(root, "reports", "weekly-report.md")); !os.IsNotExist(err) {
		t.Fatal("write undo must remove created file")
	}
}

func TestApplyRejectsJunctionPlantedAfterPlan(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionWrite, To: "reports/stolen.txt", Body: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "reports")
	if err := os.Symlink(outside, link); err != nil {
		out, err2 := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err2 != nil {
			t.Skipf("cannot create junction/symlink: %v %s", err, out)
		}
	}
	if _, err := svc.Apply(context.Background(), plan.ID); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("apply-time junction must fail closed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "stolen.txt")); !os.IsNotExist(err) {
		t.Fatal("apply must not write through a planted junction")
	}
}

func TestUndoRejectsJunctionPlantedAfterApply(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "victim.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionWrite, To: "reports/victim.txt", Body: "temp"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "reports", "victim.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "reports")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "reports")
	if err := os.Symlink(outside, link); err != nil {
		out, err2 := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err2 != nil {
			t.Skipf("cannot create junction/symlink: %v %s", err, out)
		}
	}
	if _, err := svc.Undo(context.Background(), plan.ID); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("undo-time junction must fail closed, err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(outside, "victim.txt"))
	if err != nil || string(got) != "keep" {
		t.Fatalf("undo must not delete through a planted junction: %s %v", got, err)
	}
}

func TestRejectsJunctionEscapeAndExistingDestination(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		out, err2 := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if err2 != nil {
			t.Skipf("cannot create junction/symlink: %v %s", err, out)
		}
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create("x", []Item{{Action: ActionWrite, To: "escape/stolen.txt", Body: "no"}}); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("junction escape must fail, err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("y"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create("x", []Item{{Action: ActionMove, From: "a.txt", To: "b.txt"}}); !errors.Is(err, ErrCollision) {
		t.Fatalf("existing dest must fail at plan, err=%v", err)
	}
}

func TestApplyRejectsChangedSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].SourceDigest == "" {
		t.Fatal("plan must stamp the source digest")
	}
	if err := os.WriteFile(src, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); !errors.Is(err, ErrInputChanged) {
		t.Fatalf("changed source must fail apply, err=%v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed apply must leave the source in place")
	}
}

func TestApplyChangedSourceStatusErrorIsChinese(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if !errors.Is(err, ErrInputChanged) {
		t.Fatalf("changed source must fail apply, err=%v", err)
	}
	if len(st.Items) == 0 || !strings.Contains(st.Items[0].Error, "源文件在计划后已被修改，已停止执行，未覆盖原文件") {
		t.Fatalf("status item.error must be Chinese: %+v", st.Items)
	}
	if strings.Contains(st.Items[0].Error, "fileops:") {
		t.Fatalf("must not persist English sentinel: %q", st.Items[0].Error)
	}
}

func TestApplyRejectsDestinationCreatedAfterPlan(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "txt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "txt", "notes.txt"), []byte("taken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); !errors.Is(err, ErrCollision) {
		t.Fatalf("apply-time dest must be ErrCollision, err=%v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed apply must leave the source in place")
	}
}

func TestApplyCollisionStatusErrorIsChinese(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "txt"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "txt", "notes.txt"), []byte("taken"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if !errors.Is(err, ErrCollision) {
		t.Fatalf("apply-time dest must be ErrCollision, err=%v", err)
	}
	if len(st.Items) == 0 || !strings.Contains(st.Items[0].Error, "目标路径已存在，已停止，未覆盖") {
		t.Fatalf("status item.error must be Chinese: %+v", st.Items)
	}
}

func TestUndoRejectsExistingRestoreTarget(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(context.Background(), plan.ID); !errors.Is(err, ErrUndoCollision) {
		t.Fatalf("undo dest must be ErrUndoCollision, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "txt", "notes.txt")); err != nil {
		t.Fatal("failed undo must leave the moved file")
	}
}

func TestUndoCollisionStatusErrorIsChinese(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Undo(context.Background(), plan.ID)
	if !errors.Is(err, ErrUndoCollision) {
		t.Fatalf("undo dest must be ErrUndoCollision, err=%v", err)
	}
	found := false
	for _, item := range st.Items {
		if strings.Contains(item.Error, "撤销目标已被修改，不能覆盖") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("undo status item.error must be Chinese: %+v", st.Items)
	}
}

func TestLegacyPlanWithoutSourceDigestStillApplies(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Items[0].SourceDigest = ""
	if err := svc.writeJSON(svc.planPath(plan.ID), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsPathEscapeAndDoesNotApply(t *testing.T) {
	svc, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create("x", []Item{{Action: ActionMove, From: "../secret.txt", To: "a.txt"}}); err == nil {
		t.Fatal("escape from must fail")
	}
	if _, err := svc.Create("x", []Item{{Action: ActionWrite, To: "C:/Windows/x.txt", Body: "no"}}); err == nil {
		t.Fatal("absolute to must fail")
	}
}

func TestRecipesAreLocalReversiblePlans(t *testing.T) {
	got := ClassifyRename([]string{"a.PDF", "b.docx", "../x.txt", "a.PDF"})
	if len(got) != 2 || got[0].To != "pdf/a.PDF" || got[1].To != "docx/b.docx" {
		t.Fatalf("classify %+v", got)
	}
	report := WeeklyReport("", "周报", []string{"完成验收"})
	if report[0].Action != ActionWrite || !strings.Contains(report[0].Body, "完成验收") {
		t.Fatalf("report %+v", report)
	}
	brief := Briefing("", "", []string{"公开资讯一条"})
	if !strings.Contains(brief[0].Body, "公开资讯一条") {
		t.Fatalf("brief %+v", brief)
	}
}

func TestApplyStopsAndKeepsSucceededPrefix(t *testing.T) {
	root := t.TempDir()
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("partial", []Item{
		{Action: ActionWrite, To: "ok.md", Body: "ok"},
		{Action: ActionMove, From: "missing.txt", To: "txt/missing.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if err != ErrApplyPartial || st.State != "partial" {
		t.Fatalf("partial %+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ok.md")); err != nil {
		t.Fatal("first item must stay")
	}
}

func TestApplyResumeDoesNotRedoSucceededItems(t *testing.T) {
	root := t.TempDir()
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("partial", []Item{
		{Action: ActionWrite, To: "ok.md", Body: "first"},
		{Action: ActionMove, From: "later.txt", To: "txt/later.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if !errors.Is(err, ErrApplyPartial) || st.State != "partial" {
		t.Fatalf("first apply %+v %v", st, err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.md"), []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "later.txt"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err = svc.Apply(context.Background(), plan.ID)
	if err != nil || st.State != "applied" {
		t.Fatalf("resume apply %+v %v", st, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "ok.md"))
	if err != nil || string(body) != "edited" {
		t.Fatalf("succeeded write must not be rewritten: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(root, "txt", "later.txt")); err != nil {
		t.Fatal("remaining item must finish")
	}
	if len(st.Items) != 2 || st.Items[0].State != "succeeded" || st.Items[1].State != "succeeded" {
		t.Fatalf("resume status %+v", st.Items)
	}
}

func TestApplyMissingSourceStatusErrorIsChinese(t *testing.T) {
	root := t.TempDir()
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("partial", []Item{
		{Action: ActionWrite, To: "ok.md", Body: "ok"},
		{Action: ActionMove, From: "missing.txt", To: "txt/missing.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Apply(context.Background(), plan.ID)
	if !errors.Is(err, ErrApplyPartial) {
		t.Fatalf("missing source must be partial, err=%v", err)
	}
	found := false
	for _, item := range st.Items {
		if item.State == "failed" && strings.Contains(item.Error, "源文件不存在，已停止执行") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing source status must be Chinese: %+v", st.Items)
	}
}

func TestApplyUndonePlanErrorIsChinese(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Apply(context.Background(), plan.ID)
	if !errors.Is(err, ErrAlreadyUndone) {
		t.Fatalf("re-apply undone must be ErrAlreadyUndone, err=%v", err)
	}
	if !strings.Contains(UserMessage(err), "已撤销的计划不能再次执行") {
		t.Fatalf("re-apply undone must be Chinese: %v", err)
	}
}

func TestStatusRewritesLegacyPermissionEnglish(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	st := Status{
		PlanID: plan.ID,
		State:  "partial",
		Items:  []ItemStatus{{ID: plan.Items[0].ID, State: "failed", Error: os.ErrPermission.Error()}},
	}
	if err := svc.writeJSON(svc.statusPath(plan.ID), st); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) == 0 || got.Items[0].Error != UserMessage(os.ErrPermission) {
		t.Fatalf("legacy permission status must localize: %+v", got.Items)
	}
}

func TestStatusRewritesLegacyEnglishSentinel(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	st := Status{
		PlanID: plan.ID,
		State:  "partial",
		Items:  []ItemStatus{{ID: plan.Items[0].ID, State: "failed", Error: ErrInputChanged.Error()}},
	}
	if err := svc.writeJSON(svc.statusPath(plan.ID), st); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) == 0 || got.Items[0].Error != UserMessage(ErrInputChanged) {
		t.Fatalf("legacy English status must localize: %+v", got.Items)
	}
}

func TestStatusLocalizesLegacyEnglishStoredError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.Create("x", []Item{{Action: ActionMove, From: "notes.txt", To: "txt/notes.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	st := Status{
		PlanID: plan.ID,
		State:  "partial",
		Items:  []ItemStatus{{ID: plan.Items[0].ID, State: "failed", Error: "Access is denied"}},
	}
	if err := svc.writeJSON(svc.statusPath(plan.ID), st); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Status(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) == 0 || !strings.Contains(got.Items[0].Error, "文件操作失败，已停止执行") {
		t.Fatalf("legacy English OS status must localize: %+v", got.Items)
	}
	if strings.Contains(got.Items[0].Error, "Access is denied") {
		t.Fatalf("must not leak Access is denied: %q", got.Items[0].Error)
	}
	if got.Items[0].Error != UserMessage(errors.New("Access is denied")) {
		t.Fatalf("unknown stored English must use UserMessage fallback: %q", got.Items[0].Error)
	}
}

func TestCreateNilServiceIsChineseUnavailable(t *testing.T) {
	var svc *Service
	_, err := svc.Create("x", nil)
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil service must be ErrUnavailable, got %v", err)
	}
	if !strings.Contains(UserMessage(err), "工作区尚未就绪") {
		t.Fatalf("nil service must be Chinese: %q", UserMessage(err))
	}
	if strings.Contains(err.Error(), "fileops unavailable") || strings.Contains(UserMessage(err), "unavailable") {
		t.Fatalf("must not leak English unavailable: %v", err)
	}
}

func TestNewRelativeRootIsChinese(t *testing.T) {
	_, err := New("not-absolute")
	if err == nil || !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("relative root must be ErrInvalidRoot, got %v", err)
	}
	if !strings.Contains(UserMessage(err), "工作区路径无效") {
		t.Fatalf("relative root must be Chinese: %q", UserMessage(err))
	}
	if strings.Contains(UserMessage(err), "absolute") {
		t.Fatalf("must not leak English absolute-root: %q", UserMessage(err))
	}
}

func TestUserMessageUnknownOSErrorIsChinese(t *testing.T) {
	got := UserMessage(errors.New("The process cannot access the file"))
	if !strings.Contains(got, "文件操作失败，已停止执行") {
		t.Fatalf("unknown OS error must be generic Chinese: %q", got)
	}
	if strings.Contains(got, "process cannot") {
		t.Fatalf("must not leak English OS text: %q", got)
	}
}

func TestUserMessagePermissionIsChinese(t *testing.T) {
	got := UserMessage(os.ErrPermission)
	if !strings.Contains(got, "没有权限，已停止执行") {
		t.Fatalf("permission must be Chinese: %q", got)
	}
	if strings.Contains(got, "permission denied") {
		t.Fatalf("must not leak English permission: %q", got)
	}
	wrapped := &os.PathError{Op: "mkdir", Path: "x", Err: os.ErrPermission}
	if UserMessage(wrapped) != got {
		t.Fatalf("wrapped PathError must use the same Chinese: %q", UserMessage(wrapped))
	}
}

func TestIsCrossVolumePathDetectsDifferentWindowsDrives(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters are a Windows volume signal")
	}
	if !isCrossVolumePath(`C:\ws\a.txt`, `D:\ws\b.txt`) {
		t.Fatal("C: vs D: must be cross-volume")
	}
	if isCrossVolumePath(`C:\ws\a.txt`, `C:\ws\b.txt`) {
		t.Fatal("same drive must not be cross-volume")
	}
}

func TestMapRenameErrorCrossVolume(t *testing.T) {
	err := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: crossVolumeTestErrno()}
	got := mapRenameError(err)
	if !errors.Is(got, ErrCrossVolume) {
		t.Fatalf("cross-volume rename must be ErrCrossVolume, got %v", got)
	}
	other := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: os.ErrPermission}
	if errors.Is(mapRenameError(other), ErrCrossVolume) {
		t.Fatal("permission errors must not become cross-volume")
	}
}
