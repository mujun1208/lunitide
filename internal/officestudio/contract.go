package officestudio

import (
	"errors"
	"strings"
)

const DefaultBrandID = "lunitide-classic"

var ErrFactConflict = errors.New("office: fact values conflict")

type Brief struct {
	Audience     string          `json:"audience,omitempty"`
	Purpose      string          `json:"purpose,omitempty"`
	Deliverables []Kind          `json:"deliverables,omitempty"`
	Language     string          `json:"language,omitempty"`
	TargetLength int             `json:"targetLength,omitempty"`
	Facts           []Fact          `json:"facts,omitempty"`
	Outline         []NarrativeNode `json:"outline,omitempty"`
	Confidentiality string          `json:"confidentiality,omitempty"`
}

type NarrativePlan struct {
	Nodes       []NarrativeNode `json:"nodes"`
	FitEvidence string          `json:"fitEvidence,omitempty"`
}

type Fact struct {
	FactID   string `json:"factId"`
	Value    string `json:"value"`
	Unit     string `json:"unit,omitempty"`
	Period   string `json:"period,omitempty"`
	SourceID string `json:"sourceId,omitempty"`
	Locator  string `json:"locator,omitempty"`
	Status   string `json:"status,omitempty"`
	Locked   bool   `json:"locked,omitempty"`
}

type BrandProfile struct {
	BrandID    string            `json:"brandId"`
	Version    string            `json:"version"`
	Colors     map[string]string `json:"colors,omitempty"`
	Fonts      BrandFonts        `json:"fonts"`
	ChartTheme []string          `json:"chartTheme,omitempty"`
	Logo       LogoRules         `json:"logo,omitempty"`
}

type BrandFonts struct {
	Latin      string `json:"latin,omitempty"`
	East       string `json:"east,omitempty"`
	TitlePt    int    `json:"titlePt,omitempty"`
	BodyPt     int    `json:"bodyPt,omitempty"`
	NotesPt    int    `json:"notesPt,omitempty"`
	WordBodyPt int    `json:"wordBodyPt,omitempty"`
	Fallback   string `json:"fallback,omitempty"`
}

type LogoRules struct {
	SafeInsetEMU int64  `json:"safeInsetEmu,omitempty"`
	EmbedPolicy  string `json:"embedPolicy,omitempty"`
}

type NarrativeNode struct {
	NodeID       string   `json:"nodeId"`
	Purpose      string   `json:"purpose,omitempty"`
	Claim        string   `json:"claim,omitempty"`
	EvidenceRefs []string `json:"evidenceRefs,omitempty"`
	Density      string   `json:"density,omitempty"`
	Layout       string   `json:"layout,omitempty"`
	Title        string   `json:"title,omitempty"`
	ImageAspect  string   `json:"imageAspect,omitempty"`
	Metrics      []MetricBlock
	Comparison   *ComparisonBlock
	Bullets      []string
}

type LayoutNode struct {
	VariantID    string `json:"variantId"`
	ReadingOrder int    `json:"readingOrder"`
	FitEvidence  string `json:"fitEvidence,omitempty"`
}

type QualityReport struct {
	Blockers      []Issue  `json:"blockers,omitempty"`
	Warnings      []Issue  `json:"warnings,omitempty"`
	Coverage      string   `json:"coverage,omitempty"`
	Score         int      `json:"score,omitempty"`
	RepairHistory []string `json:"repairHistory,omitempty"`
	FormalOK      bool     `json:"formalOk"`
}

type ComparisonBlock struct {
	Left  []string `json:"left,omitempty"`
	Right []string `json:"right,omitempty"`
}

type MetricBlock struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Unit   string `json:"unit,omitempty"`
	FactID string `json:"factId,omitempty"`
}

type EvidenceItem struct {
	Text   string `json:"text"`
	FactID string `json:"factId,omitempty"`
}

func DefaultBrand() BrandProfile {
	return BrandProfile{
		BrandID: DefaultBrandID,
		Version: "1",
		Colors: map[string]string{
			"navy": "0B1F3A", "teal": "0D9488", "gold": "C9A227",
			"paper": "F4F6F8", "ink": "1F2937", "muted": "64748B",
			"white": "FFFFFF", "soft": "E2E8F0",
			"emphasis": "0D9488", "risk": "B45309", "group": "E2E8F0",
		},
		Fonts: BrandFonts{
			Latin: "Calibri", East: "Microsoft YaHei",
			TitlePt: 28, BodyPt: 18, NotesPt: 11, WordBodyPt: 11,
			Fallback: "Calibri, Microsoft YaHei, Noto Sans SC",
		},
		ChartTheme: []string{"0D9488", "0B1F3A", "C9A227"},
		Logo:       LogoRules{SafeInsetEMU: 360000, EmbedPolicy: "do-not-embed-system"},
	}
}

func AdaptSpec(spec Spec) (Spec, error) {
	return AdaptSpecV1(spec)
}

func AdaptSpecV1(spec Spec) (Spec, error) {
	if spec.SchemaVersion != 0 && spec.SchemaVersion != 1 && spec.SchemaVersion != 2 {
		return Spec{}, ErrFormat
	}
	out := spec
	if strings.TrimSpace(out.BrandID) == "" {
		out.BrandID = DefaultBrandID
	}
	if out.SchemaVersion == 0 {
		out.SchemaVersion = 1
	}
	for i := range out.Slides {
		if out.Slides[i].Layout == "" && i == 0 {
			out.Slides[i].Layout = "cover"
		}
	}
	return out, nil
}

func ValidateFactSet(facts []Fact) error {
	type key struct{ id, period, unit string }
	seen := map[key]string{}
	for _, f := range facts {
		id := strings.TrimSpace(f.FactID)
		if id == "" || strings.TrimSpace(f.Value) == "" {
			return ErrFormat
		}
		if strings.EqualFold(strings.TrimSpace(f.Status), "conflict") {
			return ErrFactConflict
		}
		k := key{id, strings.TrimSpace(f.Period), strings.TrimSpace(f.Unit)}
		if prev, ok := seen[k]; ok && prev != f.Value {
			return ErrFactConflict
		}
		seen[k] = f.Value
	}
	return nil
}
