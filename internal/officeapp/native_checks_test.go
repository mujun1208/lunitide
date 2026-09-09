package officeapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/commandworker"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestNativePreviewChecksKeepIncompleteScansAndErrorsVisible(t *testing.T) {
	base := officerender.NativeEvidence{Scope: "derived-preview", SourceUnchanged: true, Recalculated: true, FormulaCells: 50000}
	if checks := nativePreviewChecks(base); len(checks) != 1 || checks[0].Status != "unsupported" {
		t.Fatalf("incomplete scan passed: %#v", checks)
	}
	base.FormulaScanFull, base.FormulaErrorCount = true, 1
	base.FormulaErrors = []officerender.FormulaError{{SheetIndex: 1, Row: 3, Column: 4, Code: 532}}
	checks := nativePreviewChecks(base)
	if len(checks) != 1 || checks[0].Status != "failed" || !strings.Contains(checks[0].Detail, "第2张表 R3C4") {
		t.Fatalf("calculation error hidden: %#v", checks)
	}
	base.Scope = "source"
	if checks := nativePreviewChecks(base); len(checks) != 1 || checks[0].Status != "failed" {
		t.Fatalf("invalid evidence scope accepted: %#v", checks)
	}
}

func TestServiceRenderFailureIsFailedAndCancelledCheckIsNotSaved(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v := generatedWord(t, svc, task, "render-failure")
	stop := false
	svc.Renderer.Run = func(_ context.Context, work commandworker.Spec, _ commandworker.StartGuard, chunk func([]byte)) (commandworker.Outcome, error) {
		if len(work.Args) == 1 && work.Args[0] == "--version" {
			if chunk != nil {
				chunk([]byte("LibreOffice failing-fixture"))
			}
			return commandworker.Outcome{}, nil
		}
		if stop {
			cancel()
			return commandworker.Outcome{}, ctx.Err()
		}
		return commandworker.Outcome{TimedOut: true}, nil
	}
	qa, err := svc.Check(ctx, task.ID, v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	actual := domain.Check{}
	for _, check := range qa.Checks {
		if check.ID == "actual-render" {
			actual = check
		}
	}
	if actual.Status != "failed" || qa.Quality != "blocked" {
		t.Fatalf("real worker failure hidden as missing component: %#v", qa)
	}
	before, err := store.ListOfficeValidations(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	stop = true
	if _, err := svc.Check(ctx, task.ID, v.ID, true); err != context.Canceled {
		t.Fatalf("cancel result: %v", err)
	}
	checks, err := store.ListOfficeValidations(context.Background(), v.ID)
	if err != nil || len(checks) != len(before) {
		t.Fatal("cancelled check was saved as completed", len(checks), err)
	}
}

// The model and worker are controlled fixtures; the file store, immutable
// version binding, SQLite validation record and service dispatch are real.
func TestServiceNativeWordPreviewPreservesSourceChecksAndReceipt(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	spec := shortWordSpec()
	spec.Blocks = append([]content.Block{{Type: "toc"}}, spec.Blocks...)
	v, err := svc.Generate(ctx, task.ID, "目录报告.docx", spec, "native-source")
	if err != nil {
		t.Fatal(err)
	}
	_, source, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4\ncontrolled worker fixture\n%%EOF\n")
	calls := 0
	svc.Renderer.Run = func(_ context.Context, work commandworker.Spec, _ commandworker.StartGuard, chunk func([]byte)) (commandworker.Outcome, error) {
		calls++
		if len(work.Args) == 1 && work.Args[0] == "--version" {
			if chunk != nil {
				chunk([]byte("LibreOffice native-fixture"))
			}
			return commandworker.Outcome{}, nil
		}
		if !strings.HasPrefix(filepath.Clean(work.Dir), filepath.Clean(svc.Renderer.Root)+string(filepath.Separator)) {
			t.Fatal("worker escaped its private directory")
		}
		if work.Args[len(work.Args)-1] == "--terminate_after_init" {
			user := filepath.Join(work.Dir, "profile", "user")
			if err := os.MkdirAll(user, 0700); err != nil {
				return commandworker.Outcome{}, err
			}
			err := os.WriteFile(filepath.Join(user, "registrymodifications.xcu"), []byte(`<oor:items xmlns:oor="http://openoffice.org/2001/registry"></oor:items>`), 0600)
			return commandworker.Outcome{}, err
		}
		if work.Args[len(work.Args)-1] != "macro:///Standard.Module1.Main" {
			t.Fatal("native update not requested", work.Args)
		}
		receipt := fmt.Sprintf("office-native-v1\nsourceSha256=%s\nkind=docx\nfieldsRefreshed=1\nupdatedIndexes=1\nrecalculated=0\nformulaCells=0\nformulaErrorCount=0\nformulaScanFull=0\ndone=1\n", v.SHA256)
		if err := os.WriteFile(filepath.Join(work.Dir, "receipt.txt"), []byte(receipt), 0600); err != nil {
			return commandworker.Outcome{}, err
		}
		return commandworker.Outcome{}, os.WriteFile(filepath.Join(work.Dir, "source.pdf"), pdf, 0600)
	}
	qa, err := svc.Check(ctx, task.ID, v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]domain.Check{}
	for _, check := range qa.Checks {
		checks[check.ID] = check
	}
	if calls != 3 || checks["preview_fields"].Status != "passed" || checks["fields_update"].Status != "unsupported" || qa.Quality != "partial" {
		t.Fatalf("derived PDF overclaimed source cache status: %#v calls=%d", qa, calls)
	}
	var evidence struct {
		Native officerender.NativeEvidence `json:"native"`
	}
	if err := json.Unmarshal(qa.Evidence, &evidence); err != nil || evidence.Native.UpdatedIndexes != 1 || !evidence.Native.SourceUnchanged {
		t.Fatal("actual native receipt not retained", err, string(qa.Evidence))
	}
	_, after, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil || !bytes.Equal(source, after) {
		t.Fatal("preview rewrote original file", err)
	}
	actual, err := svc.ReadPDF(ctx, task.ID, v.ID)
	if err != nil || !bytes.Equal(pdf, actual) {
		t.Fatal("preview PDF not available by source version", err)
	}
}
