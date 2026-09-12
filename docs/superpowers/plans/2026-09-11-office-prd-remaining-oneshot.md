# Office PRD Remaining One-Shot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the remaining *engineer-completable* items from `docs/design/PRD-office-quality-commercial-2026-09-11` (HTML and Markdown are the same document) in one sequential pass, without claiming the whole PRD product-验收 is 100%.

**Architecture:** Keep `office.generate` → `officestudio.Generate` → backends → `officerender` → Artifact/Patch. Presenton / PptxGenJS / Typst stay adapters. Imports stay Patch-only. Formal stays blockers-only. Do not invent designer-reviewed=36, calibrated visual 85, live Office/WPS pass, or competitor scores.

**Tech Stack:** Go `internal/officestudio`, `internal/officetools`, `internal/officeapp`, `internal/domain/officestudio`; existing Vitest under `web/src/officeStudio`.

**Spec:** [docs/design/PRD-office-quality-commercial-2026-09-11.md](../../design/PRD-office-quality-commercial-2026-09-11.md) §6 路线 C, §8 FR01–FR16, §11–§15. HTML twin: `docs/design/PRD-office-quality-commercial-2026-09-11.html`.

## Global Constraints

- Stay on `feat/prd-v7-s1-continuity`. No commit / push / VERSION / pack unless the user asks.
- Frozen `token_ledger`; no email/shared calendar; `html.gen` stays `penalty-shootout|timer|checklist`.
- Do not invent savings %, Word weekly-report metrics, or “supplier passed”.
- Do not claim beat Gamma / Plus AI / Beautiful.ai; do not claim 36 designer-checked variants; do not claim calibrated visual 85.
- `designerReviewed` stays `0` until a human designer actually reviews.
- Formal still requires real blockers-only; skip honest `target-*` / `visual-model` / `pdfa` gaps in `QualityFor`.
- DESIGN=off must not leak imported brand fonts/colors.
- `LUNITIDE_CHAT_LANES=off` must not break the typed office path.
- Windows `go test` / `npm` need `required_permissions: ["all"]`.
- Live app stays 0.4.75 until rebuild/restart — never claim 真机已修.
- FR17 (org template approval) and FR18 (Univer / realtime collab) stay explicit no.
- Do not edit `c:\Users\mujun\.cursor\plans\office_quality_commercial_a4a3b6e5.plan.md`.

---

## Honest scope of this one-shot

This plan can finish remaining **code** on the current branch. It cannot finish the HTML PRD as a product.

| 口径 | 当前（Waves 1–10） | 本计划做完后的上限 | 不能用代码做成 100% 的原因 |
|---|---|---|---|
| 首发工程 FR01–FR16 | ~79% | ~84% | FR04 设计师已检、FR05 真实字形、FR06/07 实机与视觉模型、FR15 出版内核、FR16 真实外部进程 |
| P0 FR01–FR13 | ~84% | ~88% | 同上，P0 里最大洞仍是 FR04 |
| FR01–FR18 | ~70% | ~75% | FR17/FR18 = 0 且不做 |
| 整份文档产品验收 | ~57% | ~60% | §10/§14–§19 需要设计师、真人盲评、实机、试点、许可复核 |

执行本计划之后，**不要**把完成度改成 100%。只把工程尾巴收干净，并把测试附录里的项标成「夹具已备 / 仍待人工或实机」。

---

## File map

| File | Responsibility in this pass |
|---|---|
| `internal/officetools/studio_docx.go` | Heading3 style sz/color from theme, not hardcoded `24` / `0B1F3A` |
| `internal/officetools/docx_design.go` | Shared heading-size helper if Heading3 must match the Word ladder |
| `internal/officestudio/workbook.go` | Missing-value cell display (`—`, never store `0` for empty) |
| `internal/officestudio/generate.go` | Rasterization reason check; keep date/number rewrite |
| `internal/officestudio/adapter.go` | Independent PDF brand tokens; adapter comparison report |
| `internal/officestudio/quality.go` | Accept new honest checks without turning Formal into a lie |
| `internal/officestudio/wave11_test.go` | Red lights for this pass |
| `internal/officestudio/perf_harness.go` | Record elapsed locally; never invent P50 |
| `testdata/office-quality/license-pack-checklist.json` | AssetRecord fields the pack must list; `reviewed=false` |
| Testing appendix only | Live Office/WPS, designer 36, blind eval, PDF/A validator, 100-task set |

---

### Task 1: Word Heading3 follows the Word body ladder (FR10)

**Files:**
- Create: `internal/officestudio/wave11_test.go`
- Modify: `internal/officetools/studio_docx.go` (Heading3 style XML around the current hardcoded `w:sz w:val="24"`)
- Modify: `internal/officetools/docx_design.go` only if a shared helper is required
- Test: `internal/officestudio/wave11_test.go`, existing `internal/officestudio/wave9_test.go`

**Interfaces:**
- Consumes: `Theme.WordBodySz` (`BrandFonts.WordBodyPt * 2`), `StudioDocumentOptions.BodyHalfPt`, `StudioDocumentOptions.Navy`
- Produces: Heading3 style default size = `max(WordBodySz, 20)` half-points; color = brand navy when options are present; run overlay still wins via `docxThemedParagraph`

- [ ] **Step 1: Write the failing test**

```go
package officestudio

import (
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
		Document: []Block{
			{Type: "heading", Text: "一章"},
			{Type: "heading3", Text: "小节"},
			{Type: "paragraph", Text: "正文 1280"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	styles := zipText(t, data, "word/styles.xml")
	if strings.Contains(styles, `w:styleId="Heading3"`) && strings.Contains(styles, `w:sz w:val="24"`) && theme.WordBodySz != 24 {
		t.Fatalf("Heading3 still hardcoded 24; WordBodySz=%d styles=%s", theme.WordBodySz, styles[strings.Index(styles, "Heading3"):min(strings.Index(styles, "Heading3")+400, len(styles))])
	}
	want := `w:sz w:val="` + strconv.Itoa(max(theme.WordBodySz, 20)) + `"`
	heading := styles[strings.Index(styles, `w:styleId="Heading3"`):]
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
	doc := zipText(t, data, "word/document.xml")
	if !strings.Contains(doc, `w:sz w:val="32"`) {
		t.Fatalf("run overlay lost BodyHalfPt: %s", doc[:min(400, len(doc))])
	}
}
```

Use the existing `zipText` helper from `wave9_test.go` / `officestudio_test.go`. If the helper is not exported from the test file, copy the same unexported helper already used in this package's tests. Do not invent a second unzip path.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestHeading3StyleUsesWordLadderNotHardcoded24|TestHeading3RunOverlayStillUsesBodyHalfPt"`

Expected: FAIL because `studio_docx.go` still embeds `w:sz w:val="24"` and `w:color w:val="0B1F3A"` in the Heading3 style.

- [ ] **Step 3: Write minimal implementation**

Replace the Heading3 style injection so size and color come from `options`:

```go
heading3Sz := 24
heading3Color := "0B1F3A"
if options != nil {
    if options.BodyHalfPt > 0 {
        heading3Sz = options.BodyHalfPt
        if heading3Sz < 20 {
            heading3Sz = 20
        }
    }
    if options.Navy != "" {
        heading3Color = options.Navy
    }
}
styles = strings.Replace(docxStylesXML, `</w:styles>`, fmt.Sprintf(
    `<w:style w:type="paragraph" w:styleId="Heading3">...</w:rPr><w:sz w:val="%d"/>...<w:color w:val="%s"/>...</w:style></w:styles>`,
    heading3Sz, heading3Color), 1)
```

Keep `docxThemedParagraph` run overlay. Do not rewrite Heading1/Heading2 style XML in this task.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio ./internal/officetools -count=1 -timeout 90s -run "TestHeading3|TestWordBody"`

Expected: PASS. `TestWordBodyUsesBrandWordLadderNotPPTBodyPt` still green.

---

### Task 2: Excel missing-value display stays honest (FR11)

**Files:**
- Modify: `internal/officestudio/workbook.go` `factValueCell`
- Modify: `internal/officestudio/generate.go` only if empty number cells currently write `0`
- Test: `internal/officestudio/wave11_test.go`

**Interfaces:**
- Consumes: `Fact.Value`, `Fact.Unit`, `Cell.Type`
- Produces: `factValueCell("")` → `{Type:"text", Value:"—"}`; never `{Type:"number", Value:"0"}` for a missing fact; stored non-empty decimals stay numbers

- [ ] **Step 1: Write the failing test**

```go
func TestMissingFactValueIsEmDashNotZero(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "台账", []Fact{{
		FactID: "f-empty", Value: "", Unit: "单", Period: "2026-08", Locator: "空值指标", Status: "ok", Locked: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw := spec.Sheets[1]
	got := raw.Rows[1][1]
	if got.Type == "number" || got.Value == "0" || got.Value == "0.00" {
		t.Fatalf("missing value became number zero: %#v", got)
	}
	if got.Value != "—" {
		t.Fatalf("want em dash, got %#v", got)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	insp, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range insp.Nodes {
		if strings.Contains(n.Text, "空值指标") {
			continue
		}
		if n.Text == "0" || n.Text == "0.00" {
			t.Fatalf("inspect showed invented zero: %#v", n)
		}
	}
	if strings.Contains(string(mustZip(t, data, "xl/worksheets/sheet2.xml")), `>0</v>`) &&
		!strings.Contains(string(mustZip(t, data, "xl/worksheets/sheet2.xml")), "—") {
		t.Fatal("sheet stored 0 instead of missing marker")
	}
}
```

Adapt `mustZip` to the existing zip helper name in this package. Do not assert the prohibition sentence in chat evidence.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run TestMissingFactValueIsEmDashNotZero`

Expected: FAIL because `factValueCell("")` currently returns `{Type:"text", Value:""}` and the empty cell can look like a blank number after excelize write.

- [ ] **Step 3: Write minimal implementation**

```go
func factValueCell(value string) Cell {
	value = strings.TrimSpace(value)
	if value == "" {
		return Cell{Type: "text", Value: "—"}
	}
	if decimalPattern.MatchString(value) {
		return Cell{Type: "number", Value: value}
	}
	return Cell{Type: "text", Value: value}
}
```

Do not change stored non-empty numbers. Do not rewrite formulas. Keep `=COUNTA('原始数据'!A2:A1048576)` pointing at 原始数据.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio -count=1 -timeout 90s -run "TestMissingFactValue|TestPlanWorkbook|TestCounta|TestDate"`

Expected: PASS. Existing COUNTA / date ISO tests stay green.

---

### Task 3: Record rasterization reason, never call a photo slide editable (FR06)

**Files:**
- Modify: `internal/officestudio/generate.go` or `internal/officestudio/images.go`
- Modify: `internal/officestudio/quality.go` only if a new check id is added
- Test: `internal/officestudio/wave11_test.go`

**Interfaces:**
- Consumes: existing image validation (`validateRaster`), geometry `unsupported`
- Produces: `Check{ID:"rasterized_object", Status:"passed"|"warning", Message:"reason=..."}` when a slide object is stored as a picture because the effect is unsupported; QualityReport coverage must contain `rasterized=` count; Formal must not become OK by rasterizing text

- [ ] **Step 1: Write the failing test**

```go
func TestRasterizedUnsupportedEffectRecordsReason(t *testing.T) {
	report := EvaluateQuality([]Check{RasterizedObjectCheck("glow", 1)}, 0, nil)
	if !strings.Contains(report.Coverage, "rasterized=1") && !hasCheck(report, "rasterized_object") {
		t.Fatalf("coverage hid rasterization: %q blockers=%v", report.Coverage, report.Blockers)
	}
	if strings.Contains(strings.ToLower(report.Coverage), "fully editable") {
		t.Fatal("must not claim fully editable after rasterization")
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
```

Add `hasCheck` locally if needed. `RasterizedObjectCheck` does not exist yet — that is the red.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestRasterizedUnsupportedEffectRecordsReason|TestRasterizingBodyTextCannotClearFormal"`

Expected: FAIL compile or FAIL missing check id.

- [ ] **Step 3: Write minimal implementation**

```go
func RasterizedObjectCheck(reason string, count int) Check {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unsupported-effect"
	}
	status := "warning"
	if reason == "whole-slide" {
		status = "failed"
	}
	return Check{ID: "rasterized_object", Status: status, Message: fmt.Sprintf("reason=%s count=%d；栅格对象不计入可编辑覆盖率", reason, count)}
}
```

Append `rasterized=%d` onto `QualityReport.Coverage` when any such check is present. Do not mark photos as unexpected raster. Do not add a visual model.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestRasterized|TestEvaluateQuality|TestFormal"`

Expected: PASS. Formal still blockers-only.

---

### Task 4: Independent PDF carries brand tokens without claiming PDF/A (FR15, FR02)

**Files:**
- Modify: `internal/officestudio/adapter.go` `typstMarkup`, `RenderIndependentPDF`
- Modify: `internal/officetools/pdf.go` only if gofpdf heading color/font can take explicit names already used by `GenStablePDF`
- Test: `internal/officestudio/wave11_test.go`, existing `adapter_test.go`

**Interfaces:**
- Consumes: `ResolveTheme(brand)`, `FormatIndependentReport`, `IndependentPDFNotice`, `IndependentPDFACheck`
- Produces: Typst markup `#set text(font: ...)` and a fill from theme navy when brand is passed; gofpdf fallback still writes 封面/目录/正文/引用; notice still says 导出 ≠ PDF/A

- [ ] **Step 1: Write the failing test**

```go
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
	if !strings.Contains(IndependentPDFNotice(), "不保证分页") {
		t.Fatalf("notice=%q", IndependentPDFNotice())
	}
	if IndependentPDFACheck().Status != "unsupported" {
		t.Fatalf("pdfa=%#v", IndependentPDFACheck())
	}
}
```

If you keep the current `typstMarkup(title, body)` signature, add `typstMarkupWithBrand` and call it from `runTypst`. Do not assert Inspect Preview contains「目录」or「1280」— PDF Inspect preview is a fixed notice.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestIndependentPDFMarkupUsesBrandFontNotClassicOnly|TestIndependentPDFNoticeStillNotPDFA"`

Expected: FAIL because `typstMarkup` has no brand font line.

- [ ] **Step 3: Write minimal implementation**

```go
func typstMarkupWithBrand(title, body string, theme Theme) string {
	var b strings.Builder
	b.WriteString("#set page(paper: \"a4\")\n")
	if theme.East != "" {
		b.WriteString("#set text(font: \"" + typstEscape(theme.East) + "\")\n")
	}
	// keep existing 封面/目录/正文/引用 heading switch
	return b.String()
}
```

Missing Typst still falls back to gofpdf. Do not set `independent_pdf` to passed without a binary.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio ./internal/officetools -count=1 -timeout 90s -run "TestIndependentPDF|TestTypst|TestExportNotice"`

Expected: PASS.

---

### Task 5: Fail-closed adapter comparison harness (FR16)

**Files:**
- Create: `internal/officestudio/adapter_compare.go`
- Test: `internal/officestudio/wave11_test.go`

**Interfaces:**
- Consumes: `ProbePresenton()`, `ProbePptxGenJS()`, `QualityFromExternalAdapter`, `EvaluateQuality`
- Produces: `type AdapterCompareReport struct { Presenton, PptxGenJS ExternalAdapterStatus; SameQualityGate bool; Notice string }`; `CompareExternalAdapters()` never returns FormalOK when either adapter is missing; never writes competitor scores

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestCompareExternalAdapters"`

Expected: FAIL compile, `CompareExternalAdapters` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
type AdapterCompareReport struct {
	Presenton, PptxGenJS ExternalAdapterStatus
	SameQualityGate      bool
	Notice               string
}

func CompareExternalAdapters() AdapterCompareReport {
	return AdapterCompareReport{
		Presenton:       ProbePresenton(),
		PptxGenJS:       ProbePptxGenJS(),
		SameQualityGate: true,
		Notice:          "外部生成器只做适配器对照，未进入生产主链；缺进程时检查为 missing，不能绕过交付门槛。",
	}
}
```

Do not shell out to Presenton. Do not add a second generate kernel.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run "TestCompareExternalAdapters|TestIndependentPDFMissing|TestProbe"`

Expected: PASS.

---

### Task 6: Performance fixture harness without fake P50 (PRD §18)

**Files:**
- Create: `internal/officestudio/perf_harness.go`
- Test: `internal/officestudio/wave11_test.go`

**Interfaces:**
- Consumes: `Generate(Spec)` for the existing 12-fixture titles under `testdata/office-quality/`
- Produces: `type PerfSample struct { Fixture, Kind string; ElapsedMS int64; Skipped bool; Reason string }`; `MeasureOfficeFixtures(n int) []PerfSample`; if `n < 30`, every sample has `Skipped=true` and `Reason="need ≥30 runs on a fixed machine"`; never write a P50 number when skipped

- [ ] **Step 1: Write the failing test**

```go
func TestMeasureOfficeFixturesSkipsFakeP50(t *testing.T) {
	samples := MeasureOfficeFixtures(1)
	if len(samples) == 0 {
		t.Fatal("expected skip samples")
	}
	for _, s := range samples {
		if !s.Skipped {
			t.Fatalf("n=1 must skip, got %#v", s)
		}
		if s.Reason == "" || strings.Contains(strings.ToLower(s.Reason), "p50=") {
			t.Fatalf("must not invent p50: %#v", s)
		}
	}
	if p50 := SummarizePerf(samples); p50.Ready || p50.P50MS != 0 {
		t.Fatalf("unready summary leaked P50: %#v", p50)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run TestMeasureOfficeFixturesSkipsFakeP50`

Expected: FAIL compile.

- [ ] **Step 3: Write minimal implementation**

```go
type PerfSample struct {
	Fixture, Kind, Reason string
	ElapsedMS             int64
	Skipped               bool
}

type PerfSummary struct {
	Ready bool
	P50MS int64
	N     int
}

func MeasureOfficeFixtures(n int) []PerfSample {
	if n < 30 {
		return []PerfSample{{Skipped: true, Reason: "need ≥30 runs on a fixed machine; not a product P50"}}
	}
	// optional: loop fixtures and time Generate; still do not claim product P50 in Coverage
	return nil
}

func SummarizePerf(samples []PerfSample) PerfSummary {
	for _, s := range samples {
		if s.Skipped {
			return PerfSummary{Ready: false}
		}
	}
	return PerfSummary{Ready: false, N: len(samples)}
}
```

Do not put `P50≤120s` into QualityReport.Coverage.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run TestMeasureOfficeFixturesSkipsFakeP50`

Expected: PASS.

---

### Task 7: License pack checklist stays unreviewed (PRD §17)

**Files:**
- Create: `testdata/office-quality/license-pack-checklist.json`
- Create: `internal/officestudio/license_pack.go` if a loader is cleaner than ad-hoc JSON in the test
- Test: `internal/officestudio/wave11_test.go`

**Interfaces:**
- Consumes: `AssetRecord` fields already on brand import
- Produces: checklist JSON with `reviewed: false` and required keys `sourceUrl, author, license, digest, commercial, redistributable`; loader rejects `reviewed: true` when any digest is empty

- [ ] **Step 1: Write the failing test and checklist file**

```json
{
  "reviewed": false,
  "notice": "发行包许可复核尚未完成。不得把本文件当作已通过商用审查。",
  "requiredFields": ["sourceUrl", "author", "license", "digest", "commercial", "redistributable"],
  "items": []
}
```

```go
func TestLicensePackChecklistIsUnreviewed(t *testing.T) {
	pack, err := LoadLicensePackChecklist("testdata/office-quality/license-pack-checklist.json")
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
```

Resolve the path the same way `testdata/office-quality` fixtures are already loaded in `contract_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run TestLicensePackChecklistIsUnreviewed`

Expected: FAIL missing file or loader.

- [ ] **Step 3: Write minimal implementation**

Load JSON. `Validate` returns error if `Reviewed && len(Items)==0`. Do not scan the whole module graph. Do not claim NOTICE is complete.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/officestudio -count=1 -timeout 60s -run TestLicensePackChecklistIsUnreviewed`

Expected: PASS.

---

### Task 8: Focused engineering gate (not the product test appendix)

**Files:** none new

- [ ] **Step 1: Run the engineering gate**

```text
go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio -count=1 -timeout 180s
go test ./internal/app -count=1 -timeout 180s -run "TestOffice|TestStudio|TestGenerate|TestChatLanesOff|TestClassifyChatLane"
npx vitest run src/officeStudio
go build ./cmd/engine ./cmd/desktop
```

Working directory for npm: `web`. PowerShell: use `;`, not `&&`.

Expected: all PASS. `npx vitest run src/officeStudio` stays in the existing 19-file officeStudio set unless this pass adds a UI file (it should not).

- [ ] **Step 2: Adjacent honesty scan**

Confirm these still hold:

- Chat evidence tests do not assert `!strings.Contains(content, "savings")`
- Blind-eval notice may contain「已超过」only inside the prohibition
- `decodePayload` still uses `DisallowUnknownFields`
- No hand-edit of `web/src/generated/bridge.ts` or `internal/bridge/schema_generated.go`
- DESIGN=off still drops imported fonts/colors
- Formal still skips honest target-app / visual-model / pdfa gaps
- `designerReviewed` still `0`
- Coverage still contains `visualScore=uncalibrated`
- COUNTA still points at `'原始数据'!A2:A1048576`
- Date cells still rewrite to ISO `t="d"`

- [ ] **Step 3: Do not claim**

Do not update VERSION. Do not say 0.4.75 真机已修. Do not set first-ship completion to 100%. After this pass, first-ship engineering should land around 84%, not 100%.

---

## Testing appendix — separate workstream

These items are **not** part of the one-shot coding pass. Do not mark them done because unit tests are green. Do not invent scores to fill the empty files.

### T-A. Already-used engineering gate (can re-run anytime)

| 项 | 命令 | 通过标准 |
|---|---|---|
| Studio / tools / app / render | `go test ./internal/officestudio ./internal/officetools ./internal/officeapp ./internal/officerender ./internal/domain/officestudio` | PASS |
| Office + lanes regression | `go test ./internal/app -run "TestOffice|TestStudio|TestGenerate|TestChatLanesOff|TestClassifyChatLane"` | PASS |
| Studio UI | `npx vitest run src/officeStudio` | existing officeStudio files PASS |
| Build | `go build ./cmd/engine ./cmd/desktop` | PASS |

This gate proves the branch compiles and contracts hold. It does **not** prove PowerPoint, WPS, or a designer signed the templates.

### T-B. Live target-app matrix (FR06 / FR07 / §15 打开率)

| 项 | 需要的机器 | 记录方式 | 未跑时的诚实状态 |
|---|---|---|---|
| Microsoft PowerPoint 打开 + 改文本/表/图后保存重开 | 已装 Microsoft 365 / 桌面 PowerPoint 的 Windows | 每份夹具一行：版本、打开、修复提示、可编辑对象 | `target-powerpoint=unsupported` |
| WPS 演示/文字/表格 同样步骤 | 已装 WPS | 同上 | `target-wps=unsupported` |
| LibreOffice 渲染 + 域更新 + 重算 | 用户机器已装 LO，走现有 `officerender` | 现有 `native_render` / `actual-render` | missing → 不能 Formal |
| 可编辑覆盖率分母 | 只统计应原生编辑的对象；照片不计入；单独统计意外栅格 | 与 Task 3 的 `rasterized_object` 对齐 | 无实机编辑则不得写 ≥95% |

Do not write a passing percentage into `QualityReport` from XML-only Inspect.

### T-C. Designer template review (FR04 / §10 / §14)

| 项 | 谁做 | 通过标准 | 未做时 |
|---|---|---|---|
| 36 个工程变体逐个看封面、正文、图表、长表、极端内容 | 有商业文档经验的设计师 | 把 `designerReviewed` 从 0 改到实际已检数量 | 保持 `designerReviewed=0` |
| 3 套 Word 正式报告、3 套 Excel 经营簿 | 同一设计师 | 分页、表头、打印预览签字 | 不得标「已检」 |
| Logo 安全区、字号阶梯、禁用色 | 设计师 + 现有 BrandProfile | 与 BrandProfile 版本绑定 | 工程默认值不是验收 |

Engineering must not flip `designerReviewed` to 36.

### T-D. 12-fixture blind eval and competitor compare (PRD §16)

| 项 | 材料 | 规则 |
|---|---|---|
| 夹具 | `testdata/office-quality/*.json` 已有 12 份 | 用同一事实、受众、语言、页数 |
| 盲评 | 至少 3 名相关经验评审，先藏品牌来源 | 写入 `testdata/office-quality/blind-eval.json` 的 `items` |
| 竞品 | 仅合法可用的账号；不支持该格式的产品不记生成失败 | `competitorScores` 为空则保持为空 |
| 禁止 | 不得在 `reviewed=0` 时填写 Gamma/Plus AI/Beautiful.ai 分数 | 现文件 `reviewed: 0`, `items: []` |

`blind-eval.json` 里的「已超过」只允许出现在禁止声明中。

### T-E. Visual score calibration (FR07 / §15.1)

| 项 | 口径 | 未校准 |
|---|---|---|
| 规则分 | 现有 `RuleVisualScore` | Coverage 必须继续写 `visualScore=uncalibrated` |
| 视觉模型 | 需要支持图片输入的模型 + 页码/节点建议 | 检查保持 `visual-model=unsupported` |
| 85 分试行门槛 | 先用标注集校准，再谈门槛 | 不得把规则分展示成行业认证 |

### T-F. PDF/A / PDF/UA (FR15 / §13.4)

| 项 | 工具 | 未跑 |
|---|---|---|
| PDF/A | 外部验证器，不是 `gofpdf` 导出成功 | `pdfa=unsupported` |
| PDF/UA | 专项无障碍标准 | 同上 |
| 同源 PDF vs Word 分页 | 实机并排 | 文案保持「分页 ≠ Word」 |

### T-G. Performance and cost (PRD §18)

| 项 | 定义 | 未跑 |
|---|---|---|
| 30 次以上 12 页 PPT / 20 页报告 / 5 Sheet | 固定机器、网络、模型 | Task 6 的 harness `Skipped=true` |
| P50 大纲 30s、首稿 120s、含修复 180s | 基线冻结后才能写进发布说明 | 不得把单次 `ElapsedMS` 写成产品 P50 |
| 接受交付物成本 | 全部尝试成本 ÷ 被接受份数 | 不得用 PRD 演示算式当账单 |

### T-H. Formal / LibreOffice / 100-task regression

| 项 | 规模 | 未做 |
|---|---|---|
| Formal 真机 | 本机有 LO 时跑 `native_render` | 无 LO 则 Formal 保持不可用 |
| 100 任务回归 | 40 PPT + 25 Word + 25 Excel + 10 独立 PDF | 现只有 12 份夹具 |
| 5–10 试点 | PRD §19 | 未做，不在本计划开发任务里 |

### T-I. What this appendix must never do

- Fill `blind-eval.json` competitor scores from memory.
- Mark `designerReviewed=36` because 36 variants exist in code.
- Claim PowerPoint/WPS passed because Inspect found DrawingML.
- Claim PDF/A because a PDF file opened.
- Claim 0.4.75 真机已修 after only `go test`.

---

## Spec coverage self-check

| Spec item | Where it lives after this plan |
|---|---|
| FR01 Brief fail-closed | Already landed; reverse tree still out |
| FR02 brand on four formats | Already landed + Task 4 PDF tokens |
| FR03 narrative | Already landed; Excel stays 说明-only |
| FR04 36 reviewed variants | Engineering catalog only; T-C |
| FR05 no truncate / measure | EstimateMeasure stays; real glyphs out |
| FR06 native edit + matrix | Honest unsupported + Task 3 raster reason; T-B |
| FR07 render diagnosis | Honest visual-model; T-E |
| FR08 bounded repair | Already landed |
| FR09 patch / import | Already landed; still Patch-only |
| FR10 Word styles | Task 1 Heading3 |
| FR11 Excel workbook | Already landed + Task 2 missing values |
| FR12 same-source PDF | Already landed; T-B/T-F |
| FR13 fact refs | Already landed in Studio preview nodes |
| FR14 brand L1 | Already landed; L2 masters out |
| FR15 independent PDF | Task 4 tokens; kernel / PDF/A in T-F |
| FR16 external adapters | Task 5 harness; still not production kernel |
| FR17 / FR18 | Explicit no |
| §16–§19 eval / pilots | Testing appendix only |

No task in this file implements FR17, FR18, a second generate kernel, or a fake 100% product-验收.

---

## Execution notes for the implementer

- One sequential pass: Task 1 → 8, then stop.
- If a task needs Typst or Presenton on the machine and they are absent, keep the fail-closed path. Do not install them silently as a product dependency.
- After Task 8, update the completion canvas only if scores actually moved, and keep full-document 验收 well below 70%.
- Testing appendix is a checklist for humans and live Office, not a coding backlog disguised as “almost done”.
