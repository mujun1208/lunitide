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

func TestEvaluateOfficeQualityWholeSlideRasterBlocksFormal(t *testing.T) {
	report := evaluateOfficeQuality([]domain.Check{
		{ID: "native_render", Status: "passed", Detail: "ok"},
		{ID: "rasterized_object", Status: "failed", Detail: "reason=whole-slide count=1；栅格对象不计入可编辑覆盖率"},
	}, nil)
	if report.FormalOK {
		t.Fatal("whole-slide raster must block Formal")
	}
	if strings.Contains(strings.ToLower(report.Coverage), "fully editable") {
		t.Fatal("must not claim fully editable after rasterization")
	}
}

func TestTargetAppCoverageChecksStayUnsupported(t *testing.T) {
	for _, c := range targetAppCoverageChecks() {
		if c.Status == "passed" || c.Required {
			t.Fatalf("coverage check must stay honest: %#v", c)
		}
	}
}

func TestCheckQualityReportBlocksFormalWhenRendererUnavailable(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "qa-missing-render")
	qa, err := svc.Check(context.Background(), task.ID, v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if qa.Quality == "passed" {
		t.Fatal("missing renderer must not mark validation passed")
	}
	var evidence struct {
		FormalOK      bool                  `json:"formalOk"`
		QualityReport content.QualityReport `json:"qualityReport"`
	}
	if err := json.Unmarshal(qa.Evidence, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.FormalOK || evidence.QualityReport.FormalOK {
		t.Fatalf("formal delivery allowed without renderer: %#v", evidence)
	}
	if len(evidence.QualityReport.Blockers) == 0 {
		t.Fatal("missing renderer must be a quality blocker")
	}
}

func TestCheckMarksLegacyQualityWhenDesignSystemOff(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	svc, _, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "design-off-scope")
	qa, err := svc.Check(context.Background(), task.ID, v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var design string
	for _, c := range qa.Checks {
		if c.ID == "design_system" {
			design = c.Detail
		}
	}
	if !strings.Contains(design, "旧质量范围") || strings.Contains(design, "已启用") {
		t.Fatalf("design off must mark legacy quality: %q quality=%s", design, qa.Quality)
	}
	if qa.Quality == "passed" {
		t.Fatal("design off must not pretend the new quality bar passed")
	}
}

func TestCheckBindsDiagnoseRenderWhenRendererUnavailable(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "diagnose-render")
	qa, err := svc.Check(context.Background(), task.ID, v.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var native string
	for _, c := range qa.Checks {
		if c.ID == "native_render" {
			native = c.Detail
		}
	}
	if !strings.Contains(native, "不能标为已验证") {
		t.Fatalf("native_render must use DiagnoseRender: %q", native)
	}
	if qa.Quality == "passed" {
		t.Fatal("missing renderer must not mark validation passed")
	}
}

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

func TestPreviewRejectsStaleSameSourcePDF(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v, err := svc.Generate(ctx, task.ID, "源.docx", shortWordSpec(), "stale-pdf")
	if err != nil {
		t.Fatal(err)
	}
	bind := content.InvalidateSameSourcePDF(content.BindSameSourcePDF(content.DOCX, "deadbeef", []byte("%PDF-1.4 stale")), v.SHA256)
	if !bind.Stale {
		t.Fatal("fixture bind must be stale")
	}
	_, err = store.AddOfficeValidation(ctx, domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "test", Quality: "partial",
		Evidence: encode(map[string]any{"pdfRef": strings.Repeat("a", 64), "sameSourcePdf": bind}),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, task.ID, v.ID)
	if err != nil || preview.PDFReady {
		t.Fatalf("stale same-source PDF marked ready: %#v %v", preview, err)
	}
	if _, err = svc.ReadPDF(ctx, task.ID, v.ID); err == nil {
		t.Fatal("stale same-source PDF still readable")
	}
}
