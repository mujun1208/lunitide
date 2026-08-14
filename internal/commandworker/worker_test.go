package commandworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestWorkerHelperProcess re-executes the test binary as the job's child.
// Behavior is driven by the argv after "--": echo/exit/sleep/write/tree.
func TestWorkerHelperProcess(t *testing.T) {
	if os.Getenv("LUNITIDE_WORKER_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	switch args[0] {
	case "echo":
		fmt.Fprintln(os.Stdout, args[1])
		fmt.Fprintln(os.Stderr, args[1])
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(args[1])
		os.Exit(code)
	case "sleep":
		seconds, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(seconds) * time.Second)
		os.Exit(0)
	case "write":
		n, _ := strconv.Atoi(args[1])
		chunk := strings.Repeat("a", 4096)
		for n > 0 {
			n -= len(chunk)
			fmt.Fprint(os.Stdout, chunk)
		}
		os.Exit(0)
	case "tree":
		// Write own pid, spawn a sleeping grandchild that writes its pid,
		// then sleep: the worker must kill both on timeout.
		_ = os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0o600)
		grandchild := os.Args[0]
		child := exec.Command(grandchild, append(helperArgs(), "treeleaf", args[2])...)
		child.Env = append(os.Environ(), "LUNITIDE_WORKER_HELPER=1")
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	case "treeleaf":
		_ = os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
	os.Exit(2)
}

func helperArgs() []string {
	return []string{"-test.run", "^TestWorkerHelperProcess$", "--"}
}

func helperSpec(t *testing.T, dir string, args ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"LUNITIDE_WORKER_HELPER=1"}
	if runtime.GOOS == "windows" {
		if root := os.Getenv("SystemRoot"); root != "" {
			env = append(env, "SystemRoot="+root)
		}
	} else {
		env = append(env, "PATH="+os.Getenv("PATH"))
	}
	return Spec{
		Exe:     exe,
		Args:    append(helperArgs(), args...),
		Dir:     dir,
		Env:     env,
		Timeout: 30 * time.Second,
	}
}

func TestSpecValidate(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := Spec{Exe: exe, Dir: dir}
	if err := base.Validate(); err != nil {
		t.Fatalf("base spec: %v", err)
	}
	if base.Timeout != DefaultTimeout || base.MaxOutputBytes != DefaultOutputCap {
		t.Fatalf("limits not normalized: %+v", base)
	}
	cases := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"relative exe", func(s *Spec) { s.Exe = "go" }},
		{"relative dir", func(s *Spec) { s.Dir = "tmp" }},
		{"timeout above cap", func(s *Spec) { s.Timeout = TimeoutHardCap + time.Second }},
		{"output above cap", func(s *Spec) { s.MaxOutputBytes = OutputHardCap + 1 }},
		{"arg with NUL", func(s *Spec) { s.Args = []string{"a\x00b"} }},
		{"env without key", func(s *Spec) { s.Env = []string{"=value"} }},
		{"env with newline", func(s *Spec) { s.Env = []string{"A=B\nC"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			tc.mutate(&spec)
			if err := spec.Validate(); !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("got %v, want ErrInvalidSpec", err)
			}
		})
	}
}

func TestRunCompletesWithOutput(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "echo", "hello-worker")
	var out strings.Builder
	outcome, err := Run(context.Background(), spec, nil, func(p []byte) { out.Write(p) })
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ExitCode != 0 || outcome.TimedOut || outcome.Truncated {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if !strings.Contains(out.String(), "hello-worker") {
		t.Fatalf("output %q missing echoed text", out.String())
	}
	if outcome.OutputBytes != int64(out.Len()) {
		t.Fatalf("output bytes %d != delivered %d", outcome.OutputBytes, out.Len())
	}
}

func TestRunPropagatesNonZeroExit(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "exit", "3")
	outcome, err := Run(context.Background(), spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", outcome.ExitCode)
	}
}

func TestCappedSinkOnlyMarksActuallyDroppedBytesTruncated(t *testing.T) {
	var out strings.Builder
	sink := cappedSink{limit: 3, cb: func(p []byte) { out.Write(p) }}
	sink.Write([]byte("abc"))
	if sink.truncated || sink.delivered != 3 || out.String() != "abc" {
		t.Fatalf("exact cap was marked truncated: %+v output=%q", sink, out.String())
	}
	sink.Write([]byte("d"))
	if !sink.truncated || sink.delivered != 3 || out.String() != "abc" {
		t.Fatalf("dropped byte was not marked truncated: %+v output=%q", sink, out.String())
	}
}

func TestRunTimeoutKillsProcess(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "sleep", "300")
	spec.Timeout = 100 * time.Millisecond
	start := time.Now()
	outcome, err := Run(context.Background(), spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.TimedOut || outcome.ExitCode != -1 {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatal("timeout did not kill the process")
	}
}

func TestRunCapsOutput(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "write", strconv.Itoa(2<<20))
	spec.MaxOutputBytes = 64 << 10
	var received int64
	outcome, err := Run(context.Background(), spec, nil, func(p []byte) { received += int64(len(p)) })
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Truncated {
		t.Fatal("expected truncation")
	}
	if outcome.OutputBytes != 64<<10 || received != 64<<10 {
		t.Fatalf("delivered %d/%d, want exactly the cap", outcome.OutputBytes, received)
	}
}

func TestRunCancelKillsProcess(t *testing.T) {
	spec := helperSpec(t, t.TempDir(), "sleep", "300")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := Run(ctx, spec, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestRunRejectsInvalidSpec(t *testing.T) {
	if _, err := Run(context.Background(), Spec{Exe: "go", Dir: t.TempDir()}, nil, nil); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("got %v, want ErrInvalidSpec", err)
	}
}

// pidAlive reports whether a process with the given pid still exists.
func pidAlive(t *testing.T, pidFile string) bool {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pid file content: %v", err)
	}
	return processAlive(pid)
}

func TestRunTimeoutKillsProcessTree(t *testing.T) {
	if testing.Short() {
		t.Skip("tree kill timing")
	}
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	leafPID := filepath.Join(dir, "leaf.pid")
	spec := helperSpec(t, dir, "tree", childPID, leafPID)
	spec.Timeout = 500 * time.Millisecond
	outcome, err := Run(context.Background(), spec, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.TimedOut {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	// The timeout path must have killed the helper and its grandchild:
	// 取消/超时后无孤儿进程 (PRD M4 门禁).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err1 := os.Stat(childPID); err1 == nil {
			if _, err2 := os.Stat(leafPID); err2 == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pidAlive(t, childPID) {
		t.Fatal("child process survived job timeout")
	}
	if pidAlive(t, leafPID) {
		t.Fatal("grandchild process survived job timeout (orphan)")
	}
}
