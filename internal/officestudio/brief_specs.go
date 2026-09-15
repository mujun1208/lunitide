package officestudio

import "strings"

func SpecsFromBrief(brief Brief, brandID string) (map[Kind]Spec, error) {
	if err := ValidateFactSet(brief.Facts); err != nil {
		return nil, err
	}
	style := briefBrandStyle(brandID)
	title := firstNonEmpty(brief.Purpose, "经营对照")
	docx, err := WordDeliverySpec(style, title, brief.Facts)
	if err != nil {
		return nil, err
	}
	xlsx, err := PlanWorkbookProfile(style, title, brief.Facts)
	if err != nil {
		return nil, err
	}
	locked := lockFacts(brief.Facts)
	metrics := make([]MetricBlock, 0, len(locked))
	bullets := make([]string, 0, len(locked))
	for _, f := range locked {
		label := firstFactLabel([]Fact{f})
		metrics = append(metrics, MetricBlock{Label: label, Value: f.Value, Unit: f.Unit, FactID: f.FactID})
		bullets = append(bullets, strings.TrimSpace(label+" "+f.Value+f.Unit))
	}
	pptx := Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         title,
		BrandID:       brandID,
		TemplateID:    brandID,
		Audience:      brief.Audience,
		Purpose:       brief.Purpose,
		Facts:         locked,
		Slides: []Slide{
			{Title: title, Layout: "cover", Subtitle: brief.Audience},
			{Title: "指标", Layout: "metrics", Metrics: metrics},
			{Title: "口径", Layout: "content", Bullets: bullets},
		},
	}
	var body strings.Builder
	for _, f := range locked {
		body.WriteString(strings.TrimSpace(firstFactLabel([]Fact{f}) + " " + f.Value + f.Unit + " " + f.Period))
		body.WriteByte('\n')
	}
	pdf := Spec{
		SchemaVersion: 2,
		Kind:          PDF,
		Title:         title,
		BrandID:       brandID,
		Audience:      brief.Audience,
		Purpose:       brief.Purpose,
		Facts:         locked,
		Body:          strings.TrimSpace(body.String()),
	}
	stampBriefBrand(&docx, brief, brandID)
	stampBriefBrand(&xlsx, brief, brandID)
	docx.Blocks = append(docx.Blocks, factParagraphs(locked)...)
	return map[Kind]Spec{DOCX: docx, PPTX: pptx, XLSX: xlsx, PDF: pdf}, nil
}

func stampBriefBrand(spec *Spec, brief Brief, brandID string) {
	spec.BrandID = brandID
	spec.Audience = brief.Audience
	spec.Purpose = brief.Purpose
	if len(spec.Facts) == 0 {
		spec.Facts = lockFacts(brief.Facts)
	}
}

func briefBrandStyle(brandID string) string {
	switch strings.TrimSpace(brandID) {
	case "ops-clear":
		return "ops"
	case "brand-pitch":
		return "brand"
	case "editorial-report":
		return "editorial"
	default:
		return strings.TrimSpace(brandID)
	}
}
