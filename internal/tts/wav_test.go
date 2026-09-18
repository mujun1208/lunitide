package tts

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConcatWAVJoinsSameFormatClips(t *testing.T) {
	a := PCM16MonoWAV(16000, []byte{1, 0, 2, 0})
	b := PCM16MonoWAV(16000, []byte{3, 0, 4, 0})
	got, err := ConcatWAV([][]byte{a, b})
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := parseWAV(got)
	if err != nil || !bytes.Equal(data, []byte{1, 0, 2, 0, 3, 0, 4, 0}) {
		t.Fatalf("pcm = %v err=%v", data, err)
	}
}

func TestConcatWAVRejectsFormatMismatch(t *testing.T) {
	a := PCM16MonoWAV(16000, []byte{1, 0})
	b := PCM16MonoWAV(8000, []byte{2, 0})
	if _, err := ConcatWAV([][]byte{a, b}); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestSplitSpeechSegmentsRespectsCap(t *testing.T) {
	text := strings.Repeat("春风又绿江南岸。", 80)
	parts := SplitSpeechSegments(text, MaxSegmentChars)
	if len(parts) < 2 {
		t.Fatalf("parts=%d text=%d", len(parts), utf8.RuneCountInString(text))
	}
	joined := strings.Join(parts, "")
	compact := strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\n", "")
	got := strings.ReplaceAll(strings.ReplaceAll(joined, " ", ""), "\n", "")
	if got != compact {
		t.Fatalf("lost text: in=%d out=%d", len(compact), len(got))
	}
	for _, p := range parts {
		if utf8.RuneCountInString(p) > MaxSegmentChars {
			t.Fatalf("segment too long: %d", utf8.RuneCountInString(p))
		}
	}
}
