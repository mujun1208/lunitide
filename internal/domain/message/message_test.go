package message

import (
	"strings"
	"testing"
	"time"
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

func TestNormalizeAssistantTextFrozenRules(t *testing.T) {
	// CRLF / CR normalization without trimming.
	got, err := NormalizeAssistantText("  a\r\nb\rc  ")
	if err != nil || got != "  a\nb\nc  " {
		t.Fatalf("got %q, %v", got, err)
	}
	// Empty rejected.
	if _, err := NormalizeAssistantText(""); err == nil {
		t.Fatal("accepted empty assistant text")
	}
	// NUL rejected.
	if _, err := NormalizeAssistantText("a\x00b"); err == nil {
		t.Fatal("accepted NUL in assistant text")
	}
	// MaxRunesAssistant boundary accepted.
	boundary := strings.Repeat("a", MaxRunesAssistant)
	if got, err := NormalizeAssistantText(boundary); err != nil || got != boundary {
		t.Fatalf("assistant boundary rejected: err=%v", err)
	}
	// MaxRunesAssistant + 1 rejected.
	if _, err := NormalizeAssistantText(strings.Repeat("a", MaxRunesAssistant+1)); err == nil {
		t.Fatal("accepted assistant text exceeding MaxRunesAssistant")
	}
	// Supplementary plane: 4 bytes per rune. MaxRunesAssistant runes = MaxBytesAssistant bytes.
	// This tests both rune and byte boundaries simultaneously.
	supplementary := strings.Repeat("😀", MaxRunesAssistant)
	if got, err := NormalizeAssistantText(supplementary); err != nil || got != supplementary {
		t.Fatalf("supplementary assistant boundary rejected: bytes=%d err=%v", len(got), err)
	}
	// Supplementary plane exceeding rune limit (and byte limit) rejected.
	if _, err := NormalizeAssistantText(strings.Repeat("😀", MaxRunesAssistant+1)); err == nil {
		t.Fatal("accepted supplementary assistant text exceeding MaxRunesAssistant")
	}
}

func TestMessageValidateAssistantRole(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Valid assistant message.
	valid := Message{ID: id, SessionID: id, Role: RoleAssistant, Status: StatusCompleted, Sequence: 1, Text: "I am fine.", CreatedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid assistant message rejected: %v", err)
	}
	// Assistant with failed status rejected (only completed allowed per frozen design).
	failed := valid
	failed.Status = StatusFailed
	if err := failed.Validate(); err == nil {
		t.Fatal("accepted assistant message with failed status")
	}
	// Assistant text exceeding assistant limit rejected.
	oversized := valid
	oversized.Text = strings.Repeat("a", MaxRunesAssistant+1)
	if err := oversized.Validate(); err == nil {
		t.Fatal("accepted assistant message with oversized text")
	}
	// Assistant text within assistant limit but exceeding user limit accepted.
	wideText := valid
	wideText.Text = strings.Repeat("a", MaxRunes+1)
	if err := wideText.Validate(); err != nil {
		t.Fatalf("assistant text within assistant limit but exceeding user limit rejected: %v", err)
	}
	// Unknown role rejected.
	unknown := valid
	unknown.Role = "system"
	if err := unknown.Validate(); err == nil {
		t.Fatal("accepted unknown role")
	}
}

func TestMessageValidateToolRole(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	// Valid tool message (tool results can be JSON, using assistant text limits).
	valid := Message{ID: id, SessionID: id, Role: RoleTool, Status: StatusCompleted, Sequence: 1, Text: `{"result":"ok"}`, CreatedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid tool message rejected: %v", err)
	}
	// Tool with failed status rejected.
	failed := valid
	failed.Status = StatusFailed
	if err := failed.Validate(); err == nil {
		t.Fatal("accepted tool message with failed status")
	}
	// Tool text exceeding assistant limit rejected.
	oversized := valid
	oversized.Text = strings.Repeat("a", MaxRunesAssistant+1)
	if err := oversized.Validate(); err == nil {
		t.Fatal("accepted tool message with oversized text")
	}
	// Tool text within assistant limit but exceeding user limit accepted.
	wideText := valid
	wideText.Text = strings.Repeat("a", MaxRunes+1)
	if err := wideText.Validate(); err != nil {
		t.Fatalf("tool text within assistant limit but exceeding user limit rejected: %v", err)
	}
}
