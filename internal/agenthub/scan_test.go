package agenthub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanWorkDirCollectsInsideAndOutside(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, promptFileName), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "away.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, []string{"hello.txt", outside}, time.Time{})
	var sources []string
	for _, art := range arts {
		if strings.HasSuffix(art.Name, promptFileName) {
			t.Fatal("prompt file should be ignored")
		}
		sources = append(sources, art.Name+":"+art.Source)
	}
	joined := strings.Join(sources, ",")
	if !strings.Contains(joined, "hello.txt:event") || !strings.Contains(joined, "away.txt:outside") {
		t.Fatal(joined)
	}
}

func TestScanSkipsNodeModules(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Time{})
	for _, art := range arts {
		if strings.Contains(filepath.ToSlash(art.Path), "node_modules") {
			t.Fatalf("leaked %s", art.Path)
		}
	}
}

func TestScanMarksRecentAsChanged(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.txt")
	if err := os.WriteFile(old, []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, started)
	got := map[string]string{}
	for _, art := range arts {
		got[art.Name] = art.Source
	}
	if got["hello.txt"] != "changed" {
		t.Fatalf("%v", got)
	}
	if got["old.txt"] != "scan" {
		t.Fatalf("old should stay scan: %v", got)
	}
}

func TestScanMarksInboxSource(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, inboxDirName)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	target := filepath.Join(inbox, "note.pdf")
	if err := os.WriteFile(target, []byte("p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, past, past); err != nil {
		t.Fatal(err)
	}
	arts := ScanWorkDir(root, nil, time.Now())
	for _, art := range arts {
		if art.Name == "note.pdf" && art.Source == "inbox" {
			return
		}
	}
	t.Fatalf("%+v", arts)
}

func TestPersistableSourceMapsInboxAndChanged(t *testing.T) {
	if persistableSource("inbox") != "scan" || persistableSource("changed") != "scan" {
		t.Fatal("inbox/changed must persist as scan")
	}
	if persistableSource("event") != "event" || persistableSource("outside") != "outside" || persistableSource("scan") != "scan" {
		t.Fatal("legacy sources stay")
	}
}

func TestRematerializeInboxFromPersistedScan(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, inboxDirName, "note.pdf")
	if rematerializeSource(root, path, "scan", time.Time{}) != "inbox" {
		t.Fatal("inbox path must come back")
	}
	if rematerializeSource(root, filepath.Join(root, "old.txt"), "event", time.Time{}) != "event" {
		t.Fatal("event stays")
	}
}

func TestPeekWorkDirListsInbox(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, inboxDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, inboxDirName, "note.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arts := peekWorkDir(root)
	for _, art := range arts {
		if art.Name == "note.txt" && art.Source == "inbox" {
			return
		}
	}
	t.Fatalf("%+v", arts)
}
