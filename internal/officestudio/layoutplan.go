package officestudio

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"unicode/utf8"
)

//go:embed templates/*.json
var templateFS embed.FS

func TemplateIDs() []string {
	entries, err := fs.Glob(templateFS, "templates/*.json")
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, name := range entries {
		raw, err := templateFS.ReadFile(name)
		if err != nil {
			continue
		}
		var m struct {
			TemplateID string `json:"templateId"`
		}
		if json.Unmarshal(raw, &m) == nil && strings.TrimSpace(m.TemplateID) != "" {
			ids = append(ids, m.TemplateID)
		}
	}
	return ids
}

const maxMetricsPerSlide = 6

type TemplateManifest struct {
	TemplateID     string   `json:"templateId"`
	Version        string   `json:"version"`
	License        string   `json:"license"`
	SupportedKinds []string `json:"supportedKinds"`
	Slots          []string `json:"slots"`
	Constraints    struct {
		MaxMetrics int    `json:"maxMetrics"`
		Canvas     string `json:"canvas"`
		Page       string `json:"page"`
	} `json:"constraints"`
}

func TemplateSupportsKind(id string, kind Kind) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	switch kind {
	case DOCX:
		_, _, ok := wordTemplateChrome(id)
		return ok
	case XLSX:
		_, ok := workbookLabel(id)
		return ok
	case PPTX:
		if _, _, word := wordTemplateChrome(id); word {
			return false
		}
		if _, excel := workbookLabel(id); excel {
			return false
		}
		_, ok := LoadTemplate(id)
		return ok
	default:
		return false
	}
}

func LoadTemplate(id string) (TemplateManifest, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TemplateManifest{}, false
	}
	entries, err := fs.Glob(templateFS, "templates/*.json")
	if err != nil {
		return TemplateManifest{}, false
	}
	for _, name := range entries {
		raw, err := templateFS.ReadFile(name)
		if err != nil {
			continue
		}
		var m TemplateManifest
		if json.Unmarshal(raw, &m) == nil && m.TemplateID == id {
			return m, true
		}
	}
	return TemplateManifest{}, false
}

type LayoutPlan struct {
	VariantID    string
	Title        string
	Layout       string
	Metrics      []MetricBlock
	Comparison   *ComparisonBlock
	Bullets      []string
	FitEvidence  string
	Notes        string
	ReadingOrder int
}

type TextMeasure interface {
	Runes(string) int
}

type RuneMeasure struct{}

func (RuneMeasure) Runes(s string) int { return utf8.RuneCountInString(s) }

type EstimateMeasure struct{}

func (EstimateMeasure) Runes(s string) int {
	width := 0
	for _, r := range s {
		if r <= 0x7F {
			width++
			continue
		}
		width += 2
	}
	return width
}

func measureLabel(measure TextMeasure) string {
	if g, ok := measure.(GlyphMeasure); ok && g.Extent != nil {
		if _, ready := g.Extent("国", g.FontFamily); ready {
			return "measure=glyph; font=" + g.FontFamily
		}
	}
	if _, ok := measure.(EstimateMeasure); ok {
		return "measure=cjk-ascii-estimate; not glyph"
	}
	return "measure=rune-count; not glyph"
}

func imageFitNote(aspect string) string {
	if strings.TrimSpace(aspect) == "" {
		return "no-image; pure-layout"
	}
	return "imageAspect=" + strings.TrimSpace(aspect) + "; imageFit=contain; not stretch"
}

func PlanLayout(node NarrativeNode, brand BrandProfile, measure TextMeasure, templateID ...string) ([]LayoutPlan, error) {
	if measure == nil {
		measure = RuneMeasure{}
	}
	layout := node.Layout
	if layout == "" {
		layout = "content"
	}
	tid := ""
	if len(templateID) > 0 {
		tid = strings.TrimSpace(templateID[0])
	}
	if tid == "" {
		tid = "ops-clear"
	}
	if strings.TrimSpace(brand.BrandID) == "" {
		brand = DefaultBrand()
	}
	density := pickDensity(node, measure)
	limit := densityItemLimit(layout, density)
	if m, ok := LoadTemplate(tid); ok && m.Constraints.MaxMetrics > 0 && m.Constraints.MaxMetrics < limit {
		limit = m.Constraints.MaxMetrics
	}
	variant := tid + "/" + layout + "/" + density
	metrics := append([]MetricBlock(nil), node.Metrics...)
	if len(metrics) > limit {
		var out []LayoutPlan
		for i := 0; i < len(metrics); i += limit {
			end := i + limit
			if end > len(metrics) {
				end = len(metrics)
			}
			title := node.Title
			if i > 0 {
				title = node.Title + "（续）"
			}
			out = append(out, LayoutPlan{
				VariantID:   variant,
				Title:       title,
				Layout:      layout,
				Metrics:     metrics[i:end],
				FitEvidence: fmt.Sprintf("split metrics %d-%d of %d; title runes=%d; template=%s; %s; %s", i+1, end, len(metrics), measure.Runes(title), tid, measureLabel(measure), imageFitNote(node.ImageAspect)),
			})
		}
		if len(node.Bullets) > 0 {
			rest, err := PlanLayout(NarrativeNode{Title: node.Title, Layout: "content", Bullets: node.Bullets}, brand, measure, tid)
			if err != nil {
				return nil, err
			}
			out = append(out, rest...)
		}
		return finalizeLayoutPlans(out, node), nil
	}
	bullets := splitOverlongBullets(node.Bullets, 300)
	itemLimit := layoutItemLimit(layout)
	if len(bullets) > itemLimit {
		var out []LayoutPlan
		for i := 0; i < len(bullets); i += itemLimit {
			end := i + itemLimit
			if end > len(bullets) {
				end = len(bullets)
			}
			title := node.Title
			if i > 0 {
				title = node.Title + "（续）"
			}
			out = append(out, LayoutPlan{
				VariantID:   variant,
				Title:       title,
				Layout:      layout,
				Metrics:     metrics,
				Comparison:  node.Comparison,
				Bullets:     append([]string(nil), bullets[i:end]...),
				FitEvidence: fmt.Sprintf("split %s items %d-%d of %d; title runes=%d; template=%s; %s; %s", layout, i+1, end, len(bullets), measure.Runes(title), tid, measureLabel(measure), imageFitNote(node.ImageAspect)),
			})
		}
		return finalizeLayoutPlans(out, node), nil
	}
	return finalizeLayoutPlans([]LayoutPlan{{
		VariantID:   variant,
		Title:       node.Title,
		Layout:      layout,
		Metrics:     metrics,
		Comparison:  node.Comparison,
		Bullets:     bullets,
		FitEvidence: fmt.Sprintf("single %s; title runes=%d; metrics=%d; bullets=%d; template=%s; %s; %s", layout, measure.Runes(node.Title), len(metrics), len(bullets), tid, measureLabel(measure), imageFitNote(node.ImageAspect)),
	}}, node), nil
}

func finalizeLayoutPlans(plans []LayoutPlan, node NarrativeNode) []LayoutPlan {
	notes := evidenceNotes(node)
	for i := range plans {
		plans[i].ReadingOrder = i + 1
		if notes != "" && !strings.Contains(plans[i].Notes, notes) {
			if plans[i].Notes != "" {
				plans[i].Notes += "\n" + notes
			} else {
				plans[i].Notes = notes
			}
		}
		if !strings.Contains(plans[i].FitEvidence, "readingOrder=") {
			plans[i].FitEvidence += fmt.Sprintf("; readingOrder=%d", plans[i].ReadingOrder)
		}
	}
	return plans
}

func evidenceNotes(node NarrativeNode) string {
	refs := uniqueNonEmpty(node.EvidenceRefs)
	if len(refs) == 0 {
		return ""
	}
	return "来源：" + strings.Join(refs, "；")
}

func layoutItemLimit(layout string) int {
	switch normalizeSemanticLayout(layout) {
	case "timeline", "metrics":
		return maxMetricsPerSlide
	default:
		return 12
	}
}

func splitOverlongBullets(items []string, maxRunes int) []string {
	if maxRunes < 1 {
		maxRunes = 300
	}
	var out []string
	for _, item := range items {
		runes := []rune(item)
		if len(runes) <= maxRunes {
			out = append(out, item)
			continue
		}
		for len(runes) > 0 {
			n := maxRunes
			if n > len(runes) {
				n = len(runes)
			}
			if n < len(runes) {
				if cut := lastBreak(runes[:n]); cut > maxRunes/2 {
					n = cut
				}
			}
			out = append(out, string(runes[:n]))
			runes = runes[n:]
		}
	}
	return out
}

func lastBreak(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		switch runes[i] {
		case '。', '！', '？', '；', '\n', '.', ';':
			return i + 1
		}
	}
	return len(runes)
}

func SpecFromLayoutPlans(title string, plans []LayoutPlan) (Spec, error) {
	if strings.TrimSpace(title) == "" || len(plans) == 0 {
		return Spec{}, ErrFormat
	}
	spec := Spec{SchemaVersion: 2, Kind: PPTX, Title: title, BrandID: DefaultBrandID}
	for _, p := range plans {
		spec.Slides = append(spec.Slides, Slide{
			Title:      p.Title,
			Layout:     normalizeSemanticLayout(p.Layout),
			Metrics:    p.Metrics,
			Comparison: p.Comparison,
			Bullets:    p.Bullets,
			Notes:      p.Notes,
			Purpose:    p.FitEvidence,
		})
	}
	return spec, nil
}

func normalizeSemanticLayout(layout string) string {
	switch layout {
	case "conclusion":
		return "closing"
	case "trend", "process", "structure", "evidence":
		if layout == "trend" {
			return "metrics"
		}
		return "content"
	default:
		return layout
	}
}
