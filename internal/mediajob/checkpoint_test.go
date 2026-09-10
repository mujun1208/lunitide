package mediajob

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSegmentResumeAndCancel(t *testing.T) {
	j := Journal{File: "talk.wav", Total: 4, Done: []int{0, 1}}
	if got := Remaining(j); len(got) != 2 || got[0] != 2 {
		t.Fatalf("resume remaining: %+v", got)
	}
	j.Cancel = true
	if got := Remaining(j); len(got) != 0 {
		t.Fatalf("cancel must stop remaining segments: %+v", got)
	}
}

func TestTTSExportWritesFileNotStudioClaim(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "voice.wav")
	if err := ExportTTSFile(dest, []byte("RIFF-fake")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || string(raw) != "RIFF-fake" {
		t.Fatalf("export %q %v", raw, err)
	}
}
