//go:build windows

package agenthub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildFakeCLI(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "fake.exe")
	cmd := exec.Command("go", "build", "-o", exe, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake cli: %v\n%s", err, out)
	}
	return exe
}

func TestStartProcessWritesStdinAndLines(t *testing.T) {
	exe := buildFakeCLI(t, `package main
import ("fmt"; "io"; "os")
func main() {
  b, _ := io.ReadAll(os.Stdin)
  fmt.Println("{\"type\":\"started\",\"title\":\"start\"}")
  fmt.Printf("{\"type\":\"message\",\"detail\":%q}\n", string(b))
  _ = os.WriteFile("hello.txt", []byte("ok"), 0644)
}
`)
	dir := t.TempDir()
	var lines []string
	exit, timedOut, err := StartProcess(context.Background(), ProcSpec{Exe: exe, Dir: dir, Stdin: []byte("full-prompt"), Timeout: 20 * time.Second}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil || timedOut || exit != 0 {
		t.Fatalf("exit=%d timeout=%v err=%v lines=%v", exit, timedOut, err, lines)
	}
	if len(lines) < 2 || !strings.Contains(strings.Join(lines, "\n"), "full-prompt") {
		t.Fatalf("stdin not streamed: %v", lines)
	}
	if _, err = os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestStartProcessCancelKills(t *testing.T) {
	exe := buildFakeCLI(t, `package main
import "time"
func main() { time.Sleep(8 * time.Second) }
`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := StartProcess(ctx, ProcSpec{Exe: exe, Dir: t.TempDir(), Timeout: time.Minute}, nil)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errorsIsCanceled(err) {
			t.Fatalf("%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop process")
	}
}

func TestStartProcessRunsCmdShim(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "shim.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\necho {\"type\":\"message\",\"detail\":\"from-cmd\"}\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	exit, timedOut, err := StartProcess(context.Background(), ProcSpec{Exe: script, Dir: dir, Timeout: 20 * time.Second}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil || timedOut || exit != 0 {
		t.Fatalf("exit=%d timeout=%v err=%v lines=%v", exit, timedOut, err, lines)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "from-cmd") {
		t.Fatalf("cmd shim not executed: %v", lines)
	}
}

func TestStartProcessKeepsReadingAfterHugeLine(t *testing.T) {
	exe := buildFakeCLI(t, `package main
import ("fmt"; "strings")
func main() {
  fmt.Println(strings.Repeat("x", 5<<20))
  fmt.Println("{\"type\":\"message\",\"detail\":\"after-huge\"}")
}
`)
	var lines []string
	exit, timedOut, err := StartProcess(context.Background(), ProcSpec{Exe: exe, Dir: t.TempDir(), Timeout: 20 * time.Second}, func(line string) {
		lines = append(lines, line)
	})
	if err != nil || timedOut || exit != 0 {
		t.Fatalf("exit=%d timeout=%v err=%v", exit, timedOut, err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "after-huge") {
		t.Fatalf("huge line stopped the stream: %d lines", len(lines))
	}
}

func TestStartProcessTimeout(t *testing.T) {
	exe := buildFakeCLI(t, `package main
import "time"
func main() { time.Sleep(5 * time.Second) }
`)
	_, timedOut, err := StartProcess(context.Background(), ProcSpec{Exe: exe, Dir: t.TempDir(), Timeout: 200 * time.Millisecond}, nil)
	if err != nil || !timedOut {
		t.Fatalf("timeout=%v err=%v", timedOut, err)
	}
}

func errorsIsCanceled(err error) bool {
	return err != nil && (err == context.Canceled || strings.Contains(err.Error(), "canceled") || strings.Contains(err.Error(), "cancelled"))
}
