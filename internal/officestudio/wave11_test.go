package officestudio

import (
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/officetools"
)

func TestHeading3StyleUsesWordLadderNotHardcoded24(t *testing.T) {
	theme := ResolveTheme(DefaultBrand())
	data, err := Generate(Spec{
		SchemaVersion: 2,
		Kind:          DOCX,
		Title:         "标题阶梯",
		BrandID:       DefaultBrand().BrandID,
		TemplateID:    "research-report",
		Blocks: []Block{
			{Type: "heading", Text: "一章"},
			{Type: "heading3", Text: "小节"},
			{Type: "paragraph", Text: "正文 1280"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	styles := string(zipParts(t, data)["word/styles.xml"])
	heading := styles[strings.Index(styles, `w:styleId="Heading3"`):]
	if strings.Contains(heading, `w:sz w:val="24"`) && theme.WordBodySz != 24 {
		t.Fatalf("Heading3 still hardcoded 24; WordBodySz=%d styles=%s", theme.WordBodySz, heading[:min(400, len(heading))])
	}
	want := `w:sz w:val="` + strconv.Itoa(max(theme.WordBodySz, 20)) + `"`
	if !strings.Contains(heading, want) {
		t.Fatalf("Heading3 missing %s in %s", want, heading[:min(400, len(heading))])
	}
	if strings.Contains(heading, `w:color w:val="0B1F3A"`) && theme.Navy != "0B1F3A" {
		t.Fatalf("Heading3 leaked classic navy")
	}
}

func TestHeading3RunOverlayStillUsesBodyHalfPt(t *testing.T) {
	opts := officetools.StudioDocumentOptions{BodyHalfPt: 32, Navy: "112233", Latin: "Calibri", East: "Microsoft YaHei"}
	data, err := officetools.GenStudioDocxWithOptions("t", []officetools.StudioDocxBlock{{Type: "heading3", Text: "小节"}}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(zipParts(t, data)["word/document.xml"])
	if !strings.Contains(doc, `w:sz w:val="32"`) {
		t.Fatalf("run overlay lost BodyHalfPt: %s", doc[:min(400, len(doc))])
	}
}

func TestFactValueCellEmptyIsEmDashNotZero(t *testing.T) {
	got := factValueCell("")
	if got.Type == "number" || got.Value == "0" || got.Value == "0.00" {
		t.Fatalf("missing value became number zero: %#v", got)
	}
	if got.Type != "text" || got.Value != "—" {
		t.Fatalf("want text em dash, got %#v", got)
	}
	num := factValueCell("1280")
	if num.Type != "number" || num.Value != "1280" {
		t.Fatalf("non-empty decimal must stay number: %#v", num)
	}
}

func TestEmptyWorkbookPlaceholderValueIsEmDash(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "台账", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := spec.Sheets[1]
	if raw.Name != "原始数据" {
		t.Fatalf("sheet1=%s", raw.Name)
	}
	got := raw.Rows[1][1]
	if got.Type == "number" || got.Value == "0" || got.Value == "0.00" {
		t.Fatalf("placeholder became number zero: %#v", got)
	}
	if got.Value != "—" {
		t.Fatalf("want em dash, got %#v", got)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(zipParts(t, data)["xl/worksheets/sheet2.xml"])
	if strings.Contains(sheet, `>0</v>`) && !strings.Contains(sheet, "—") {
		t.Fatal("sheet stored 0 instead of missing marker")
	}
	insp, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	var sawDash bool
	for _, n := range insp.Nodes {
		if n.Text == "—" {
			sawDash = true
		}
	}
	if !sawDash {
		t.Fatal("inspect missing em dash for empty workbook placeholder")
	}
}

func TestRasterizedUnsupportedEffectRecordsReason(t *testing.T) {
	report := EvaluateQuality([]Check{RasterizedObjectCheck("glow", 1)}, 0, nil)
	if !strings.Contains(report.Coverage, "rasterized=1") {
		t.Fatalf("coverage hid rasterization: %q blockers=%v", report.Coverage, report.Blockers)
	}
	if strings.Contains(strings.ToLower(report.Coverage), "fully editable") {
		t.Fatal("must not claim fully editable after rasterization")
	}
	if !strings.Contains(report.Coverage, "visualScore=uncalibrated") || !strings.Contains(report.Coverage, "designerReviewed=0") {
		t.Fatalf("honesty flags dropped: %q", report.Coverage)
	}
}

func TestRasterizingBodyTextCannotClearFormal(t *testing.T) {
	report := EvaluateQuality([]Check{
		{ID: "native_render", Status: "passed", Message: "ok"},
		RasterizedObjectCheck("whole-slide", 1),
		{ID: "unexpected_raster_text", Status: "failed", Message: "正文被整页栅格化"},
	}, 90, nil)
	if report.FormalOK {
		t.Fatal("whole-slide raster of body cannot be Formal")
	}
}

func TestRasterChecksFromInspectionFlagsWholeSlidePicture(t *testing.T) {
	checks := RasterChecksFromInspection(Inspection{
		Kind: PPTX,
		Nodes: []Node{
			{ID: "s1-pic", Kind: "image", Part: "ppt/slides/slide1.xml", Image: &ImageInfo{}},
		},
	})
	if len(checks) != 1 || checks[0].ID != "rasterized_object" || checks[0].Status != "failed" {
		t.Fatalf("want whole-slide failed, got %#v", checks)
	}
	if strings.Contains(strings.ToLower(checks[0].Message), "fully editable") {
		t.Fatal("must not claim fully editable")
	}
	report := EvaluateQuality(append([]Check{{ID: "native_render", Status: "passed"}}, checks...), 0, nil)
	if report.FormalOK {
		t.Fatal("whole-slide picture cannot be Formal")
	}
	if !strings.Contains(report.Coverage, "visualScore=uncalibrated") || !strings.Contains(report.Coverage, "designerReviewed=0") {
		t.Fatalf("honesty flags dropped: %q", report.Coverage)
	}
}

func TestRasterChecksFromInspectionPhotosWithTextAreNotUnexpected(t *testing.T) {
	checks := RasterChecksFromInspection(Inspection{
		Kind: PPTX,
		Nodes: []Node{
			{ID: "s1-t", Kind: "text", Part: "ppt/slides/slide1.xml", Text: "结论 1280"},
			{ID: "s1-p", Kind: "image", Part: "ppt/slides/slide1.xml", Text: "照片与来源", Image: &ImageInfo{}},
		},
	})
	for _, c := range checks {
		if c.ID == "rasterized_object" && c.Status == "failed" {
			t.Fatal("photo plus text is not whole-slide raster")
		}
	}
}

func TestValidatePPTXWiresRasterChecksFromInspection(t *testing.T) {
	raw := studioTestImage(t, 80, 40, false)
	data := generatedPictures(t, studioImageSpec(raw))
	insp, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	want := RasterChecksFromInspection(insp)
	got, err := Validate(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Checks {
		if c.ID == "rasterized_object" && c.Status == "failed" {
			t.Fatalf("photo+text flagged whole-slide: %#v", c)
		}
	}
	for _, w := range want {
		found := false
		for _, c := range got.Checks {
			if c.ID == w.ID && c.Status == w.Status && c.Message == w.Message {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Validate dropped raster check %#v from %#v", w, got.Checks)
		}
	}
	nodes := imageNodes(t, data)
	stripped := regexp.MustCompile(`<a:t[^>]*>[^<]*</a:t>`).ReplaceAll(zipParts(t, data)[nodes[0].Part], nil)
	pictureOnly := editZIP(t, data, map[string][]byte{nodes[0].Part: stripped})
	v, err := Validate(PPTX, pictureOnly)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Checks {
		if c.ID == "rasterized_object" && c.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("picture-only slide missing whole-slide raster check: %#v", v.Checks)
	}
}

func TestIndependentPDFInputWritesConfidentialityWithoutInventing(t *testing.T) {
	_, body, _ := independentPDFInput(Spec{
		SchemaVersion: 2, Kind: PDF, Title: "月报", Confidentiality: "内部", Body: "订单 1280单",
	})
	if !strings.Contains(body, "密级：内部") {
		t.Fatalf("authored confidentiality dropped: %q", body)
	}
	if strings.Contains(body, "机密") {
		t.Fatal("invented confidentiality")
	}
	_, empty, _ := independentPDFInput(Spec{SchemaVersion: 2, Kind: PDF, Title: "月报", Body: "订单 1280单"})
	if strings.Contains(empty, "密级：") {
		t.Fatalf("invented confidentiality label: %q", empty)
	}
}

func TestIndependentPDFInputWritesAudiencePurposeWithoutInventing(t *testing.T) {
	_, body, _ := independentPDFInput(Spec{
		SchemaVersion: 2, Kind: PDF, Title: "月报", Audience: "客户", Purpose: "方案汇报", Body: "订单 1280单",
	})
	if !strings.Contains(body, "受众：客户") || !strings.Contains(body, "用途：方案汇报") {
		t.Fatalf("authored brief dropped: %q", body)
	}
	if strings.Contains(body, "1281") {
		t.Fatal("rewrote fact")
	}
	_, empty, _ := independentPDFInput(Spec{SchemaVersion: 2, Kind: PDF, Title: "月报", Body: "订单 1280单"})
	if strings.Contains(empty, "受众：管理层") || strings.Contains(empty, "用途：经营汇报") {
		t.Fatalf("invented brief defaults: %q", empty)
	}
}

func TestIndependentPDFInputUsesBrandTheme(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand := DefaultBrand()
	brand.BrandID = "pdf-east-brand"
	brand.Fonts.East = "Source Han Serif SC"
	if err := RegisterBrand(brand); err != nil {
		t.Fatal(err)
	}
	title, body, theme := independentPDFInput(Spec{
		SchemaVersion: 2, Kind: PDF, Title: "月报", BrandID: brand.BrandID, Body: "订单 1280单",
	})
	if title != "月报" || !strings.Contains(body, "订单 1280单") {
		t.Fatalf("input dropped content title=%q body=%q", title, body)
	}
	if theme.East != "Source Han Serif SC" {
		t.Fatalf("theme.East=%q", theme.East)
	}
	markup := typstMarkupWithBrand(title, body, theme)
	if !strings.Contains(markup, "Source Han Serif SC") {
		t.Fatalf("markup missing brand font: %s", markup[:min(400, len(markup))])
	}
}

func TestRunTypstWritesBrandFontIntoMarkup(t *testing.T) {
	dir := t.TempDir()
	dummy := filepath.Join(dir, "typst.bat")
	if err := os.WriteFile(dummy, []byte("@echo off\r\nexit /b 1\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := runTypst(dir, "月报", "订单 1280单", dummy, Theme{East: "Source Han Serif SC"})
	if err == nil {
		t.Fatal("dummy typst must fail compile")
	}
	markup, readErr := os.ReadFile(filepath.Join(dir, "independent.typ"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(markup), "Source Han Serif SC") {
		t.Fatalf("runTypst ignored theme: %s", markup)
	}
}

func TestRenderIndependentPDFWithThemeFallsBackWhenTypstMissing(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	data, check, err := RenderIndependentPDFWithTheme(t.TempDir(), "标题", "订单 1280单", Theme{East: "Source Han Serif SC"})
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != "passed" || !strings.Contains(check.Message, "稳定独立 PDF") || !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("fallback: %#v", check)
	}
}

func TestGeneratePDFFallbackUsesBrandHeadingColor(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	t.Setenv("LUNITIDE_TYPST", "")
	brand := DefaultBrand()
	brand.BrandID = "pdf-navy-brand"
	brand.Colors = map[string]string{"navy": "AA1122"}
	if err := RegisterBrand(brand); err != nil {
		t.Fatal(err)
	}
	data, err := Generate(Spec{SchemaVersion: 2, Kind: PDF, Title: "月报", BrandID: brand.BrandID, Body: "订单 1280单"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inflatedPDFStreams(t, data), "0.667 0.067 0.133") {
		t.Fatal("Generate PDF fallback ignored brand navy")
	}
}

func inflatedPDFStreams(t *testing.T, data []byte) string {
	t.Helper()
	var b strings.Builder
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		rest = rest[i+6:]
		if len(rest) >= 2 && rest[0] == '\r' && rest[1] == '\n' {
			rest = rest[2:]
		} else if len(rest) >= 1 && (rest[0] == '\n' || rest[0] == '\r') {
			rest = rest[1:]
		}
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			break
		}
		raw := bytes.TrimSpace(rest[:j])
		rest = rest[j+9:]
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		out, err := io.ReadAll(zr)
		_ = zr.Close()
		if err == nil {
			b.Write(out)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestGeneratePDFUsesIndependentPDFInput(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	t.Setenv("LUNITIDE_TYPST", "")
	brand := DefaultBrand()
	brand.BrandID = "pdf-gen-brand"
	brand.Fonts.East = "Source Han Serif SC"
	if err := RegisterBrand(brand); err != nil {
		t.Fatal(err)
	}
	spec := Spec{SchemaVersion: 2, Kind: PDF, Title: "月报", BrandID: brand.BrandID, Body: "订单 1280单"}
	_, _, theme := independentPDFInput(spec)
	if theme.East != "Source Han Serif SC" {
		t.Fatalf("Generate input theme.East=%q", theme.East)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Fatal("Generate PDF missing header")
	}
}

func TestTypstMarkupUsesFontFallbackChain(t *testing.T) {
	brand := DefaultBrand()
	brand.Fonts.East = "Source Han Serif SC"
	brand.Fonts.Fallback = "Calibri, Noto Sans SC"
	markup := typstMarkupWithBrand("月报", "订单 1280单", ResolveTheme(brand))
	if !strings.Contains(markup, "Source Han Serif SC") || !strings.Contains(markup, "Noto Sans SC") {
		t.Fatalf("fallback chain missing: %s", markup[:min(400, len(markup))])
	}
	if strings.Contains(markup, "已嵌入微软雅黑") {
		t.Fatal("markup claimed system font embed")
	}
}

func TestIndependentPDFMarkupUsesBrandFontNotClassicOnly(t *testing.T) {
	brand := DefaultBrand()
	brand.Fonts.East = "Source Han Serif SC"
	markup := typstMarkupWithBrand("月报", FormatIndependentReport("月报", "管理层", "复盘", "订单 1280单", []string{"src-1"}), ResolveTheme(brand))
	if !strings.Contains(markup, "Source Han Serif SC") {
		t.Fatalf("typst markup ignored brand font: %s", markup[:min(400, len(markup))])
	}
	if strings.Contains(markup, "PDF/A") && strings.Contains(markup, "已符合") {
		t.Fatal("markup claimed PDF/A")
	}
}

func TestIndependentPDFNoticeStillNotPDFA(t *testing.T) {
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	if !strings.Contains(IndependentPDFNotice(), "不保证分页") {
		t.Fatalf("notice=%q", IndependentPDFNotice())
	}
	if IndependentPDFACheck().Status != "unsupported" {
		t.Fatalf("pdfa=%#v", IndependentPDFACheck())
	}
}

func TestCompareExternalAdaptersMissingStaysFailClosed(t *testing.T) {
	t.Setenv("LUNITIDE_PRESENTON", "")
	t.Setenv("LUNITIDE_PPTXGENJS", "")
	rep := CompareExternalAdapters()
	if rep.Presenton.Available || rep.PptxGenJS.Available {
		t.Fatalf("empty env must be unavailable: %#v", rep)
	}
	q := QualityFromExternalAdapter(rep.Presenton)
	if q.FormalOK {
		t.Fatal("missing Presenton must not be Formal")
	}
	if strings.Contains(rep.Notice, "已超过") || strings.Contains(strings.ToLower(rep.Notice), "gamma") {
		t.Fatalf("comparison invented competitor claim: %q", rep.Notice)
	}
	if !strings.Contains(rep.Notice, "未进入生产主链") {
		t.Fatalf("notice=%q", rep.Notice)
	}
}

func TestCompareExternalAdaptersDoesNotBypassArtifact(t *testing.T) {
	rep := CompareExternalAdapters()
	if rep.SameQualityGate != true {
		t.Fatal("external results must stay on the same QualityReport gate")
	}
}

func TestMeasureOfficeFixturesSkipsFakeP50(t *testing.T) {
	samples := MeasureOfficeFixtures(0)
	if len(samples) == 0 {
		t.Fatal("expected skip samples")
	}
	for _, s := range samples {
		if !s.Skipped {
			t.Fatalf("n=0 must skip, got %#v", s)
		}
		if s.Reason == "" || strings.Contains(strings.ToLower(s.Reason), "p50=") {
			t.Fatalf("must not invent p50: %#v", s)
		}
	}
	if p50 := SummarizePerf(samples); p50.Ready {
		t.Fatalf("unready summary leaked ready P50: %#v", p50)
	}
}

func TestApplyLayoutPlanningWritesReadingOrderIntoNotes(t *testing.T) {
	metrics := make([]MetricBlock, 8)
	for i := range metrics {
		metrics[i] = MetricBlock{Label: "项", Value: "1280", Unit: "单", FactID: "orders"}
	}
	out, err := applyLayoutPlanning(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "经营汇报",
		Slides: []Slide{{
			Title: "指标", Layout: "metrics", Metrics: metrics, EvidenceRefs: []string{"orders"},
		}},
	})
	if err != nil || len(out.Slides) < 2 {
		t.Fatalf("split: %v n=%d", err, len(out.Slides))
	}
	for i, s := range out.Slides {
		if !strings.Contains(s.Notes, "阅读顺序 "+strconv.Itoa(i+1)) {
			t.Fatalf("slide %d notes missing reading order: %q", i, s.Notes)
		}
		if !strings.Contains(s.Notes, "orders") {
			t.Fatalf("slide %d dropped evidence: %q", i, s.Notes)
		}
	}
}

func TestValidatePDFIncludesHonestPDFAUnsupported(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	data, err := Generate(Spec{SchemaVersion: 2, Kind: PDF, Title: "月报", Body: "订单 1280单"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := Validate(PDF, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Checks {
		if c.ID != "pdfa" {
			continue
		}
		found = true
		if c.Status != "unsupported" || strings.Contains(c.Message, "已符合") {
			t.Fatalf("pdfa: %#v", c)
		}
	}
	if !found {
		t.Fatalf("Validate PDF dropped pdfa: %#v", v.Checks)
	}
}

func TestHeading1And2StylesUseBrandNavy(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand := DefaultBrand()
	brand.BrandID = "word-navy-brand"
	brand.Colors = map[string]string{"navy": "AA1122"}
	if err := RegisterBrand(brand); err != nil {
		t.Fatal(err)
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "标题色", BrandID: brand.BrandID,
		Blocks: []Block{
			{Type: "heading", Text: "一章"},
			{Type: "heading2", Text: "二节"},
			{Type: "paragraph", Text: "正文 1280"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	styles := string(zipParts(t, data)["word/styles.xml"])
	h1 := styles[strings.Index(styles, `w:styleId="Heading1"`):]
	h2 := styles[strings.Index(styles, `w:styleId="Heading2"`):]
	if !strings.Contains(h1[:min(500, len(h1))], `w:color w:val="AA1122"`) {
		t.Fatalf("Heading1 missing brand navy: %s", h1[:min(400, len(h1))])
	}
	if !strings.Contains(h2[:min(500, len(h2))], `w:color w:val="AA1122"`) {
		t.Fatalf("Heading2 missing brand navy: %s", h2[:min(400, len(h2))])
	}
}

func TestWithBrandLogoIssuesUsesTaskInsetNotDefaultOnly(t *testing.T) {
	n := Node{ID: "node_logo", Part: "ppt/slides/slide1.xml", Image: &ImageInfo{
		X: DefaultBrand().Logo.SafeInsetEMU, Y: DefaultBrand().Logo.SafeInsetEMU, Width: 400000, Height: 400000,
	}}
	insp := Inspection{Kind: PPTX, Nodes: []Node{n}, Issues: LogoSafeAreaIssues([]Node{n}, DefaultBrand())}
	if len(insp.Issues) != 0 {
		t.Fatalf("default inset flagged: %#v", insp.Issues)
	}
	wide := DefaultBrand()
	wide.Logo.SafeInsetEMU = DefaultBrand().Logo.SafeInsetEMU + 200000
	got := WithBrandLogoIssues(insp, wide)
	if len(got.Issues) == 0 || got.Issues[0].Code != "OFFICE_LOGO_SAFE_AREA" || got.Issues[0].NodeID != "node_logo" {
		t.Fatalf("wider brand inset must flag: %#v", got.Issues)
	}
	if strings.Contains(got.Issues[0].Message, "1281") {
		t.Fatal("logo check rewrote fact")
	}
}

func TestLicensePackChecklistIsUnreviewed(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "office-quality", "license-pack-checklist.json")
	pack, err := LoadLicensePackChecklist(path)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Reviewed {
		t.Fatal("empty pack must not be reviewed")
	}
	if err := pack.Validate(); err != nil {
		t.Fatal(err)
	}
	pack.Reviewed = true
	if err := pack.Validate(); err == nil {
		t.Fatal("reviewed=true with empty items must fail")
	}
}

func TestLoadLicensePackChecklistRejectsReviewedEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"reviewed":true,"items":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLicensePackChecklist(path); err == nil {
		t.Fatal("reviewed empty pack must fail load")
	}
}
