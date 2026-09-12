package agenthub

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testService(t *testing.T) *Service {
	t.Helper()
	s := New(NewMemoryStore(), t.TempDir(), func(string, string) error { return nil })
	s.Look = func(string) (string, error) { return `C:\fake\codex.exe`, nil }
	s.Version = func(string, time.Duration) (string, error) { return "codex 1", nil }
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`{"type":"started","title":"start"}`)
		onLine(`{"type":"message","detail":"` + strings.ReplaceAll(string(spec.Stdin), `"`, ``) + `"}`)
		if err := os.WriteFile(filepath.Join(spec.Dir, "hello.txt"), []byte("ok"), 0o644); err != nil {
			return 1, false, err
		}
		return 0, false, nil
	}
	return s
}

func TestStartRejectedWhenUnavailable(t *testing.T) {
	s := testService(t)
	s.Look = func(string) (string, error) { return "", errors.New("missing") }
	_, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hi", IdempotencyKey: "k1"})
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("%v", err)
	}
}

func TestKimiStartWhenAvailable(t *testing.T) {
	s := testService(t)
	s.Look = func(string) (string, error) { return `C:\kimi.exe`, nil }
	detail, err := s.StartTask(TaskRequest{Agent: "kimi", Prompt: "hi", IdempotencyKey: "kimi-1"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "success" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%+v", detail)
}

func TestStartAndGetEvents(t *testing.T) {
	s := testService(t)
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hello", IdempotencyKey: "k2"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, err = s.GetTask(detail.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if detail.Task.Status != "success" || len(detail.Events) < 2 {
		t.Fatalf("%+v", detail)
	}
	if len(detail.Artifacts) == 0 {
		t.Fatal("expected scanned hello.txt")
	}
}

func TestAllocateDirConcurrentDoesNotCollide(t *testing.T) {
	s := testService(t)
	var a, b string
	var errA, errB error
	done := make(chan struct{})
	go func() {
		a, errA = s.allocateDir()
		done <- struct{}{}
	}()
	b, errB = s.allocateDir()
	<-done
	if errA != nil || errB != nil || a == "" || a == b {
		t.Fatalf("a=%q (%v) b=%q (%v)", a, errA, b, errB)
	}
}

func TestMaybeRunDoesNotDoubleStartSameTask(t *testing.T) {
	s := testService(t)
	var started int
	hold := make(chan struct{})
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		started++
		<-hold
		return 0, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "once", IdempotencyKey: "once"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	adapter, err := Adapter("codex")
	if err != nil {
		t.Fatal(err)
	}
	s.maybeRun(adapter, TaskRequest{Agent: "codex", Prompt: "once", WorkDir: detail.Task.WorkDir}, detail.Task, time.Minute)
	close(hold)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if started != 1 {
		t.Fatalf("started %d", started)
	}
}

func TestRecoverScansInterruptedWorkDir(t *testing.T) {
	s := testService(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kept.md"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.InsertTask(TaskRecord{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA2", Agent: "codex", Prompt: "stale", WorkDir: dir, Status: "running", CreatedAt: "2026-01-01T00:00:00Z", IdempotencyKey: "scan-stale"}); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	detail, err := s.GetTask("01ARZ3NDEKTSV4RRFFQ69G5FA2")
	if err != nil || detail.Task.Status != "failed" {
		t.Fatalf("%+v %v", detail, err)
	}
	found := false
	for _, art := range detail.Artifacts {
		if art.Name == "kept.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recover should keep leftover files: %+v", detail.Artifacts)
	}
}

func TestRecoverFailsStaleRunningAndStartsQueued(t *testing.T) {
	s := testService(t)
	dir := t.TempDir()
	if err := s.Store.InsertTask(TaskRecord{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA0", Agent: "codex", Prompt: "stale", WorkDir: dir, Status: "running", CreatedAt: "2026-01-01T00:00:00Z", IdempotencyKey: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.InsertTask(TaskRecord{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", Agent: "codex", Prompt: "queued", WorkDir: dir, Status: "queued", CreatedAt: "2026-01-01T00:00:01Z", IdempotencyKey: "queued"}); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	stale, err := s.Store.GetTask("01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if err != nil || stale.Status != "failed" || stale.ErrorMsg == "" {
		t.Fatalf("%+v %v", stale, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var queued TaskDetail
	for time.Now().Before(deadline) {
		queued, err = s.GetTask("01ARZ3NDEKTSV4RRFFQ69G5FA1")
		if err == nil && queued.Task.Status == "success" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("queued task after recover: %+v", queued)
}

func TestSameAgentSecondTaskStaysQueuedUntilFirstFinishes(t *testing.T) {
	s := testService(t)
	hold := make(chan struct{})
	started := make(chan string, 2)
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		started <- string(spec.Stdin)
		if string(spec.Stdin) == "first" {
			<-hold
		}
		return 0, false, nil
	}
	first, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "first", IdempotencyKey: "q1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "second", TimeoutMin: 120, IdempotencyKey: "q2"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		second, _ = s.GetTask(second.Task.ID)
		if second.Task.Status == "queued" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if second.Task.Status != "queued" {
		t.Fatalf("second should wait: %+v", second.Task)
	}
	close(hold)
	resumeUntil := time.Now().Add(2 * time.Second)
	for time.Now().Before(resumeUntil) {
		second, _ = s.GetTask(second.Task.ID)
		if second.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if second.Task.Status != "success" {
		t.Fatalf("queued task did not resume: %+v", second.Task)
	}
	_ = first
	if len(started) < 2 {
		t.Fatalf("expected both tasks to start, got %d", len(started))
	}
}

func TestQueuePreservesCustomTimeout(t *testing.T) {
	s := testService(t)
	hold := make(chan struct{})
	got := make(chan time.Duration, 2)
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got <- spec.Timeout
		if string(spec.Stdin) == "first" {
			<-hold
		}
		return 0, false, nil
	}
	if _, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "first", TimeoutMin: 30, IdempotencyKey: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "second", TimeoutMin: 120, IdempotencyKey: "t2"}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-got:
		if d != 30*time.Minute {
			t.Fatalf("first timeout %s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first did not start")
	}
	close(hold)
	select {
	case d := <-got:
		if d != 120*time.Minute {
			t.Fatalf("queued timeout lost: %s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second did not start")
	}
}

func TestGetTaskListsLiveFilesWhileRunning(t *testing.T) {
	s := testService(t)
	started := make(chan struct{})
	release := make(chan struct{})
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		close(started)
		<-release
		return 0, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "live", IdempotencyKey: "live"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("did not start")
	}
	detail, err = s.GetTask(detail.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(detail.Task.WorkDir, "live.md"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detail, err = s.GetTask(detail.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, art := range detail.Artifacts {
		if art.Name == "live.md" {
			found = true
		}
	}
	close(release)
	if !found {
		t.Fatalf("running get should surface live.md: %+v", detail.Artifacts)
	}
}

func TestStartUsesRequestedWorkDir(t *testing.T) {
	s := testService(t)
	dir := t.TempDir()
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "in-project", WorkDir: dir, IdempotencyKey: "wd1"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.WorkDir != filepath.Clean(dir) {
		t.Fatalf("work dir %q", detail.Task.WorkDir)
	}
}

func TestChooseDirRejectsSystemRootsAndCancel(t *testing.T) {
	s := testService(t)
	s.Pick = func() (string, error) { return `C:\Windows`, nil }
	if _, err := s.ChooseDir(); err == nil {
		t.Fatal("system dir must stay rejected")
	}
	s.Pick = func() (string, error) { return "", ErrPickCanceled }
	if _, err := s.ChooseDir(); !errors.Is(err, ErrPickCanceled) {
		t.Fatalf("%v", err)
	}
	want := t.TempDir()
	s.Pick = func() (string, error) { return want, nil }
	got, err := s.ChooseDir()
	if err != nil || got != filepath.Clean(want) {
		t.Fatalf("%q %v", got, err)
	}
}

func TestResolveWorkDirRejectsSystemRoots(t *testing.T) {
	s := testService(t)
	if _, err := s.resolveWorkDir(`C:\`); err == nil {
		t.Fatal("drive root should be rejected")
	}
	if _, err := s.resolveWorkDir(`C:\Windows\System32`); err == nil {
		t.Fatal("windows dir should be rejected")
	}
	if _, err := s.resolveWorkDir(s.Root); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyTruncatesBodyAndIgnoresError(t *testing.T) {
	s := testService(t)
	var title, body string
	s.Notify = func(t, b string) error {
		title, body = t, b
		return errors.New("toast failed")
	}
	long := strings.Repeat("周报", 400)
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: long, IdempotencyKey: "toast"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "success" && title != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if title != "Lunitide" {
		t.Fatalf("title %q", title)
	}
	if len([]rune(body)) > 500 {
		t.Fatalf("body too long: %d", len([]rune(body)))
	}
}

func TestFailedUnsupportedModelUsesChinese(t *testing.T) {
	s := testService(t)
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`{"type":"failed","detail":"The 'gpt-6-astra' model requires a newer version of Codex. Please upgrade to the latest app or CLI and try again."}`)
		return 1, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hi", IdempotencyKey: "astra"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(detail.Task.ErrorMsg, "模型") {
		t.Fatalf("%q", detail.Task.ErrorMsg)
	}
}

func TestFailedKimiNoModelConfiguredUsesChinese(t *testing.T) {
	s := testService(t)
	s.Look = func(string) (string, error) { return `C:\kimi.exe`, nil }
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`error: failed to run prompt: No model configured. Run kimi and use /login to sign in, then retry; or set default_model in config.toml.`)
		return 1, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "kimi", Prompt: "hi", IdempotencyKey: "kimi-nologin"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(detail.Task.ErrorMsg, "登录") {
		t.Fatalf("%q", detail.Task.ErrorMsg)
	}
}

func TestFailedLoginUsesChinese(t *testing.T) {
	s := testService(t)
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`{"type":"failed","detail":"Please login with ` + "`codex login`" + ` first"}`)
		return 1, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hi", IdempotencyKey: "login"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(detail.Task.ErrorMsg, "登录") {
		t.Fatalf("%q", detail.Task.ErrorMsg)
	}
}

func TestFailedRateLimitUsesChinese(t *testing.T) {
	s := testService(t)
	s.Start = func(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		onLine(`{"type":"failed","detail":"Rate limit exceeded (429)"}`)
		return 1, false, nil
	}
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hi", IdempotencyKey: "rl"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(detail.Task.ErrorMsg, "限流") {
		t.Fatalf("%q", detail.Task.ErrorMsg)
	}
}

func TestIdempotentStartReturnsSameTask(t *testing.T) {
	s := testService(t)
	first, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "same", IdempotencyKey: "idem"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "same", IdempotencyKey: "idem"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Task.ID != second.Task.ID {
		t.Fatalf("%s vs %s", first.Task.ID, second.Task.ID)
	}
}

func TestSafePathRejectsJunctionEscape(t *testing.T) {
	s := testService(t)
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hello", IdempotencyKey: "junction"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(detail.Task.WorkDir, "out")
	if out, linkErr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); linkErr != nil {
		t.Skipf("mklink: %v %s", linkErr, out)
	}
	if _, err = s.ResolveFile(detail.Task.ID, filepath.Join("out", "secret.txt")); !errors.Is(err, ErrPathOutside) {
		t.Fatalf("%v", err)
	}
}

func TestPreviewRejectsEscape(t *testing.T) {
	s := testService(t)
	detail, err := s.StartTask(TaskRequest{Agent: "codex", Prompt: "hello", IdempotencyKey: "k3"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, _ = s.GetTask(detail.Task.ID)
		if detail.Task.Status == "success" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err = s.ResolveFile(detail.Task.ID, `..\Windows\win.ini`)
	if !errors.Is(err, ErrPathOutside) {
		t.Fatalf("%v", err)
	}
}
