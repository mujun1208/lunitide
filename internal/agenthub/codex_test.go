package agenthub

import (
	"strings"
	"testing"
)

func TestCodexBuildCommand(t *testing.T) {
	exe, args, stdin, err := (codexAdapter{}).BuildCommand(TaskRequest{Prompt: "hi", WorkDir: `D:\work`, Sandbox: "read-only"})
	if err != nil || exe != "codex" || string(stdin) != "hi" {
		t.Fatalf("%s %v %q %v", exe, args, stdin, err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "exec") || !strings.Contains(joined, "--json") || !strings.Contains(joined, "--skip-git-repo-check") || !strings.Contains(joined, "--ignore-user-config") || !strings.Contains(joined, `D:\work`) || !strings.Contains(joined, "codex-last-message.md") {
		t.Fatalf("argv missing hub flags: %v", args)
	}
}
