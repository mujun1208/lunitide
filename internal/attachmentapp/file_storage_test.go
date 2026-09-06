package attachmentapp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDirFilesAtomicReplacementNeverExposesPartialContent(t *testing.T) {
	files := NewDirFileStorage(t.TempDir())
	ctx := context.Background()
	old, next := bytes.Repeat([]byte("a"), 65536), bytes.Repeat([]byte("b"), 98304)
	if err := files.WriteFile(ctx, "report", old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			data, err := files.ReadFile(ctx, "report")
			if err != nil {
				failures <- err
				return
			}
			if !bytes.Equal(data, old) && !bytes.Equal(data, next) {
				failures <- errors.New("partial replacement visible")
				return
			}
		}
	}()
	for i := 0; i < 30; i++ {
		data := old
		if i%2 == 0 {
			data = next
		}
		if err := files.WriteFile(ctx, "report", data); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
}
func TestDirFilesEnforceSizeContextAndPathBoundaries(t *testing.T) {
	root := t.TempDir()
	files := NewDirFileStorage(root)
	ctx := context.Background()
	for _, name := range []string{"../escape", "C:stream", "CON", "trailing.", "folder/file"} {
		if err := files.WriteFile(ctx, name, []byte("x")); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if err := files.WriteFile(ctx, "large", make([]byte, MaxFileSize+1)); err == nil {
		t.Fatal("oversized write accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "large"), make([]byte, MaxFileSize+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := files.ReadFile(ctx, "large"); err == nil {
		t.Fatal("oversized read accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := files.WriteFile(cancelled, "cancelled", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("write err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cancelled")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled write created file: %v", err)
	}
}
