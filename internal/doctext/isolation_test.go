package doctext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var workerTestJob = flag.String("doctext-worker", "", "child parser test entry point")

func TestMain(m *testing.M) {
	flag.Parse()
	if *workerTestJob != "" {
		if err := RunWorker(*workerTestJob); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func configureTestWorker(t *testing.T) string {
	t.Helper()
	old := parserConfig.Load()
	t.Cleanup(func() { parserConfig.Store(old) })
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureWorker(exe, root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestIsolatedParserRoundTripFailureAndCleanup(t *testing.T) {
	root := configureTestWorker(t)
	// The child runs the actual parser with a stripped environment and staged input.
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"manual.md", "# 测试手册\n\n完整正文", nil},
		{"empty.txt", "  ", ErrNoTextLayer},
		{"unknown.bin", "\x00\x01", ErrUnsupportedFormat},
		{"limit.txt", strings.Repeat("界", MaxExtractRunes+1), ErrBudgetExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExtractContext(context.Background(), tc.name, []byte(tc.body), "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			if tc.want == nil && result.Text != tc.body {
				t.Fatalf("incomplete text: %q", result.Text)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("staged data not cleaned up: %v %v", entries, err)
			}
		})
	}
}

func TestIsolatedParserDeadlineReleasesSlotAndFiles(t *testing.T) {
	root := configureTestWorker(t)
	// The deadline expires during child launch/parse; the parent must await exit
	// and clean the snapshot before admitting another document.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := ExtractContext(ctx, "large.txt", []byte(strings.Repeat("x", MaxExtractRunes)), "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline=%v", err)
	}
	if len(parserSlots) != 0 {
		t.Fatal("parser slot leaked")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staged data retained: %v %v", entries, err)
	}
	if _, err := ExtractContext(context.Background(), "next.txt", []byte("next document"), ""); err != nil {
		t.Fatal(err)
	}
}

func TestParserArchiveAndSourceBudgets(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	part, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(part, strings.NewReader(strings.Repeat("x", (16<<20)+1)), (16<<20)+1); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract("bomb.docx", buf.Bytes(), ""); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("zip expansion accepted: %v", err)
	}
	path := filepath.Join(t.TempDir(), "large.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxInputBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := ReadSource(path); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("large source read: %v", err)
	}
	if _, err := ReadSource(filepath.Dir(path)); err == nil {
		t.Fatalf("directory read: %v", err)
	}
}

func TestWorkerJobSizeIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", 8193)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(path); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("job budget=%v", err)
	}
}

func TestParserReclaimsOnlyOldOwnedSnapshots(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"parse-expired", "parse-recent", "unrelated"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "input.bin"), []byte("private snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
		if name != "parse-recent" {
			old := time.Now().Add(-48 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := removeExpiredParserJobs(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "parse-expired")); !os.IsNotExist(err) {
		t.Fatalf("expired snapshot retained: %v", err)
	}
	for _, name := range []string{"parse-recent", "unrelated"} {
		if _, err := os.Stat(filepath.Join(root, name, "input.bin")); err != nil {
			t.Fatal(err)
		}
	}
}
