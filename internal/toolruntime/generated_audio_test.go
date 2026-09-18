package toolruntime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/tts"
)

func TestGeneratedAudioDeliveryAndConfinement(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data := tts.PCM16MonoWAV(16000, []byte{1, 0, 2, 0, 3, 0, 4, 0})
	out, err := r.SaveGeneratedAudio(officeSession, "generated/clip.wav", data)
	if err != nil || out.Artifact == nil || out.Artifact.Kind != "audio" || out.Artifact.Path != "generated/clip.wav" {
		t.Fatalf("%+v %v", out, err)
	}
	root, err := r.SessionFolder(officeSession)
	if err != nil {
		t.Fatal(err)
	}
	read, err := os.ReadFile(filepath.Join(root, out.Artifact.Path))
	if err != nil || len(read) != len(data) {
		t.Fatal("generated audio bytes were lost")
	}
	for _, path := range []string{"../escape.wav", "C:/escape.wav", "clip.txt"} {
		if _, err = r.SaveGeneratedAudio(officeSession, path, data); err == nil {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
	if _, err = r.SaveGeneratedAudio(officeSession, "bad.wav", []byte("not audio")); err == nil {
		t.Fatal("invalid wav accepted")
	}
}
