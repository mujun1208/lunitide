package officestudio

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestGenerateV2SplitsOverflowMetricsOnMainPath(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "经营指标",
		Slides: []Slide{{
			Title:  "本月经营指标对照以及需要向管理层单独说明的口径变化",
			Layout: "metrics",
			Metrics: []MetricBlock{
				{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
				{Label: "退款率", Value: "2.4", Unit: "%", FactID: "refund"},
				{Label: "在途", Value: "86", Unit: "单", FactID: "transit"},
				{Label: "客诉", Value: "11", Unit: "件", FactID: "tickets"},
				{Label: "复购", Value: "19", Unit: "%", FactID: "repeat"},
				{Label: "新品", Value: "7", Unit: "个", FactID: "newsku"},
				{Label: "缺货", Value: "3", Unit: "SKU", FactID: "stockout"},
			},
		}},
	}
	prepared, err := PrepareManagedSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Slides) < 2 {
		t.Fatalf("main path must plan overflow metrics, got %d slides", len(prepared.Slides))
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	findText(t, i, "3")
	if strings.Contains(i.Preview, "口径变") && !strings.Contains(i.Preview, "口径变化") {
		t.Fatal("title truncated on generate path")
	}
}

func TestGenerateSplitsOverflowTimelineInsteadOfFailing(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "时间线",
		Slides: []Slide{{
			Title:  "节点",
			Layout: "timeline",
			Bullets: []string{"启动", "核对", "复盘", "发布", "回访", "归档", "复盘纪要"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "启动")
	findText(t, i, "复盘纪要")
}

func TestPrepareRejectsExplicitConflictStatus(t *testing.T) {
	_, err := PrepareManagedSpec(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "冲突",
		Facts:  []Fact{{FactID: "orders", Value: "1280", Status: "conflict"}},
		Blocks: []Block{{Type: "paragraph", Text: "订单 1280"}},
	})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("conflict status: %v", err)
	}
}

func TestGenerateRejectsConflictingFactIDs(t *testing.T) {
	_, err := Generate(Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "冲突",
		Slides: []Slide{
			{Title: "A", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}},
			{Title: "B", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "999", Unit: "单", FactID: "orders"}}},
		},
	})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("conflicting facts: %v", err)
	}
}

func TestGenerateAppliesRegisteredBrandColorsAndDesignOffFallsBack(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "client-teal",
		Colors:  map[string]string{"navy": "112233", "teal": "AABBCC"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/brand", License: "client-granted",
		Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "品牌色", BrandID: brand.BrandID,
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	slide := parts["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide, []byte("112233")) {
		t.Fatalf("registered brand color missing: %s", slide[:min(400, len(slide))])
	}
	if !bytes.Contains(slide, []byte(">1280<")) {
		t.Fatal("brand apply rewrote fact")
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	classic := zipParts(t, off)["ppt/slides/slide1.xml"]
	if bytes.Contains(classic, []byte("112233")) {
		t.Fatal("design off must not leak imported brand colors")
	}
	if !bytes.Contains(classic, []byte("0B1F3A")) {
		t.Fatal("design off must use classic navy")
	}
}

func TestGenerateAppliesRegisteredBrandFontsAndDesignOffFallsBack(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "client-serif",
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/fonts", License: "client-granted",
		Digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "品牌字体", BrandID: brand.BrandID,
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	slide := zipParts(t, data)["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide, []byte(`typeface="Georgia"`)) || !bytes.Contains(slide, []byte(`typeface="SimSun"`)) {
		t.Fatalf("registered brand fonts missing: %s", slide[:min(500, len(slide))])
	}
	if !bytes.Contains(slide, []byte(">1280<")) {
		t.Fatal("font apply rewrote fact")
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	classic := zipParts(t, off)["ppt/slides/slide1.xml"]
	if bytes.Contains(classic, []byte(`typeface="Georgia"`)) || bytes.Contains(classic, []byte(`typeface="SimSun"`)) {
		t.Fatal("design off must not leak imported brand fonts")
	}
	if !bytes.Contains(classic, []byte(`typeface="Calibri"`)) || !bytes.Contains(classic, []byte(`typeface="Microsoft YaHei"`)) {
		t.Fatal("design off must use classic fonts")
	}
}

func TestGenerateCoverKeepsV2Metrics(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "封面指标",
		Slides: []Slide{{
			Title:   "经营回顾",
			Layout:  "cover",
			Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
}

func TestGenerateAppliesStarterTemplateBrandFromTemplateID(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "品牌方案", TemplateID: "brand-pitch",
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	slide := zipParts(t, data)["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide, []byte(StarterBrand("brand-pitch").Colors["navy"])) {
		t.Fatalf("template brand missing: %s", slide[:min(400, len(slide))])
	}
	if !bytes.Contains(slide, []byte(">1280<")) {
		t.Fatal("template brand rewrote fact")
	}
	if _, err = Generate(Spec{SchemaVersion: 2, Kind: PPTX, Title: "未知模板", TemplateID: "not-a-template", Slides: []Slide{{Title: "页"}}}); err == nil {
		t.Fatal("unknown template accepted")
	}
}

func TestGenerateAppliesRegisteredBrandToPptThemePart(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "theme-serif",
		Colors:  map[string]string{"navy": "112233"},
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/theme", License: "client-granted",
		Digest: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "母版品牌", BrandID: brand.BrandID,
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	theme := zipParts(t, data)["ppt/theme/theme1.xml"]
	if !bytes.Contains(theme, []byte(`typeface="Georgia"`)) || !bytes.Contains(theme, []byte(`typeface="SimSun"`)) {
		t.Fatalf("theme fonts missing: %s", theme[:min(400, len(theme))])
	}
	if !bytes.Contains(theme, []byte("112233")) {
		t.Fatalf("theme navy missing: %s", theme[:min(400, len(theme))])
	}
	if bytes.Contains(theme, []byte("Calibri Light")) {
		t.Fatal("classic master font leaked into branded theme")
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "母版品牌", BrandID: brand.BrandID,
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	classic := zipParts(t, off)["ppt/theme/theme1.xml"]
	if bytes.Contains(classic, []byte(`typeface="Georgia"`)) || bytes.Contains(classic, []byte("112233")) {
		t.Fatal("design off leaked branded theme")
	}
}

func TestPrepareManagedSpecClearsKindMismatchedTemplate(t *testing.T) {
	word, err := PrepareManagedSpec(Spec{
		SchemaVersion: 1, Kind: DOCX, Title: "说明", TemplateID: "brand-pitch",
		Blocks: []Block{{Type: "paragraph", Text: "正文"}},
	})
	if err != nil {
		t.Fatalf("known ppt template on word must not fail generate prep: %v", err)
	}
	if word.TemplateID != "" {
		t.Fatalf("kind-mismatched template kept: %q", word.TemplateID)
	}
	xlsx, err := PrepareManagedSpec(Spec{
		SchemaVersion: 1, Kind: XLSX, Title: "表", TemplateID: "brand-pitch",
		Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{{Type: "text", Value: "a"}}}}},
	})
	if err != nil {
		t.Fatalf("known ppt template on excel must not fail generate prep: %v", err)
	}
	if xlsx.TemplateID != "" {
		t.Fatalf("excel kept ppt template: %q", xlsx.TemplateID)
	}
}
