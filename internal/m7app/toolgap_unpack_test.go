package m7app

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnpackZipRejectsDeclaredBombRatio(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "bomb.zip")
	dst := filepath.Join(root, "out")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, lyingZip("huge.bin", []byte("hi"), 101<<20, 2), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := unpackZip(src, dst, root, 16, 8<<20)
	if err == nil || !strings.Contains(err.Error(), "ratio") {
		t.Fatalf("zip bomb must be rejected by ratio: %v", err)
	}
	if entries, _ := os.ReadDir(dst); len(entries) != 0 {
		t.Fatalf("bomb must not extract: %v", entries)
	}
}

func TestUnpackZipWritesSafeEntry(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "ok.zip")
	dst := filepath.Join(root, "out")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("note.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := unpackZip(src, dst, root, 16, 8<<20)
	if err != nil || got.EntryCount != 1 {
		t.Fatalf("safe zip: %+v %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(dst, "note.txt"))
	if err != nil || string(raw) != "hello" {
		t.Fatalf("extracted %q %v", raw, err)
	}
}

func lyingZip(name string, data []byte, uncompressed, compressed uint32) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	w, err := zw.CreateHeader(h)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write(data); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	raw := buf.Bytes()
	patchZipUncompressed(raw, uncompressed)
	_ = compressed
	return raw
}

func patchZipUncompressed(raw []byte, uncompressed uint32) {
	for i := 0; i+30 <= len(raw); i++ {
		if raw[i] == 'P' && raw[i+1] == 'K' && raw[i+2] == 3 && raw[i+3] == 4 {
			binary.LittleEndian.PutUint32(raw[i+22:], uncompressed)
		}
		if raw[i] == 'P' && raw[i+1] == 'K' && raw[i+2] == 1 && raw[i+3] == 2 && i+28 <= len(raw) {
			binary.LittleEndian.PutUint32(raw[i+24:], uncompressed)
		}
	}
}
