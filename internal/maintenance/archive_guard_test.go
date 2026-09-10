package maintenance

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProbeManifest(dir string) error {
	body := `{"format":"lunitide-directory-backup-v1","files":[{"path":"lunitide.db","size":1,"sha256":"00"}]}`
	return os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(body), 0600)
}

func TestRejectUnsafeArchivePathAndZipBomb(t *testing.T) {
	if err := RejectUnsafeArchivePath("../escape"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if err := RejectUnsafeArchivePath("ok/file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := RejectZipBomb(200<<20, 100); err == nil {
		t.Fatal("compression bomb must be rejected")
	}
	if err := RejectZipBomb(1024, 512); err != nil {
		t.Fatal(err)
	}
}

func TestProbeBackupIsReadOnlyAndRejectsTraversal(t *testing.T) {
	if _, err := Probe("..\\escape"); err == nil {
		t.Fatal("probe must reject traversal")
	}
	dir := t.TempDir()
	if err := writeProbeManifest(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Probe(dir)
	if err != nil || !got.OK || got.Format != "lunitide-directory-backup-v1" || got.Files != 1 {
		t.Fatalf("probe %+v %v", got, err)
	}
	if err := RejectUnsafeArchivePath("../escape"); err == nil {
		t.Fatal("manifest entries must reject traversal")
	}
	if err := CheckManifestPaths([]string{"lunitide.db", "../escape"}); err == nil {
		t.Fatal("backup manifest must reject traversal entries")
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"format":"lunitide-directory-backup-v1","files":[{"path":"../escape"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(dir); err == nil {
		t.Fatal("probe must reject traversal entries inside the manifest")
	}
}
