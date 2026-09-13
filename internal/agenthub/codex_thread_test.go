package agenthub

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexThreadArgvOmitsIgnoreUserConfig(t *testing.T) {
	exe, args := codexThreadArgv(`D:\work`, "workspace-write")
	if exe != "codex" {
		t.Fatalf("exe = %q, want codex", exe)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "exec") || !strings.Contains(joined, "--json") || !strings.Contains(joined, "--skip-git-repo-check") || !strings.Contains(joined, "--sandbox") || !strings.Contains(joined, "--cd") {
		t.Fatalf("thread argv missing flags: %v", args)
	}
	if !hasPair(args, "--sandbox", "workspace-write") || !hasPair(args, "--cd", `D:\work`) {
		t.Fatalf("sandbox/cd = %v", args)
	}
	if !strings.Contains(joined, filepath.Join(`D:\work`, "codex-last-message.md")) {
		t.Fatalf("missing last-message output: %v", args)
	}
	if strings.Contains(joined, "--ignore-user-config") {
		t.Fatalf("thread argv must not include --ignore-user-config: %v", args)
	}
}

func TestCodexThreadArgvDefaultsSandbox(t *testing.T) {
	_, args := codexThreadArgv(`D:\work`, "")
	if !hasPair(args, "--sandbox", "workspace-write") {
		t.Fatalf("default sandbox = %v", args)
	}
	if strings.Contains(strings.Join(args, " "), "--ignore-user-config") {
		t.Fatalf("thread argv must not include --ignore-user-config: %v", args)
	}
}

func TestDetectCodexAvailableHintCannotAsk(t *testing.T) {
	want := "当前只能一把跑完，不能中途提问"
	st := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "codex-cli 0.1.0", nil
	})
	if st.State != "available" || st.Interactive || st.Protocol != "exec" {
		t.Fatalf("codex detect = %+v, want available/false/exec", st)
	}
	if st.Hint != want {
		t.Fatalf("hint = %q, want %q", st.Hint, want)
	}

	timeout := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "", errors.New("timeout")
	})
	if timeout.State != "available" || timeout.Hint != want {
		t.Fatalf("timeout available hint = %+v", timeout)
	}

	cursor := detectOne("cursor", func(string) (string, error) { return `C:\cursor-agent.cmd`, nil }, func(string, time.Duration) (string, error) {
		return "2026.09.10", nil
	})
	if cursor.Hint != "可用" {
		t.Fatalf("cursor hint = %q, want 可用", cursor.Hint)
	}
}

func TestThreadAdapterWiresCodex(t *testing.T) {
	s := &Service{Threads: NewThreadStore(openThreadDB(t))}
	adapter, err := s.threadAdapter("codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*CodexThread); !ok {
		t.Fatalf("codex adapter = %T", adapter)
	}
}

func TestCodexThreadPromptRunsExecWithoutIgnore(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	var got ProcSpec
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(_ context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got = spec
		onLine(`{"type":"agent.message","text":"done as-is"}`)
		return 0, false, nil
	}
	if err := adapter.Prompt(thread.ID, "user as-is"); err != nil {
		t.Fatal(err)
	}
	if string(got.Stdin) != "user as-is" {
		t.Fatalf("stdin = %q, want user text as-is", got.Stdin)
	}
	if got.Dir != thread.WorkspaceRoot {
		t.Fatalf("dir = %q", got.Dir)
	}
	joined := strings.Join(got.Args, " ")
	if strings.Contains(joined, "--ignore-user-config") {
		t.Fatalf("prompt argv has ignore: %v", got.Args)
	}
	if !strings.Contains(joined, "exec") || !strings.Contains(joined, "--json") || !strings.Contains(joined, "--skip-git-repo-check") {
		t.Fatalf("prompt argv missing exec flags: %v", got.Args)
	}
	gotThread, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotThread.Status != "success" {
		t.Fatalf("status = %q, want success", gotThread.Status)
	}
	role, content := loadLastMessage(t, store.db, thread.ID)
	if role != "assistant" || !strings.Contains(content, "done as-is") {
		t.Fatalf("assistant = %s %q", role, content)
	}
}

func TestCodexThreadPromptStdinIncludesStoredSystem(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	if err := insertThreadMessage(store, thread.ID, "system", sceneFix); err != nil {
		t.Fatal(err)
	}
	var got ProcSpec
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(_ context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
		got = spec
		onLine(`{"type":"agent.message","text":"ok"}`)
		return 0, false, nil
	}
	if err := adapter.Prompt(thread.ID, "user as-is"); err != nil {
		t.Fatal(err)
	}
	stdin := string(got.Stdin)
	if !strings.Contains(stdin, sceneFix) {
		t.Fatalf("stdin missing system text: %q", stdin)
	}
	if !strings.Contains(stdin, "user as-is") {
		t.Fatalf("stdin missing user text: %q", stdin)
	}
}

func TestCodexThreadPromptReadsLastMessageFile(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(_ context.Context, spec ProcSpec, _ func(string)) (int64, bool, error) {
		if err := os.WriteFile(filepath.Join(spec.Dir, "codex-last-message.md"), []byte("from last-message"), 0o644); err != nil {
			return 1, false, err
		}
		return 0, false, nil
	}
	if err := adapter.Prompt(thread.ID, "hi"); err != nil {
		t.Fatal(err)
	}
	role, content := loadLastMessage(t, store.db, thread.ID)
	if role != "assistant" || content != "from last-message" {
		t.Fatalf("assistant = %s %q", role, content)
	}
}

func TestCodexThreadPromptFaultsOnStartError(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return "", errors.New("not found") }
	if err := adapter.Prompt(thread.ID, "hi"); err != nil {
		t.Fatalf("persisted fault must return nil so GetThread can load it: %v", err)
	}
	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
	role, content := loadFirstUserMessage(t, store.db, thread.ID)
	if role != "user" || content != "hi" {
		t.Fatalf("user row = %s %q", role, content)
	}
}

func TestCodexThreadPromptFaultsOnRunError(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(context.Context, ProcSpec, func(string)) (int64, bool, error) {
		return 1, false, errors.New("exec failed")
	}
	if err := adapter.Prompt(thread.ID, "run me"); err != nil {
		t.Fatalf("persisted fault must return nil so GetThread can load it: %v", err)
	}
	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "faulted" {
		t.Fatalf("status = %q, want faulted", got.Status)
	}
	role, content := loadFirstUserMessage(t, store.db, thread.ID)
	if role != "user" || content != "run me" {
		t.Fatalf("user row = %s %q", role, content)
	}
}

func loadFirstUserMessage(t *testing.T, db *sql.DB, threadID string) (role, content string) {
	t.Helper()
	err := db.QueryRow(`SELECT role, content FROM agent_hub_messages WHERE thread_id=? AND role='user' ORDER BY seq LIMIT 1`, threadID).Scan(&role, &content)
	if err != nil {
		t.Fatal(err)
	}
	return role, content
}

func TestCodexThreadCloseCancelsInFlightPrompt(t *testing.T) {
	store := NewThreadStore(openThreadDB(t))
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "codex", "Exec", false)
	thread.WorkspaceRoot = t.TempDir()
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	adapter := NewCodexThread(store)
	adapter.look = func(string) (string, error) { return `C:\fake-codex.exe`, nil }
	adapter.start = func(ctx context.Context, _ ProcSpec, _ func(string)) (int64, bool, error) {
		close(started)
		<-ctx.Done()
		return 1, false, ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- adapter.Prompt(thread.ID, "long") }()
	<-started
	if err := adapter.Close(thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := setThreadStatus(store, thread.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("prompt after cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt did not return after Close")
	}
	got, err := store.Get(thread.ID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("status = %#v %v", got, err)
	}
}

func TestCodexThreadRespondErrors(t *testing.T) {
	adapter := NewCodexThread(NewThreadStore(openThreadDB(t)))
	if err := adapter.Respond("x", "c", "yes", ""); err == nil {
		t.Fatal("exec cannot respond mid-turn")
	}
}
