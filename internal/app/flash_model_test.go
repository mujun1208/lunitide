package app

import "testing"

func TestIsFlashModelID(t *testing.T) {
	t.Parallel()
	yes := []string{
		"doubao-lite-32k", "gemini-2.0-flash", "claude-3-haiku",
		"gpt-4o-mini", "DeepSeek-V3-air",
	}
	no := []string{
		"", "gpt-4o", "claude-3-5-sonnet",
		"gpt-4o-realtime-preview", "gemini-live-2.5",
	}
	for _, id := range yes {
		if !isFlashModelID(id) {
			t.Fatalf("%q should be flash", id)
		}
	}
	for _, id := range no {
		if isFlashModelID(id) {
			t.Fatalf("%q should not be flash", id)
		}
	}
}

func TestPickFlashModelID(t *testing.T) {
	t.Parallel()
	cands := []string{"gpt-4o", "gpt-4o-mini", "o1"}
	if got := pickFlashModelID("gpt-4o", cands); got != "gpt-4o" {
		t.Fatalf("keep current: %q", got)
	}
	if got := pickFlashModelID("missing-plus", cands); got != "gpt-4o-mini" {
		t.Fatalf("D-C1 pick flash: %q", got)
	}
	if got := pickFlashModelID("only-plus", []string{"gpt-4o", "claude-sonnet"}); got != "only-plus" {
		t.Fatalf("no flash stays current: %q", got)
	}
	if got := pickJudgeModelID("gpt-4o", cands); got != "gpt-4o-mini" {
		t.Fatalf("D-C1 judge prefers flash: %q", got)
	}
}
