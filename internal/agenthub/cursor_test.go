package agenthub

import (
	"strings"
	"testing"
)

func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestCursorBuildCommand(t *testing.T) {
	exe, args, stdin, err := (cursorAdapter{}).BuildCommand(TaskRequest{Prompt: "hi", WorkDir: `D:\work`})
	if err != nil || exe != "cursor-agent" {
		t.Fatalf("%s %v", exe, err)
	}
	joined := strings.Join(args, " ")
	if string(stdin) != "hi" || !strings.Contains(joined, "-p") || !strings.Contains(joined, "--force") || !strings.Contains(joined, "--trust") || !strings.Contains(joined, "--output-format") || !strings.Contains(joined, "stream-json") || !hasPair(args, "--workspace", `D:\work`) {
		t.Fatalf("%v %q", args, stdin)
	}
}
