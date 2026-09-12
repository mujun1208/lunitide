package officeapp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/officestudio"
)

func zipOfficePart(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if _, err = b.ReadFrom(r); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		return b.Bytes()
	}
	t.Fatalf("missing part %s", name)
	return nil
}

func TestFormatBriefEvidenceDoesNotInventDefaults(t *testing.T) {
	empty := FormatBriefEvidence(officestudio.Brief{})
	if !strings.Contains(empty, "audience unset") || !strings.Contains(empty, "purpose unset") || !strings.Contains(empty, "targetLength unset") {
		t.Fatalf("empty brief evidence: %q", empty)
	}
	if strings.Contains(empty, "管理层") || strings.Contains(empty, "经营汇报") || strings.Contains(empty, "audience=管理层") {
		t.Fatalf("invented brief evidence: %q", empty)
	}
	if strings.Contains(empty, "机密") {
		t.Fatal("invented confidentiality")
	}
	got := FormatBriefEvidence(officestudio.Brief{Audience: "客户", Purpose: "方案汇报", TargetLength: 8, Confidentiality: "内部"})
	if !strings.Contains(got, "audience=客户") || !strings.Contains(got, "purpose=方案汇报") || !strings.Contains(got, "targetLength=8") || !strings.Contains(got, "confidentiality=内部") {
		t.Fatalf("authored brief evidence: %q", got)
	}
	if strings.Contains(got, "机密") {
		t.Fatal("invented stricter classification")
	}
}

func TestNormalizeBriefFillsDefaultsWithoutInventingFacts(t *testing.T) {
	got := NormalizeBrief(officestudio.Brief{})
	if got.Audience == "" || got.Language == "" || got.TargetLength == 0 {
		t.Fatalf("empty brief must get defaults: %#v", got)
	}
	if len(got.Facts) != 0 {
		t.Fatalf("defaults must not invent facts: %#v", got.Facts)
	}
	if got.Purpose == "" {
		t.Fatal("purpose default missing")
	}
	if got.Confidentiality != "" {
		t.Fatal("defaults must not invent confidentiality")
	}
}

func TestGenerateMergesCheckpointBriefFactsAndRejectsDrop(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	task.Checkpoint = json.RawMessage(`{"brief":{"facts":[{"factId":"sample_n","value":"42","unit":"份","locked":true,"locator":"样本数"}]}}`)
	if _, err := store.UpdateOfficeTask(ctx, task, task.Revision); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Generate(ctx, task.ID, "缺事实.docx", officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.DOCX, Title: "缺事实",
		Blocks: []officestudio.Block{{Type: "paragraph", Text: "正文未写入样本数"}},
	}, "brief-drop")
	if !errors.Is(err, officestudio.ErrFactConflict) {
		t.Fatalf("checkpoint locked fact dropped: %v", err)
	}
	v, err := svc.Generate(ctx, task.ID, "有事实.docx", officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.DOCX, Title: "有事实",
		Blocks: []officestudio.Block{{Type: "paragraph", Text: "样本数 42份"}},
	}, "brief-keep")
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	i, err := officestudio.Inspect(officestudio.DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(i.Preview, "42") {
		t.Fatal("brief fact missing from generated word")
	}
}

func TestGenerateRejectsBriefAndSpecFactConflict(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	task.Checkpoint = json.RawMessage(`{"brief":{"facts":[{"factId":"orders","value":"1280","unit":"单","locked":true}]}}`)
	if _, err := store.UpdateOfficeTask(ctx, task, task.Revision); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Generate(ctx, task.ID, "冲突.docx", officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.DOCX, Title: "冲突",
		Facts:  []officestudio.Fact{{FactID: "orders", Value: "999", Unit: "单", Locked: true}},
		Blocks: []officestudio.Block{{Type: "paragraph", Text: "订单 999单"}},
	}, "brief-conflict")
	if !errors.Is(err, officestudio.ErrFactConflict) {
		t.Fatalf("brief vs spec conflict: %v", err)
	}
}

func TestGenerateAppliesCheckpointBrandAndDesignOffFallsBack(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	task.Checkpoint = WithTaskBrand(task.Checkpoint, officestudio.BrandProfile{
		BrandID: "task-teal",
		Colors:  map[string]string{"navy": "112233"},
		Fonts:   officestudio.BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, officestudio.AssetRecord{
		SourceURL: "https://example.invalid/task-brand", License: "client-granted",
		Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Commercial: true,
	})
	if _, err := store.UpdateOfficeTask(ctx, task, task.Revision); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Generate(ctx, task.ID, "品牌.docx", officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.DOCX, Title: "品牌",
		Blocks: []officestudio.Block{{Type: "paragraph", Text: "订单 1280单"}},
	}, "task-brand-on")
	if err != nil {
		t.Fatal(err)
	}
	var spec officestudio.Spec
	if err = json.Unmarshal(v.Spec, &spec); err != nil || spec.BrandID != "task-teal" {
		t.Fatalf("checkpoint brand not applied: %s %v", v.Spec, err)
	}
	_, data, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(zipOfficePart(t, data, "word/document.xml"))
	if !strings.Contains(doc, `w:ascii="Georgia"`) || !strings.Contains(doc, `w:eastAsia="SimSun"`) || !strings.Contains(doc, "1280") {
		t.Fatalf("task brand fonts/facts missing: %s", doc[:min(400, len(doc))])
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := svc.Generate(ctx, task.ID, "关闭.docx", officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.DOCX, Title: "关闭",
		Blocks: []officestudio.Block{{Type: "paragraph", Text: "订单 1280单"}},
	}, "task-brand-off")
	if err != nil {
		t.Fatal(err)
	}
	_, offData, err := svc.ReadVersion(ctx, task.ID, off.ID)
	if err != nil {
		t.Fatal(err)
	}
	classic := string(zipOfficePart(t, offData, "word/document.xml"))
	if strings.Contains(classic, `w:ascii="Georgia"`) || strings.Contains(classic, "112233") {
		t.Fatal("design off leaked checkpoint brand")
	}
	if !strings.Contains(classic, "1280") {
		t.Fatal("design off dropped fact")
	}
}

func TestWithTaskBrandKeepsNavyAndLogoDigest(t *testing.T) {
	logo := strings.Repeat("cd", 32)
	raw := WithTaskBrand(nil, officestudio.BrandProfile{
		BrandID: "task-teal",
		Colors:  map[string]string{"navy": "112233"},
	}, officestudio.AssetRecord{
		SourceURL: "https://example.invalid/logo", License: "client-granted",
		Digest: strings.Repeat("ab", 32), LogoDigest: logo, Commercial: true,
	})
	brand, asset, ok := BrandFromCheckpoint(raw)
	if !ok || brand.Colors["navy"] != "112233" || asset.LogoDigest != logo {
		t.Fatalf("navy/logo digest dropped: %s", raw)
	}
	if strings.Contains(string(raw), "<a:blip") || strings.Contains(string(raw), "r:embed") {
		t.Fatal("logo digest must not stamp a layout image")
	}
}

func TestWithTaskBrandSurvivesStyleAndBrief(t *testing.T) {
	prev := WithTaskBrand(nil, officestudio.BrandProfile{BrandID: "task-teal", Fonts: officestudio.BrandFonts{Latin: "Georgia"}}, officestudio.AssetRecord{
		SourceURL: "https://example.invalid/keep", License: "client-granted",
		Digest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	})
	next := WithTaskStyle(WithTaskBrief(prev, officestudio.Brief{Audience: "客户"}), "brand-pitch")
	brand, asset, ok := BrandFromCheckpoint(next)
	if !ok || brand.BrandID != "task-teal" || brand.Fonts.Latin != "Georgia" || asset.License != "client-granted" {
		t.Fatalf("brand dropped: %s", next)
	}
}

func TestApplyTaskBriefWritesOutlinePurposeWithoutRewritingClaim(t *testing.T) {
	task := domain.Task{Checkpoint: WithTaskBrief(nil, officestudio.Brief{
		Outline: []officestudio.NarrativeNode{{Title: "指标", Purpose: "指标概览", Claim: "不得覆盖"}},
	})}
	got := applyTaskBrief(task, officestudio.Spec{
		SchemaVersion: 2, Kind: officestudio.PPTX, Title: "经营汇报",
		Slides: []officestudio.Slide{{
			Title: "指标", Layout: "metrics", Claim: "订单仍为 1280",
			Metrics: []officestudio.MetricBlock{{Label: "订单", Value: "1280", FactID: "orders"}},
		}},
	})
	if got.Slides[0].Purpose != "指标概览" {
		t.Fatalf("outline purpose: %q", got.Slides[0].Purpose)
	}
	if got.Slides[0].Claim != "订单仍为 1280" {
		t.Fatalf("claim rewritten: %q", got.Slides[0].Claim)
	}
}

func TestApplyTaskBriefCopiesPDFConfidentialityWithoutInventing(t *testing.T) {
	task := domain.Task{Checkpoint: WithTaskBrief(nil, officestudio.Brief{Confidentiality: "内部"})}
	got := applyTaskBrief(task, officestudio.Spec{Kind: officestudio.PDF, Title: "月报", Body: "订单 1280单"})
	if got.Confidentiality != "内部" {
		t.Fatalf("pdf confidentiality: %q", got.Confidentiality)
	}
	empty := applyTaskBrief(domain.Task{}, officestudio.Spec{Kind: officestudio.PDF, Title: "月报", Body: "订单 1280单"})
	if empty.Confidentiality != "" {
		t.Fatalf("invented pdf confidentiality: %q", empty.Confidentiality)
	}
}

func TestApplyTaskBriefCopiesPDFAudienceWithoutInventingDefaults(t *testing.T) {
	task := domain.Task{Checkpoint: WithTaskBrief(nil, officestudio.Brief{Audience: "客户", Purpose: "方案汇报"})}
	got := applyTaskBrief(task, officestudio.Spec{Kind: officestudio.PDF, Title: "月报", Body: "订单 1280单"})
	if got.Audience != "客户" || got.Purpose != "方案汇报" {
		t.Fatalf("pdf brief: audience=%q purpose=%q", got.Audience, got.Purpose)
	}
	empty := applyTaskBrief(domain.Task{}, officestudio.Spec{Kind: officestudio.PDF, Title: "月报", Body: "订单 1280单"})
	if empty.Audience != "" || empty.Purpose != "" {
		t.Fatalf("invented pdf brief: audience=%q purpose=%q", empty.Audience, empty.Purpose)
	}
}

func TestApplyTaskBriefWritesPPTAndExcelConfidentialityWithoutInventing(t *testing.T) {
	task := domain.Task{Checkpoint: WithTaskBrief(nil, officestudio.Brief{Confidentiality: "内部"})}
	ppt := applyTaskBrief(task, officestudio.Spec{
		Kind:   officestudio.PPTX,
		Title:  "月报",
		Slides: []officestudio.Slide{{Title: "指标", Notes: "阅读顺序 1"}},
	})
	if len(ppt.Slides) == 0 || !strings.Contains(ppt.Slides[0].Notes, "内部") {
		t.Fatalf("pptx notes missing authored confidentiality: %#v", ppt.Slides)
	}
	if strings.Contains(ppt.Slides[0].Notes, "机密") {
		t.Fatal("invented pptx confidentiality")
	}
	emptyPPT := applyTaskBrief(domain.Task{}, officestudio.Spec{
		Kind:   officestudio.PPTX,
		Title:  "月报",
		Slides: []officestudio.Slide{{Title: "指标", Notes: "阅读顺序 1"}},
	})
	if strings.Contains(emptyPPT.Slides[0].Notes, "机密") || strings.Contains(emptyPPT.Slides[0].Notes, "密级") {
		t.Fatalf("empty brief invented pptx confidentiality: %q", emptyPPT.Slides[0].Notes)
	}

	xlsx := applyTaskBrief(task, officestudio.Spec{
		Kind: officestudio.XLSX,
		Title: "经营簿",
		Sheets: []officestudio.Sheet{
			{Name: "说明", Rows: [][]officestudio.Cell{{{Type: "text", Value: "输入在原始数据"}}}},
			{Name: "原始数据", Rows: [][]officestudio.Cell{{{Type: "text", Value: "指标"}}}},
		},
	})
	if len(xlsx.Sheets) == 0 || len(xlsx.Sheets[0].Rows) == 0 || !strings.Contains(xlsx.Sheets[0].Rows[0][0].Value, "内部") {
		t.Fatalf("xlsx 说明 missing authored confidentiality: %#v", xlsx.Sheets)
	}
	if strings.Contains(xlsx.Sheets[1].Rows[0][0].Value, "机密") || strings.Contains(xlsx.Sheets[1].Rows[0][0].Value, "密级") {
		t.Fatalf("xlsx raw sheet invented confidentiality: %#v", xlsx.Sheets[1])
	}
	emptyXLSX := applyTaskBrief(domain.Task{}, officestudio.Spec{
		Kind: officestudio.XLSX,
		Title: "经营簿",
		Sheets: []officestudio.Sheet{
			{Name: "说明", Rows: [][]officestudio.Cell{{{Type: "text", Value: "输入在原始数据"}}}},
		},
	})
	if strings.Contains(emptyXLSX.Sheets[0].Rows[0][0].Value, "机密") || strings.Contains(emptyXLSX.Sheets[0].Rows[0][0].Value, "密级") {
		t.Fatalf("empty brief invented xlsx confidentiality: %q", emptyXLSX.Sheets[0].Rows[0][0].Value)
	}
}

func TestApplyTaskBriefWritesConfidentialityHeaderWithoutInventing(t *testing.T) {
	task := domain.Task{Checkpoint: WithTaskBrief(nil, officestudio.Brief{Confidentiality: "内部"})}
	got := applyTaskBrief(task, officestudio.Spec{
		Kind: officestudio.DOCX, Title: "研究报告",
		Document: &officestudio.DocumentOptions{Header: "研究报告"},
	})
	if got.Document == nil || !strings.Contains(got.Document.Header, "内部") {
		t.Fatalf("confidentiality not applied: %#v", got.Document)
	}
	if strings.Contains(got.Document.Header, "机密") {
		t.Fatal("invented confidentiality label")
	}
	empty := applyTaskBrief(domain.Task{}, officestudio.Spec{
		Kind: officestudio.DOCX, Title: "研究报告",
		Document: &officestudio.DocumentOptions{Header: "研究报告"},
	})
	if empty.Document.Header != "研究报告" {
		t.Fatalf("empty brief rewrote header: %q", empty.Document.Header)
	}
}

func TestWithTaskBriefPersistsAuthoredFieldsWithoutInventingDefaults(t *testing.T) {
	got := BriefFromCheckpoint(WithTaskBrief(nil, officestudio.Brief{Confidentiality: "内部"}))
	if got.Audience != "" || got.Purpose != "" || got.Language != "" || got.TargetLength != 0 || len(got.Deliverables) != 0 {
		t.Fatalf("persisted invented defaults: %#v", got)
	}
	if got.Confidentiality != "内部" {
		t.Fatalf("dropped confidentiality: %#v", got)
	}
	empty := BriefFromCheckpoint(WithTaskBrief(nil, officestudio.Brief{}))
	if empty.Audience == "管理层" || empty.Purpose == "经营汇报" || empty.TargetLength == 12 || empty.Confidentiality != "" {
		t.Fatalf("empty brief invented values: %#v", empty)
	}
}

func TestWithTaskBriefSurvivesStyleUpdate(t *testing.T) {
	prev := WithTaskBrief(nil, officestudio.Brief{
		Audience: "客户",
		Facts:    []officestudio.Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
	})
	next := WithTaskStyle(prev, "brand-pitch")
	brief := BriefFromCheckpoint(next)
	if brief.Audience != "客户" || len(brief.Facts) != 1 || brief.Facts[0].Value != "1280" {
		t.Fatalf("style update dropped brief: %s", next)
	}
	if StyleFromCheckpoint(next) != "brand-pitch" {
		t.Fatalf("style missing after brief write: %s", next)
	}
}
