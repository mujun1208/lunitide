package officestudio

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLayoutFitPreservesLockedFacts(t *testing.T) {
	title := "这是一个需要自动换行同时保持清晰阅读层级的企业数字化转型方案对比标题示例"
	if utf8.RuneCountInString(title) != 36 {
		t.Fatalf("P02 title fixture changed: %d", utf8.RuneCountInString(title))
	}
	left, right := make([]string, 12), make([]string, 12)
	for i := 0; i < 12; i++ {
		n := strconv.Itoa(i + 1)
		left[i] = "方案A第" + n + "项详细说明"
		right[i] = "方案B第" + n + "项不同策略"
	}
	left[0] += " 1280万"
	spec := Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "超量内容适配",
		BrandID:       "brand-pitch",
		TemplateID:    "brand-pitch",
		Facts:         []Fact{{FactID: "capex", Value: "1280", Unit: "万", Locked: true}},
		Slides: []Slide{{
			Title:      title,
			Layout:     "comparison",
			Comparison: &ComparisonBlock{Left: left, Right: right},
		}},
	}
	prepared, err := PrepareManagedSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Slides) == 0 || prepared.Slides[0].LayoutTrace.FitEvidence == "" {
		t.Fatal("prepare must write fitEvidence onto slides")
	}
	if prepared.Slides[0].LayoutTrace.ResolvedVariant == "" || prepared.Slides[0].LayoutTrace.Density == "" {
		t.Fatalf("prepare dropped variant/density: %+v", prepared.Slides[0].LayoutTrace)
	}
	gen := prepared
	gen.Facts = nil
	data, err := Generate(gen)
	if err != nil {
		t.Fatal(err)
	}
	_, qa, err := BoundedRepair(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(qa.RepairHistory) == 0 {
		t.Fatal("repair history required")
	}
	if MaxGeometryRepairRounds > 2 {
		t.Fatalf("repair budget drifted: %d", MaxGeometryRepairRounds)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	blob := i.Preview
	for _, body := range zipParts(t, data) {
		blob += string(body)
	}
	if !strings.Contains(blob, "1280") {
		t.Fatal("locked fact 1280 missing")
	}
	if !strings.Contains(blob, title) {
		t.Fatal("P02 title truncated")
	}
	for n := 1; n <= 12; n++ {
		if !strings.Contains(blob, "方案A第"+strconv.Itoa(n)+"项详细说明") || !strings.Contains(blob, "方案B第"+strconv.Itoa(n)+"项不同策略") {
			t.Fatalf("comparison item %d dropped", n)
		}
	}
	brand := BrandForSpec(prepared)
	minTitle, minBody := brand.Fonts.TitlePt*100, brand.Fonts.BodyPt*100
	szRe := regexp.MustCompile(`sz="(\d+)"`)
	foundTitle, foundBody := false, false
	for name, body := range zipParts(t, data) {
		if !strings.Contains(name, "/slides/slide") || strings.Contains(name, "/_rels/") || strings.Contains(name, "notes") {
			continue
		}
		if !szRe.Match(body) {
			t.Fatalf("%s missing font size", name)
		}
		for _, m := range szRe.FindAllSubmatch(body, -1) {
			sz, _ := strconv.Atoi(string(m[1]))
			if sz >= minTitle {
				foundTitle = true
			}
			if sz >= minBody {
				foundBody = true
			}
		}
	}
	if !foundTitle || !foundBody {
		t.Fatalf("brand title/body sizes missing title=%v body=%v mins=%d/%d", foundTitle, foundBody, minTitle, minBody)
	}
}
