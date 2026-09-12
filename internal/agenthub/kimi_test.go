package agenthub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKimiDetectAvailableWhenInstalled(t *testing.T) {
	st := (kimiAdapter{}).Detect(func(string) (string, error) { return `C:\kimi.exe`, nil }, func(string, time.Duration) (string, error) {
		return "kimi 1.2.3", nil
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("%+v", st)
	}
}

func TestKimiBuildCommand(t *testing.T) {
	dir := t.TempDir()
	exe, args, stdin, err := (kimiAdapter{}).BuildCommand(TaskRequest{Prompt: "写 hello.txt", WorkDir: dir})
	if err != nil || exe != "kimi" || len(stdin) != 0 {
		t.Fatalf("%s %v %q %v", exe, args, stdin, err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "写 hello.txt") {
		t.Fatalf("long prompt must not sit on argv: %v", args)
	}
	if !strings.Contains(joined, "-p") || !strings.Contains(joined, "stream-json") {
		t.Fatalf("%v", args)
	}
	body, err := os.ReadFile(filepath.Join(dir, promptFileName))
	if err != nil || !strings.Contains(string(body), "写 hello.txt") {
		t.Fatalf("%q %v", body, err)
	}
}

func TestKimiBuildCommandRequiresWorkDir(t *testing.T) {
	_, _, _, err := (kimiAdapter{}).BuildCommand(TaskRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected work dir error")
	}
}
