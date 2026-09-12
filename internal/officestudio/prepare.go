package officestudio

import (
	"fmt"
	"strings"
)

func PrepareManagedSpec(spec Spec) (Spec, error) {
	out, err := AdaptSpec(spec)
	if err != nil {
		return Spec{}, err
	}
	if tid := strings.TrimSpace(out.TemplateID); tid != "" {
		if _, ok := LoadTemplate(tid); !ok {
			return Spec{}, ErrFormat
		}
		if !TemplateSupportsKind(tid, out.Kind) {
			out.TemplateID = ""
		}
	}
	if out.Kind == PPTX && strings.TrimSpace(out.TemplateID) == "" {
		out.TemplateID = "ops-clear"
	}
	if err = ValidateFactSet(factsFromSpec(out)); err != nil {
		return Spec{}, err
	}
	if out.Kind == XLSX && len(out.Sheets) == 0 {
		if _, ok := workbookLabel(out.TemplateID); ok {
			planned, planErr := PlanWorkbook(out.TemplateID, out.Title, factsFromSpec(out))
			if planErr != nil {
				return Spec{}, planErr
			}
			planned.BrandID = firstNonEmpty(out.BrandID, planned.BrandID)
			planned.SchemaVersion = out.SchemaVersion
			out = planned
		}
	}
	plan, planErr := PlanNarrative(out, Brief{Facts: out.Facts, Outline: nil})
	if planErr != nil {
		return Spec{}, planErr
	}
	out = ApplyNarrativePlan(out, plan)
	out, err = applyLayoutPlanning(out)
	if err != nil {
		return Spec{}, err
	}
	if err = rejectV2LayoutGuesses(out); err != nil {
		return Spec{}, err
	}
	return out, nil
}

func rejectV2LayoutGuesses(spec Spec) error {
	if spec.SchemaVersion != 2 || spec.Kind != PPTX {
		return nil
	}
	for _, s := range spec.Slides {
		switch normalizeSemanticLayout(s.Layout) {
		case "comparison":
			if s.Comparison == nil {
				return fmt.Errorf("%w: v2 comparison requires left/right blocks", ErrFormat)
			}
		case "metrics":
			if len(s.Metrics) == 0 {
				return fmt.Errorf("%w: v2 metrics requires Metric items", ErrFormat)
			}
		}
	}
	return nil
}

func keepSlideNarrative(s Slide) Slide {
	if claim := strings.TrimSpace(s.Claim); claim != "" && strings.TrimSpace(s.Subtitle) == "" && claim != strings.TrimSpace(s.Title) && !containsString(s.Bullets, claim) {
		s.Subtitle = claim
	}
	parts := []string{}
	if note := strings.TrimSpace(s.Notes); note != "" {
		parts = append(parts, note)
	}
	appendUnique := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		for _, part := range parts {
			if part == text || strings.Contains(part, text) {
				return
			}
		}
		parts = append(parts, text)
	}
	appendUnique(s.Purpose)
	for _, item := range s.Evidence {
		appendUnique(item.Text)
	}
	for _, ref := range s.EvidenceRefs {
		appendUnique("来源：" + ref)
	}
	if len(parts) > 0 {
		s.Notes = strings.Join(parts, "\n")
	}
	return s
}

func FactsFromSpec(spec Spec) []Fact {
	return factsFromSpec(spec)
}

func factsFromSpec(spec Spec) []Fact {
	var facts []Fact
	for _, f := range spec.Facts {
		if strings.TrimSpace(f.FactID) == "" || strings.TrimSpace(f.Value) == "" {
			continue
		}
		facts = append(facts, f)
	}
	for _, s := range spec.Slides {
		for _, m := range s.Metrics {
			if m.FactID == "" || m.Value == "" {
				continue
			}
			facts = append(facts, Fact{FactID: m.FactID, Value: m.Value, Unit: m.Unit, Locked: true})
		}
	}
	return facts
}

func applyLayoutPlanning(spec Spec) (Spec, error) {
	if spec.Kind != PPTX || len(spec.Slides) == 0 {
		return spec, nil
	}
	brand := brandForSpec(spec)
	slides := make([]Slide, 0, len(spec.Slides))
	for _, s := range spec.Slides {
		aspect := ""
		if len(s.Images) > 0 && s.Images[0].Width > 0 && s.Images[0].Height > 0 {
			aspect = fmt.Sprintf("%d/%d", s.Images[0].Width, s.Images[0].Height)
		}
		plans, err := PlanLayout(NarrativeNode{
			Title:        s.Title,
			Layout:       s.Layout,
			Purpose:      s.Purpose,
			Claim:        s.Claim,
			EvidenceRefs: s.EvidenceRefs,
			Metrics:      s.Metrics,
			Comparison:   s.Comparison,
			Bullets:      s.Bullets,
			ImageAspect:  aspect,
		}, brand, EstimateMeasure{}, spec.TemplateID)
		if err != nil {
			return Spec{}, err
		}
		for i, p := range plans {
			next := s
			if i > 0 {
				next.Images = nil
				next.Charts = nil
				next.Rows = nil
			}
			next.Title = p.Title
			next.Layout = p.Layout
			next.Metrics = p.Metrics
			if p.Bullets != nil {
				next.Bullets = p.Bullets
			}
			if p.Comparison != nil {
				next.Comparison = p.Comparison
			}
			if p.Notes != "" {
				if next.Notes != "" && !strings.Contains(next.Notes, p.Notes) {
					next.Notes = next.Notes + "\n" + p.Notes
				} else if next.Notes == "" {
					next.Notes = p.Notes
				}
			}
			if p.ReadingOrder > 0 {
				order := fmt.Sprintf("阅读顺序 %d", p.ReadingOrder)
				if !strings.Contains(next.Notes, order) {
					if next.Notes != "" {
						next.Notes += "\n" + order
					} else {
						next.Notes = order
					}
				}
			}
			slides = append(slides, keepSlideNarrative(next))
		}
	}
	spec.Slides = slides
	return spec, nil
}
