package officestudio

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/officerender"
)

type blindEvalSheet struct {
	Notice           string             `json:"notice"`
	Reviewed         int                `json:"reviewed"`
	Items            []json.RawMessage  `json:"items"`
	CompetitorScores map[string]float64 `json:"competitorScores"`
}

func TestOfficeQualityQAHarnessTwelveFixtures(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "office-quality")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var briefs []Brief
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "blind-eval.json" || e.Name() == "license-pack-checklist.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			t.Fatal(e.Name(), err)
		}
		var brief Brief
		if err := json.Unmarshal(raw, &brief); err != nil {
			t.Fatal(e.Name(), err)
		}
		briefs = append(briefs, brief)
		for _, kind := range brief.Deliverables {
			spec, err := qaSpecFromBrief(brief, kind)
			if err != nil {
				t.Fatalf("%s %s: %v", e.Name(), kind, err)
			}
			data, err := Generate(spec)
			if err != nil {
				t.Fatalf("%s generate %s: %v", e.Name(), kind, err)
			}
			i, err := Inspect(kind, data)
			if err != nil {
				t.Fatalf("%s inspect %s: %v", e.Name(), kind, err)
			}
			if kind == PDF {
				if _, err = Validate(PDF, data); err != nil {
					t.Fatalf("%s validate pdf: %v", e.Name(), err)
				}
				continue
			}
			for _, f := range brief.Facts {
				if !strings.Contains(i.Preview, f.Value) {
					found := false
					for _, n := range i.Nodes {
						if strings.Contains(n.Text, f.Value) {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("%s %s dropped fact %s=%s", e.Name(), kind, f.FactID, f.Value)
					}
				}
			}
		}
	}
	if len(briefs) < 12 {
		t.Fatalf("harness needs 12 fixtures, got %d", len(briefs))
	}
	raw, err := os.ReadFile(filepath.Join(root, "blind-eval.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sheet blindEvalSheet
	if err := json.Unmarshal(raw, &sheet); err != nil {
		t.Fatal(err)
	}
	if sheet.Reviewed != 0 || len(sheet.Items) != 0 || len(sheet.CompetitorScores) != 0 {
		t.Fatalf("blind eval must stay empty: %#v", sheet)
	}
	if strings.Contains(strings.ToLower(string(raw)), `"gamma"`) && sheet.Reviewed > 0 {
		t.Fatal("blind eval invented competitor scores")
	}
	native := (&officerender.Renderer{Root: t.TempDir()}).Probe(context.Background())
	_, desktopReady := officerender.DesktopApplicationsNotice(officerender.ProbeDesktopApplications())
	if !native.Available && !desktopReady {
		t.Log("native_render=missing; Word/WPS/LibreOffice not detected — skip live open checks")
		return
	}
	t.Logf("desktop present notice logged only; harness does not invent WPS/Office pass: libre=%v desktop=%v", native.Available, desktopReady)
}

func qaSpecFromBrief(brief Brief, kind Kind) (Spec, error) {
	facts := append([]Fact(nil), brief.Facts...)
	switch kind {
	case DOCX:
		id := "research-report"
		if brief.Purpose == "产品项目文档" {
			id = "product-project"
		} else if strings.Contains(brief.Purpose, "客户") || strings.Contains(brief.Purpose, "方案") {
			id = "client-proposal"
		}
		return WordReportSpec(id, brief.Purpose, facts)
	case XLSX:
		id := "ops-ledger"
		if strings.Contains(brief.Purpose, "销售") {
			id = "sales-pipeline"
		}
		return PlanWorkbook(id, brief.Purpose, facts)
	case PPTX:
		metrics := make([]MetricBlock, 0, len(facts))
		for _, f := range facts {
			metrics = append(metrics, MetricBlock{Label: f.Locator, Value: f.Value, Unit: f.Unit, FactID: f.FactID})
		}
		slide := Slide{
			Title: brief.Purpose, Layout: "metrics", Metrics: metrics,
			Bullets: []string{facts[0].Locator + " " + facts[0].Value + facts[0].Unit},
		}
		return Spec{
			SchemaVersion: 2, Kind: PPTX, Title: brief.Purpose, TemplateID: "ops-clear",
			Facts:  facts,
			Slides: []Slide{slide},
		}, nil
	case PDF:
		body := brief.Purpose
		for _, f := range facts {
			body += "\n" + f.Locator + " " + f.Value + f.Unit
		}
		return Spec{SchemaVersion: 2, Kind: PDF, Title: brief.Purpose, Body: body, Facts: facts}, nil
	default:
		return Spec{}, ErrFormat
	}
}
