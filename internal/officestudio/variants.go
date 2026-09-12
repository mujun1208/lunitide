package officestudio

import (
	"fmt"
	"strings"
)

var semanticLayouts = []string{
	"cover", "section", "conclusion", "two-column", "comparison", "metrics",
	"trend", "structure", "process", "timeline", "evidence", "closing",
}

var designSystems = []string{"ops-clear", "brand-pitch", "editorial-report"}

var layoutDensities = []string{"compact", "standard", "airy"}

type LayoutVariant struct {
	VariantID        string
	System           string
	Layout           string
	Density          string
	MaxItems         int
	DesignerReviewed bool
}

func EngineeredVariants() []LayoutVariant {
	out := make([]LayoutVariant, 0, 36)
	for _, system := range designSystems {
		for _, layout := range semanticLayouts {
			out = append(out, LayoutVariant{
				VariantID: system + "/" + layout,
				System:    system,
				Layout:    layout,
				Density:   "standard",
				MaxItems:  densityItemLimit(layout, "standard"),
			})
		}
	}
	return out
}

func TemplateVariantCoverage() string {
	return fmt.Sprintf("templateVariants=%d engineered; designerReviewed=0", len(EngineeredVariants()))
}

func LookupVariant(id string) (LayoutVariant, bool) {
	system, layout, density := parseVariantID(id)
	if system == "" || layout == "" {
		return LayoutVariant{}, false
	}
	for _, v := range EngineeredVariants() {
		if v.System == system && v.Layout == layout {
			if density != "" {
				v.Density = density
				v.MaxItems = densityItemLimit(layout, density)
				v.VariantID = system + "/" + layout + "/" + density
			}
			return v, true
		}
	}
	return LayoutVariant{}, false
}

func parseVariantID(id string) (system, layout, density string) {
	parts := strings.Split(strings.TrimSpace(id), "/")
	if len(parts) == 2 {
		return parts[0], parts[1], ""
	}
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return "", "", ""
}

func pickDensity(node NarrativeNode, measure TextMeasure) string {
	d := strings.ToLower(strings.TrimSpace(node.Density))
	for _, allowed := range layoutDensities {
		if d == allowed {
			return d
		}
	}
	items := len(node.Metrics) + len(node.Bullets)
	titleRunes := 0
	if measure != nil {
		titleRunes = measure.Runes(node.Title)
	}
	if items >= 6 || titleRunes > 24 {
		return "compact"
	}
	if items <= 2 && titleRunes <= 12 {
		return "airy"
	}
	return "standard"
}

func densityItemLimit(layout, density string) int {
	base := layoutItemLimit(layout)
	switch density {
	case "airy":
		if base <= 6 {
			return 3
		}
		return 6
	case "standard":
		if base <= 6 {
			return 4
		}
		return 8
	default:
		return base
	}
}

