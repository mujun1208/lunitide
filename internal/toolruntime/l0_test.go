package toolruntime

import (
	"strings"
	"testing"
)

func TestAppendL0JSONKeepsOpenedPrefix(t *testing.T) {
	got := appendL0JSON("opened soda", "foreground", true, false, "soda")
	if !strings.HasPrefix(got, "opened ") {
		t.Fatalf("companion settle prefix lost: %q", got)
	}
	if !strings.Contains(got, `"l0"`) || !strings.Contains(got, `"kind":"foreground"`) {
		t.Fatalf("l0 missing: %q", got)
	}
}
