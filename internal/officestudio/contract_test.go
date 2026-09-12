package officestudio

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdaptSpecV1KeepsComparisonBullets(t *testing.T) {
	in := Spec{
		SchemaVersion: 1,
		Kind:          PPTX,
		Title:         "方案对比",
		Slides: []Slide{{
			Title:   "方案对比",
			Layout:  "comparison",
			Bullets: []string{"方案A", "成本低", "方案B", "能力高"},
		}},
	}
	got, err := AdaptSpec(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.BrandID != DefaultBrandID {
		t.Fatalf("brand=%q want default", got.BrandID)
	}
	if got.Slides[0].Layout != "comparison" {
		t.Fatalf("layout=%q", got.Slides[0].Layout)
	}
	if strings.Join(got.Slides[0].Bullets, ",") != "方案A,成本低,方案B,能力高" {
		t.Fatalf("bullets dropped: %#v", got.Slides[0].Bullets)
	}
}

func TestValidateFactSetRejectsSameIDDifferentValues(t *testing.T) {
	err := ValidateFactSet([]Fact{
		{FactID: "revenue", Value: "100", Unit: "万元", Period: "2026-Q1", SourceID: "sheet-a"},
		{FactID: "revenue", Value: "120", Unit: "万元", Period: "2026-Q1", SourceID: "sheet-b"},
	})
	if err == nil {
		t.Fatal("conflicting facts must not merge")
	}
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("err=%v want ErrFactConflict", err)
	}
}

func TestValidateFactSetRejectsExplicitConflictStatus(t *testing.T) {
	err := ValidateFactSet([]Fact{{FactID: "orders", Value: "1280", Unit: "单", Status: "conflict"}})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("explicit conflict status: %v", err)
	}
}

func TestValidateFactSetAcceptsSameValueAcrossSources(t *testing.T) {
	if err := ValidateFactSet([]Fact{
		{FactID: "orders", Value: "80", Unit: "单", Period: "2026-08", SourceID: "xlsx"},
		{FactID: "orders", Value: "80", Unit: "单", Period: "2026-08", SourceID: "brief"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOfficeQualityFixturesHaveFactIDsAndNoInventedSavings(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "office-quality")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	briefs := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "blind-eval.json" || e.Name() == "license-pack-checklist.json" {
			continue
		}
		briefs++
		raw, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "节约") || strings.Contains(string(raw), "供应商已通过") {
			t.Fatalf("%s invents savings or supplier pass", e.Name())
		}
		var brief Brief
		if err := json.Unmarshal(raw, &brief); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if strings.TrimSpace(brief.Purpose) == "" || len(brief.Facts) == 0 {
			t.Fatalf("%s missing purpose or facts", e.Name())
		}
		for _, f := range brief.Facts {
			if f.FactID == "" || f.Value == "" {
				t.Fatalf("%s fact missing id/value", e.Name())
			}
		}
		if err := ValidateFactSet(brief.Facts); err != nil {
			t.Fatalf("%s facts: %v", e.Name(), err)
		}
	}
	if briefs < 12 {
		t.Fatalf("need at least 12 brief fixtures, got %d", briefs)
	}
}
