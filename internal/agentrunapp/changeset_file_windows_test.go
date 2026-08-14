//go:build windows

package agentrunapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsAtomicReplaceAcrossRepeatedOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "replace.txt")
	if err := os.WriteFile(p, []byte("zero"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"one", "two", "three"} {
		if err := atomicReplace(p, []byte(body)); err != nil {
			t.Fatalf("replace %q: %v", body, err)
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != body {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}
