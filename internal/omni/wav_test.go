package omni

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWritePCM16MonoWAV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk.wav")
	pcm := make([]byte, ChunkBytes)
	if err := WritePCM16MonoWAV(path, SampleRate, pcm); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(44+ChunkBytes) {
		t.Fatalf("wav size = %d; want %d", info.Size(), 44+ChunkBytes)
	}
}

func TestWritePCM16MonoWAVRejectsOddLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.wav")
	if err := WritePCM16MonoWAV(path, SampleRate, []byte{1}); err == nil {
		t.Fatal("expected odd-length pcm to fail")
	}
}
