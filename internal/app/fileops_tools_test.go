package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/fileops"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestFileOpsToolsPlanApplyUndo(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.pdf"), []byte("%PDF-1.4"), 0600); err != nil {
		t.Fatal(err)
	}
	planOut, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"recipe":"classify","files":["a.pdf"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var plan fileops.Plan
	if json.Unmarshal([]byte(planOut.Output), &plan) != nil || plan.ID == "" {
		t.Fatalf("plan %s", planOut.Output)
	}
	applyArgs, _ := json.Marshal(map[string]string{"planId": plan.ID})
	if _, err := e.executeFileOps(context.Background(), session, "files.apply", applyArgs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "pdf", "a.pdf")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.executeFileOps(context.Background(), session, "files.undo", applyArgs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.pdf")); err != nil {
		t.Fatal("undo should restore a.pdf")
	}
	report, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"recipe":"weekly-report","title":"周报","notes":["本周完成验收"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Output, "本周完成验收") {
		t.Fatalf("report plan %s", report.Output)
	}
}

func TestFileOpsApplyRejectsChangedSource(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	planOut, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"items":[{"action":"move","from":"notes.txt","to":"txt/notes.txt"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var plan fileops.Plan
	if json.Unmarshal([]byte(planOut.Output), &plan) != nil || plan.ID == "" {
		t.Fatalf("plan %s", planOut.Output)
	}
	if err := os.WriteFile(src, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	applyArgs, _ := json.Marshal(map[string]string{"planId": plan.ID})
	out, err := e.executeFileOps(context.Background(), session, "files.apply", applyArgs)
	if err == nil || !strings.Contains(err.Error(), "已被修改") || !errors.Is(err, fileops.ErrInputChanged) {
		t.Fatalf("changed source must fail closed: %v", err)
	}
	var st fileops.Status
	if json.Unmarshal([]byte(out.Output), &st) != nil || len(st.Items) == 0 || st.Items[0].State != "failed" {
		t.Fatalf("apply must return failed status: %s", out.Output)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed apply must leave the source")
	}
}

func TestFileOpsUndoRejectsExistingRestoreTarget(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	planOut, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"items":[{"action":"move","from":"notes.txt","to":"txt/notes.txt"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var plan fileops.Plan
	if json.Unmarshal([]byte(planOut.Output), &plan) != nil || plan.ID == "" {
		t.Fatalf("plan %s", planOut.Output)
	}
	applyArgs, _ := json.Marshal(map[string]string{"planId": plan.ID})
	if _, err := e.executeFileOps(context.Background(), session, "files.apply", applyArgs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("recreated"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = e.executeFileOps(context.Background(), session, "files.undo", applyArgs)
	if err == nil || !strings.Contains(err.Error(), "撤销目标已被修改") {
		t.Fatalf("undo collision must be Chinese: %v", err)
	}
	if !errors.Is(err, fileops.ErrUndoCollision) {
		t.Fatalf("undo must wrap ErrUndoCollision: %v", err)
	}
}

func TestFileOpsUserErrorCrossVolumeChinese(t *testing.T) {
	err := fileOpsUserError(fileops.ErrCrossVolume)
	if err == nil || !strings.Contains(err.Error(), "不支持跨卷移动，已停止，未复制") {
		t.Fatalf("cross-volume must be Chinese fail-closed: %v", err)
	}
	if !errors.Is(err, fileops.ErrCrossVolume) {
		t.Fatalf("must keep ErrCrossVolume: %v", err)
	}
}

func TestFileOpsUserErrorRemainingSentinelsChinese(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fileops.ErrOutsideRoot, "路径超出工作区"},
		{fileops.ErrPlanNotFound, "文件计划不存在"},
		{fileops.ErrInvalidPlan, "文件计划无效"},
		{fileops.ErrApplyPartial, "批次已停止，已完成的步骤保留"},
		{fileops.ErrAlreadyUndone, "已撤销的计划不能再次执行"},
		{fileops.ErrUnavailable, "工作区尚未就绪"},
		{fileops.ErrInvalidRoot, "工作区路径无效"},
	}
	for _, tc := range cases {
		got := fileOpsUserError(tc.err)
		if got == nil || !strings.Contains(got.Error(), tc.want) {
			t.Fatalf("%v must be Chinese %q, got %v", tc.err, tc.want, got)
		}
		if !errors.Is(got, tc.err) {
			t.Fatalf("must keep sentinel %v: %v", tc.err, got)
		}
		if strings.HasPrefix(got.Error(), "fileops:") {
			t.Fatalf("must not leak English sentinel prefix: %v", got)
		}
	}
}

func TestFilesToolReturnsStatusKeepsPartialAndCrossVolume(t *testing.T) {
	if !filesToolReturnsStatus("files.apply", fileops.ErrCrossVolume) {
		t.Fatal("cross-volume apply must keep status JSON")
	}
	if !filesToolReturnsStatus("files.apply", fileops.ErrApplyPartial) {
		t.Fatal("partial apply must keep status JSON")
	}
	if !filesToolReturnsStatus("files.apply", fmt.Errorf("wrap: %w", fileops.ErrApplyPartial)) {
		t.Fatal("wrapped partial must keep status JSON")
	}
	if filesToolReturnsStatus("files.plan", fileops.ErrApplyPartial) {
		t.Fatal("plan must not pretend to return apply status")
	}
}

func TestFileOpsForUnwiredWorkspaceIsChinese(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	_, err := e.fileOpsFor(ulid.Make().String())
	if err == nil || !strings.Contains(err.Error(), "工作区尚未就绪") {
		t.Fatalf("unwired workspace must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "workspace unavailable") {
		t.Fatalf("must not leak English workspace gap: %v", err)
	}
}

func TestExecuteFileOpsUnknownToolIsChinese(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	_, err = e.executeFileOps(context.Background(), ulid.Make().String(), "files.hack", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "未知文件操作") {
		t.Fatalf("unknown files tool must be Chinese: %v", err)
	}
	if strings.Contains(err.Error(), "unknown files tool") {
		t.Fatalf("must not leak English unknown-tool gap: %v", err)
	}
}

func TestFileOpsUserErrorPermissionIsChinese(t *testing.T) {
	got := fileOpsUserError(os.ErrPermission)
	if got == nil || !strings.Contains(got.Error(), "没有权限，已停止执行") {
		t.Fatalf("permission must be Chinese: %v", got)
	}
	if strings.Contains(got.Error(), "permission denied") {
		t.Fatalf("must not leak English permission: %v", got)
	}
}

func TestLooksLikeOCRRequest(t *testing.T) {
	if !looksLikeOCRRequest("请识别这张发票上的文字") || looksLikeOCRRequest("今天天气怎么样") {
		t.Fatal("OCR intent classification")
	}
}

func TestWeeklyReportReadsWorkspaceFilesIntoNotes(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("本周完成接口联调"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"recipe":"weekly-report","title":"周报","files":["a.md"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.Output, "本周完成接口联调") {
		t.Fatalf("weekly-report must read files[] into notes: %s", report.Output)
	}
	if strings.Contains(report.Output, "没有可用的参考材料正文") {
		t.Fatalf("must not write an empty-materials report when files[] has text: %s", report.Output)
	}
}

func TestWeeklyReportFailsClosedWhenOnlyMissingFilesProvided(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	if err := os.MkdirAll(filepath.Join(runtime.WorkspaceRoot(), session), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = e.executeFileOps(context.Background(), session, "files.plan", json.RawMessage(`{"recipe":"weekly-report","title":"周报","files":["missing.md"]}`))
	if err == nil || !strings.Contains(err.Error(), "源文件不存在") {
		t.Fatalf("missing files[] must fail closed in Chinese: %v", err)
	}
	if !errors.Is(err, fileops.ErrSourceMissing) {
		t.Fatalf("must keep ErrSourceMissing: %v", err)
	}
	if strings.HasPrefix(err.Error(), "fileops:") {
		t.Fatalf("must not lead with English sentinel: %v", err)
	}
}
