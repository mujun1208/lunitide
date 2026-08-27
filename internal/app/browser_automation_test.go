package app

import "testing"

func TestAppendPostActSnapshot(t *testing.T) {
	if got := appendPostActSnapshot("snapshot", "tree", "ignored"); got != "tree" {
		t.Fatalf("snapshot op should not wrap itself: %q", got)
	}
	if got := appendPostActSnapshot("click", "clicked", ""); got != "clicked" {
		t.Fatalf("empty follow-up: %q", got)
	}
	got := appendPostActSnapshot("click", "clicked B1", `{"refs":["B2"]}`)
	if got != "clicked B1\n\n[snapshot after click]\n{\"refs\":[\"B2\"]}" {
		t.Fatalf("got %q", got)
	}
	if got := appendPostActSnapshot("navigate", "", "tree-only"); got != "tree-only" {
		t.Fatalf("empty primary: %q", got)
	}
}
