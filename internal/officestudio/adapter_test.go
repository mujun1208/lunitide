package officestudio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportBrandL1RequiresAssetRecordAndKeepsFacts(t *testing.T) {
	in := BrandImport{
		BrandID: "client-a",
		Colors:  map[string]string{"navy": "111111", "teal": "22AA88"},
		Fonts:   BrandFonts{Latin: "Calibri", East: "Microsoft YaHei"},
	}
	if _, err := ImportBrandL1(in, AssetRecord{}); err == nil {
		t.Fatal("brand without asset record must fail")
	}
	brand, err := ImportBrandL1(in, AssetRecord{
		SourceURL: "https://example.invalid/brand", Author: "客户", License: "client-granted",
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Commercial: true,
	})
	if err != nil || brand.BrandID != "client-a" || brand.Colors["navy"] != "111111" {
		t.Fatalf("brand: %#v %v", brand, err)
	}
	spec := Spec{SchemaVersion: 2, Kind: PPTX, Title: "品牌", BrandID: brand.BrandID, Slides: []Slide{{
		Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}},
	}}}
	adapted, err := AdaptSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if adapted.Slides[0].Metrics[0].Value != "1280" {
		t.Fatal("brand import rewrote facts")
	}
}

func TestPresentonAndPptxGenJSMissingCannotFormalDeliver(t *testing.T) {
	t.Setenv("LUNITIDE_PRESENTON", "")
	t.Setenv("LUNITIDE_PPTXGENJS", "")
	for _, st := range []ExternalAdapterStatus{ProbePresenton(), ProbePptxGenJS()} {
		if st.Available {
			t.Fatalf("empty env looked available: %#v", st)
		}
		report := QualityFromExternalAdapter(st)
		if report.FormalOK || len(report.Blockers) == 0 {
			t.Fatalf("missing adapter formal: %#v", report)
		}
	}
}

func TestExternalAdapterMissingCannotFormalDeliver(t *testing.T) {
	st := ProbeExternalAdapter("presenton", "")
	if st.Available {
		t.Fatal("empty executable must be unavailable")
	}
	report := QualityFromExternalAdapter(st)
	if report.FormalOK || len(report.Blockers) == 0 {
		t.Fatalf("missing adapter formal: %#v", report)
	}
	for _, b := range report.Blockers {
		if b.Severity == "passed" || strings.Contains(b.Message, "已验证") {
			t.Fatalf("fake green: %#v", b)
		}
	}
}

func TestTypstIndependentPDFNoticeDiffersFromWord(t *testing.T) {
	notice := IndependentPDFNotice()
	if !strings.Contains(notice, "Word") || !strings.Contains(notice, "分页") {
		t.Fatalf("notice=%q", notice)
	}
	if ProbeTypst("").Available {
		t.Fatal("missing typst must not look available")
	}
}

func TestIndependentPDFMissingWithoutTypstBlocksFormal(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	data, check, err := RenderIndependentPDFWithTheme(t.TempDir(), "标题", "订单 1280单", Theme{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("gofpdf fallback missing file: %#v", check)
	}
	if check.ID != "independent_pdf" || check.Status != "passed" {
		t.Fatalf("gofpdf fallback check: %#v", check)
	}
	if !strings.Contains(check.Message, "稳定独立 PDF") || strings.Contains(check.Message, "Typst 已验证") {
		t.Fatalf("gofpdf message must name this file's backend: %#v", check)
	}
	report := EvaluateQuality([]Check{check}, 88, nil)
	if !report.FormalOK || len(report.Blockers) != 0 {
		t.Fatalf("gofpdf fallback must Formal: %#v", report)
	}
	if IndependentPDFCheck().Status == "passed" {
		t.Fatal("env probe without file bytes must not pass independent_pdf")
	}
}

func TestIndependentPDFTypstConfiguredButCompileFailsIsGofpdfNotTypstPassed(t *testing.T) {
	dummy := filepath.Join(t.TempDir(), "typst.bat")
	if err := os.WriteFile(dummy, []byte("@echo off\r\nexit /b 1\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUNITIDE_TYPST", dummy)
	data, check, err := RenderIndependentPDFWithTheme(t.TempDir(), "标题", "订单 1280单", Theme{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") || check.Status != "passed" {
		t.Fatalf("fallback after typst fail: %#v", check)
	}
	if strings.Contains(check.Message, "Typst 已验证") || strings.Contains(check.Message, "已检测到工作进程") {
		t.Fatalf("compile failure must not look Typst-passed: %#v", check)
	}
	if !strings.Contains(check.Message, "稳定独立 PDF") {
		t.Fatalf("fallback must name gofpdf: %#v", check)
	}
	if IndependentPDFCheck(data).Status != "passed" || strings.Contains(IndependentPDFCheck(data).Message, "Typst 已验证") {
		t.Fatalf("bytes check used env: %#v", IndependentPDFCheck(data))
	}
	report := EvaluateQuality([]Check{check}, 88, nil)
	if !report.FormalOK {
		t.Fatalf("gofpdf after typst fail must Formal: %#v", report)
	}
}

func TestValidateBrandIndependentPDFUsesFileBackendNotEnv(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", filepath.Join(t.TempDir(), "missing-typst.exe"))
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	data, _, err := RenderIndependentPDF(t.TempDir(), "标题", "订单 1280单")
	if err != nil {
		t.Fatal(err)
	}
	v, err := ValidateBrand(PDF, data, DefaultBrand())
	if err != nil {
		t.Fatal(err)
	}
	var pdfCheck Check
	for _, c := range v.Checks {
		if c.ID == "independent_pdf" {
			pdfCheck = c
		}
	}
	if pdfCheck.Status != "passed" || strings.Contains(pdfCheck.Message, "Typst 已验证") {
		t.Fatalf("validate used env probe: %#v", v.Checks)
	}
	report := EvaluateQuality(v.Checks, 0, nil)
	if !report.FormalOK {
		t.Fatalf("validated gofpdf PDF must Formal: %#v", report)
	}
}

func TestExportNoticeIndependentPDFOnly(t *testing.T) {
	if ExportNotice(PDF, false) != IndependentPDFNotice() {
		t.Fatalf("independent pdf notice: %q", ExportNotice(PDF, false))
	}
	docx := ExportNotice(DOCX, false)
	if strings.Contains(docx, "分页") || strings.Contains(docx, "像素") {
		t.Fatalf("docx export used independent notice: %q", docx)
	}
	same := ExportNotice(PDF, true)
	if strings.Contains(same, "不保证分页") {
		t.Fatalf("same-source pdf used independent notice: %q", same)
	}
}

func TestRenderIndependentPDFFallsBackWhenTypstMissing(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	data, check, err := RenderIndependentPDF(t.TempDir(), "标题", "订单 1280单")
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != "passed" || !strings.Contains(check.Message, "稳定独立 PDF") || !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("fallback: %#v %v", check, err)
	}
	if len(data) < 32 {
		t.Fatal("fallback dropped pdf body")
	}
}
