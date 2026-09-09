package officerender

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/commandworker"
)

func TestOfficeRendererOwnedProfileAndBoundedOutput(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "soffice.com")
	var job string
	calls := 0
	r := &Renderer{Root: root, Executable: exe, Preflight: func(string, []byte) error { return nil }}
	r.Run = func(_ context.Context, s commandworker.Spec, _ commandworker.StartGuard, out func([]byte)) (commandworker.Outcome, error) {
		calls++
		if s.Exe != exe {
			t.Fatal("executable drift")
		}
		if len(s.Args) == 1 && s.Args[0] == "--version" {
			out([]byte("LibreOffice test adapter"))
			return commandworker.Outcome{}, nil
		}
		job = s.Dir
		rel, err := filepath.Rel(root, job)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatal("worker escaped root")
		}
		if !strings.HasPrefix(s.Args[0], "-env:UserInstallation=file:///") || s.MaxMemoryBytes == 0 || s.Timeout == 0 {
			t.Fatalf("unbounded/shared worker: %+v", s)
		}
		for _, e := range s.Env {
			if strings.Contains(e, "OFFICE_TEST_PRIVATE_KEY") {
				t.Fatal("private credentials leaked")
			}
		}
		if err = os.WriteFile(filepath.Join(job, "output", "source.pdf"), []byte("%PDF-1.7\nfixture\n%%EOF"), 0600); err != nil {
			t.Fatal(err)
		}
		return commandworker.Outcome{}, nil
	}
	t.Setenv("OFFICE_TEST_PRIVATE_KEY", "do-not-forward")
	result, err := r.Render(context.Background(), "docx", []byte("preflighted synthetic document"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PDF) == 0 || len(result.SourceDigest) != 64 || calls != 2 {
		t.Fatalf("missing evidence: %+v calls%d", result, calls)
	}
	if _, err = os.Stat(job); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned staging folder survived")
	}
}

func TestOfficeRendererRejectsUnsafeBeforeStartingAnyProcess(t *testing.T) {
	r := &Renderer{Root: t.TempDir(), Executable: filepath.Join(t.TempDir(), "soffice.com"), Preflight: func(string, []byte) error { return errors.New("active content") }, Run: func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error) {
		t.Fatal("must not start")
		return commandworker.Outcome{}, nil
	}}
	if _, err := r.Render(context.Background(), "pptx", []byte("unsafe")); err == nil {
		t.Fatal("unsafe file accepted")
	}
}

func TestOfficeRendererMissingOutputDoesNotMeanSuccess(t *testing.T) {
	r := &Renderer{Root: t.TempDir(), Executable: filepath.Join(t.TempDir(), "soffice.com"), Preflight: func(string, []byte) error { return nil }, Run: func(_ context.Context, s commandworker.Spec, _ commandworker.StartGuard, f func([]byte)) (commandworker.Outcome, error) {
		if s.Args[0] == "--version" {
			f([]byte("LibreOffice test"))
		}
		return commandworker.Outcome{}, nil
	}}
	if _, err := r.Render(context.Background(), "xlsx", []byte("fixture")); err == nil || !strings.Contains(err.Error(), "未产生 PDF") {
		t.Fatalf("fake success: %v", err)
	}
}
