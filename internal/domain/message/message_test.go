package message

import (
	"strings"
	"testing"
)

func TestNormalizeTextFrozenRules(t *testing.T) {
	got, err := NormalizeText("  a\r\nb\rc  ")
	if err != nil || got != "  a\nb\nc  " {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, input := range []string{"", strings.Repeat("a", MaxRunes+1), strings.Repeat("😀", MaxBytes/4+1)} {
		if _, err := NormalizeText(input); err == nil {
			t.Fatalf("accepted invalid text of %d bytes", len(input))
		}
	}
	if _, err := NormalizeText(strings.Repeat("😀", MaxBytes/4)); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeText("a\x00b"); err == nil {
		t.Fatal("accepted NUL")
	}
	boundary := strings.Repeat("😀", MaxRunes)
	if got, err := NormalizeText(boundary); err != nil || got != boundary || len(got) != MaxBytes {
		t.Fatalf("supplementary Unicode boundary rejected: bytes=%d err=%v", len(got), err)
	}
}
