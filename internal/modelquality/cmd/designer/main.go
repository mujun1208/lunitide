package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/officestudio"
)

const (
	system  = "ops-clear"
	density = "standard"
)

var semanticLayouts = []string{
	"cover", "section", "conclusion", "two-column", "comparison", "metrics",
	"trend", "structure", "process", "timeline", "evidence", "closing",
}

type comboEvidence struct {
	VariantID  string `json:"variantId"`
	Layout     string `json:"layout"`
	SHA256     string `json:"sha256,omitempty"`
	GenerateOK bool   `json:"generateOk"`
	Error      string `json:"error,omitempty"`
	File       string `json:"file,omitempty"`
}

type designerEvidence struct {
	SchemaVersion    int             `json:"schemaVersion"`
	CheckedAt        string          `json:"checkedAt"`
	DesignerReviewed int             `json:"designerReviewed"`
	Certified        bool            `json:"certified"`
	LayersD          string          `json:"layersD"`
	System           string          `json:"system"`
	Density          string          `json:"density"`
	GenerateMethod   string          `json:"generateMethod"`
	LiveLLM          bool            `json:"liveLLM"`
	VisualScore      string          `json:"visualScore"`
	Combos           []comboEvidence `json:"combos"`
	GeneratedOK      int             `json:"generatedOk"`
	GeneratedTotal   int             `json:"generatedTotal"`
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	outDir := filepath.Join(root, "docs", "audits", "model-office-upgrade", "designer-samples")
	evidencePath := filepath.Join(root, "docs", "audits", "model-office-upgrade", "d-designer-evidence.json")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	brand := officestudio.LookupBrand(system)
	evidence := designerEvidence{
		SchemaVersion:    1,
		CheckedAt:        time.Now().UTC().Format(time.RFC3339),
		DesignerReviewed: 0,
		Certified:        false,
		LayersD:          "not_run",
		System:           system,
		Density:          density,
		GenerateMethod:   "officestudio.Generate",
		LiveLLM:          false,
		VisualScore:      "uncalibrated",
		GeneratedTotal:   len(semanticLayouts),
	}

	for _, layout := range semanticLayouts {
		variantID := system + "/" + layout + "/" + density
		entry := comboEvidence{VariantID: variantID, Layout: layout}
		marker := "布局标记-" + layout
		node := officestudio.NarrativeNode{
			Title:   layout + " 示例",
			Layout:  layout,
			Density: density,
			Bullets: []string{marker + " 要点"},
		}
		if layout == "metrics" || layout == "trend" {
			node.Metrics = []officestudio.MetricBlock{
				{Label: "订单", Value: "1280", Unit: "单", FactID: "orders-" + layout},
			}
		}
		if layout == "comparison" {
			node.Comparison = &officestudio.ComparisonBlock{
				Left:  []string{marker},
				Right: []string{"对照"},
			}
		}
		plans, planErr := officestudio.PlanLayout(node, brand, officestudio.RuneMeasure{}, system)
		if planErr != nil {
			entry.Error = planErr.Error()
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		if len(plans) == 0 {
			entry.Error = "layout produced no page"
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		if plans[0].VariantID != variantID {
			entry.Error = fmt.Sprintf("variant mismatch: got %s want %s", plans[0].VariantID, variantID)
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		spec, specErr := officestudio.SpecFromLayoutPlans(system+" 核心组合", plans)
		if specErr != nil {
			entry.Error = specErr.Error()
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		spec.TemplateID = system
		spec.BrandID = system
		data, genErr := officestudio.Generate(spec)
		if genErr != nil {
			entry.Error = genErr.Error()
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		insp, inspErr := officestudio.Inspect(officestudio.PPTX, data)
		if inspErr != nil {
			entry.Error = inspErr.Error()
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		name := strings.ReplaceAll(variantID, "/", "_") + ".pptx"
		filePath := filepath.Join(outDir, name)
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			entry.Error = err.Error()
			evidence.Combos = append(evidence.Combos, entry)
			continue
		}
		entry.GenerateOK = true
		entry.SHA256 = insp.SHA256
		entry.File = filepath.ToSlash(filepath.Join("docs", "audits", "model-office-upgrade", "designer-samples", name))
		evidence.Combos = append(evidence.Combos, entry)
		evidence.GeneratedOK++
	}

	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(evidencePath, raw, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("generatedOk=%d total=%d evidence=%s\n", evidence.GeneratedOK, evidence.GeneratedTotal, evidencePath)
	if evidence.GeneratedOK != evidence.GeneratedTotal {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
