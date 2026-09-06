package brapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserProfileCleanupRejectsLinksAndPreservesExternalData(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	cache := filepath.Join(root, "edge-profile", "Cache")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(outside, "protected.txt")
	if err := os.WriteFile(protected, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := makeBrowserTestLink(outside, filepath.Join(cache, "linked")); err != nil {
		t.Skipf("local symlink creation unavailable: %v", err)
	}
	host := NewLocalHost(root)
	if _, err := host.ClearData(context.Background(), ModeEdge, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("linked profile was reported as successfully cleaned")
	}
	raw, err := os.ReadFile(protected)
	if err != nil || string(raw) != "keep" {
		t.Fatalf("external file changed: %q %v", raw, err)
	}
}

func TestBrowserProfileBudgetCancellationAndSessionUsage(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "edge-profile", "0123456789abcdef0123456789abcdef", "Cache")
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cache, "cached")
	if err := os.WriteFile(file, []byte("cache"), 0600); err != nil {
		t.Fatal(err)
	}
	host := NewLocalHost(root)
	profile, cached, _, err := host.SnapshotUsageChecked(context.Background(), ModeEdge)
	if err != nil || profile != 5 || cached != 5 {
		t.Fatalf("private session usage missing: %d %d %v", profile, cached, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := host.ClearData(ctx, ModeEdge, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("cancelled cleanup reported success")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("cancelled cleanup removed file")
	}
	freed, err := host.ClearData(context.Background(), ModeEdge, time.Now().Add(time.Hour))
	if err != nil || freed != 5 {
		t.Fatalf("cleanup failed: %d %v", freed, err)
	}
}
