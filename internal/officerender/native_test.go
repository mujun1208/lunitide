package officerender

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/commandworker"
)

func nativeTestReceipt(kind string, data []byte) string {
	fields, recalc, cells, full := 0, 0, 0, 0
	if kind == "docx" {
		fields = 1
	} else {
		recalc, cells, full = 1, 2, 1
	}
	return fmt.Sprintf("office-native-v1\nsourceSha256=%x\nkind=%s\nfieldsRefreshed=%d\nupdatedIndexes=0\nrecalculated=%d\nformulaCells=%d\nformulaErrorCount=0\nformulaScanFull=%d\ndone=1\n", sha256.Sum256(data), kind, fields, recalc, cells, full)
}

func TestNativeWorkerOwnedLibraryEvidenceAndCleanup(t *testing.T) {
	for _, kind := range []string{"docx", "xlsx"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte("synthetic input, not executable code")
			r := &Renderer{Root: t.TempDir(), Executable: "fixed-test-worker", Preflight: func(string, []byte) error { return nil }}
			calls, job := 0, ""
			r.Run = func(ctx context.Context, s commandworker.Spec, _ commandworker.StartGuard, stdout func([]byte)) (commandworker.Outcome, error) {
				calls++
				if s.Args[0] == "--version" {
					stdout([]byte("LibreOffice fixture"))
					return commandworker.Outcome{}, nil
				}
				job = s.Dir
				if s.Timeout <= 0 || s.MaxMemoryBytes == 0 || s.MaxOutputBytes == 0 || !strings.HasPrefix(s.Args[0], "-env:UserInstallation=file:///") {
					t.Fatal("worker isolation missing")
				}
				if !strings.Contains(strings.Join(s.Env, "\n"), "TEMP="+job) {
					t.Fatal("controlled temporary environment missing")
				}
				registry := filepath.Join(job, "profile", "user", "registrymodifications.xcu")
				if s.Args[len(s.Args)-1] == "--terminate_after_init" {
					if err := os.MkdirAll(filepath.Dir(registry), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(registry, []byte(`<oor:items xmlns:oor="http://openoffice.org/2001/registry"><item>first-run-completed</item></oor:items>`), 0600); err != nil {
						t.Fatal(err)
					}
					return commandworker.Outcome{}, nil
				}
				settings, _ := os.ReadFile(registry)
				if !strings.Contains(string(settings), "first-run-completed") || !strings.Contains(string(settings), "<value>3</value>") {
					t.Fatal("startup state/security lost")
				}
				module, err := os.ReadFile(filepath.Join(job, "profile", "user", "basic", "Standard", "Module1.xba"))
				if err != nil {
					t.Fatal(err)
				}
				text := string(module)
				if !strings.Contains(text, "MacroExecutionMode") || !strings.Contains(text, "p(1).Value=0") || !strings.Contains(text, "p(2).Value=0") || strings.Contains(text, string(data)) {
					t.Fatal("document macros/updates enabled or document content in executable library")
				}
				for name, b := range map[string][]byte{"receipt.txt": []byte(nativeTestReceipt(kind, data)), "source.pdf": []byte("%PDF-1.7\nfixture\n%%EOF")} {
					if err := os.WriteFile(filepath.Join(job, name), b, 0600); err != nil {
						t.Fatal(err)
					}
				}
				return commandworker.Outcome{}, nil
			}
			result, err := r.RenderWithChecks(context.Background(), kind, data, NativeOptions{UpdateFields: kind == "docx", Recalculate: kind == "xlsx"})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 3 || result.Native == nil || result.Native.Scope != "derived-preview" || !result.Native.SourceUnchanged {
				t.Fatalf("missing evidence: %+v", result)
			}
			if _, err = os.Stat(job); !os.IsNotExist(err) {
				t.Fatal("owned worker directory remains")
			}
		})
	}
}

func TestNativeWorkerDoesNotTrustExitZeroWithoutReceipts(t *testing.T) {
	for _, failure := range []string{"missing-receipt", "wrong-digest", "missing-pdf", "truncated-pdf", "changed-input", "script-error", "timeout", "oversize-receipt"} {
		t.Run(failure, func(t *testing.T) {
			data := []byte("synthetic")
			r := &Renderer{Root: t.TempDir(), Executable: "fixture", Preflight: func(string, []byte) error { return nil }}
			r.Run = func(_ context.Context, s commandworker.Spec, _ commandworker.StartGuard, out func([]byte)) (commandworker.Outcome, error) {
				if s.Args[0] == "--version" {
					out([]byte("LibreOffice fixture"))
					return commandworker.Outcome{}, nil
				}
				write := func(name string, b []byte) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(s.Dir, name), b, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if s.Args[len(s.Args)-1] == "--terminate_after_init" {
					if err := os.MkdirAll(filepath.Join(s.Dir, "profile", "user"), 0700); err != nil {
						t.Fatal(err)
					}
					write("profile/user/registrymodifications.xcu", []byte(`<oor:items></oor:items>`))
					return commandworker.Outcome{}, nil
				}
				receipt := nativeTestReceipt("xlsx", data)
				if failure == "wrong-digest" {
					receipt = nativeTestReceipt("xlsx", []byte("different"))
				}
				if failure == "oversize-receipt" {
					receipt = strings.Repeat("x", 16385)
				}
				if failure != "missing-receipt" {
					write("receipt.txt", []byte(receipt))
				}
				if failure != "missing-pdf" {
					pdf := []byte("%PDF-1.7\n%%EOF")
					if failure == "truncated-pdf" {
						pdf = []byte("%PDF-1.7\ntruncated")
					}
					write("source.pdf", pdf)
				}
				if failure == "changed-input" {
					write("source.xlsx", []byte("changed"))
				}
				if failure == "script-error" {
					write("error.txt", []byte("failure"))
				}
				return commandworker.Outcome{TimedOut: failure == "timeout"}, nil
			}
			if _, err := r.RenderWithChecks(context.Background(), "xlsx", data, NativeOptions{Recalculate: true}); err == nil {
				t.Fatal("failure marked passed")
			}
		})
	}
}

func TestNativeReceiptReportsFormulaErrorsAndScanLimits(t *testing.T) {
	data := []byte("synthetic")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	good := nativeTestReceipt("xlsx", data)
	withError := strings.Replace(good, "formulaErrorCount=0", "formulaErrorCount=1", 1)
	withError = strings.Replace(withError, "done=1", "error=1,2,3,532\ndone=1", 1)
	e, err := parseNativeReceipt([]byte(withError), "xlsx", digest)
	if err != nil || e.FormulaErrorCount != 1 || len(e.FormulaErrors) != 1 || e.FormulaErrors[0].Code != 532 {
		t.Fatalf("error erased: %+v %v", e, err)
	}
	limited := strings.ReplaceAll(strings.Replace(good, "formulaCells=2", "formulaCells=50000", 1), "formulaScanFull=1", "formulaScanFull=0")
	e, err = parseNativeReceipt([]byte(limited), "xlsx", digest)
	if err != nil || e.FormulaScanFull || e.FormulaCells != 50000 {
		t.Fatalf("scan cap erased: %+v %v", e, err)
	}
	for _, bad := range []string{good + "done=1\n", strings.Replace(good, "done=1", "done=0", 1), strings.Replace(good, "formulaCells=2", "formulaCells=50001", 1), strings.Replace(good, "formulaScanFull=1", "formulaScanFull=0", 1), strings.Replace(withError, "error=1,2,3,532\n", "", 1), strings.Replace(withError, "error=1,2,3,532", "error=1,0,3,532", 1), good + "unknown=1\n"} {
		if _, err := parseNativeReceipt([]byte(bad), "xlsx", digest); err == nil {
			t.Fatal("invalid receipt accepted", bad)
		}
	}
}

func TestNativeOptionsAndPreflightCannotStartUnsafeWorkers(t *testing.T) {
	r := &Renderer{Root: t.TempDir(), Executable: "fixture", Run: func(context.Context, commandworker.Spec, commandworker.StartGuard, func([]byte)) (commandworker.Outcome, error) {
		t.Fatal("worker started")
		return commandworker.Outcome{}, nil
	}}
	for _, kind := range []string{"pptx", "docx", "xlsx"} {
		if _, err := r.RenderWithChecks(context.Background(), kind, []byte("unsafe"), NativeOptions{UpdateFields: true}); err == nil {
			t.Fatal("invalid options/preflight accepted")
		}
	}
}
