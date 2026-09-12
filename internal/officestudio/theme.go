package officestudio

import (
	"os"
	"strings"
	"sync"
)

var (
	brandMu    sync.RWMutex
	brandStore = map[string]BrandProfile{DefaultBrandID: DefaultBrand()}
)

func init() {
	for _, id := range []string{"ops-clear", "brand-pitch", "editorial-report"} {
		_ = RegisterBrand(StarterBrand(id))
	}
}

func StarterBrand(id string) BrandProfile {
	base := DefaultBrand()
	switch id {
	case "brand-pitch":
		base.BrandID = "brand-pitch"
		base.Colors = map[string]string{
			"navy": "1A0A2E", "teal": "7C3AED", "gold": "D4A017",
			"paper": "F5F0FF", "ink": "1F1233", "muted": "6B5B7A",
			"white": "FFFFFF", "soft": "EDE4F5",
		}
	case "editorial-report":
		base.BrandID = "editorial-report"
		base.Colors = map[string]string{
			"navy": "3F2E1E", "teal": "5B7C5A", "gold": "B08968",
			"paper": "F8F4EE", "ink": "2C1810", "muted": "7A6A58",
			"white": "FFFCF7", "soft": "EFE6D9",
		}
	default:
		base.BrandID = "ops-clear"
	}
	return base
}

func RegisterBrand(brand BrandProfile) error {
	if strings.TrimSpace(brand.BrandID) == "" {
		return ErrFormat
	}
	brandMu.Lock()
	brandStore[brand.BrandID] = brand
	brandMu.Unlock()
	return nil
}

func LookupBrand(id string) BrandProfile {
	brandMu.RLock()
	defer brandMu.RUnlock()
	if b, ok := brandStore[id]; ok {
		return b
	}
	return DefaultBrand()
}

func officeDesignEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_DESIGN")), "off")
}

func DesignScopeCheck() Check {
	if officeDesignEnabled() {
		return Check{ID: "design_system", Status: "passed", Message: "已启用设计系统品牌与布局规划"}
	}
	return Check{ID: "design_system", Status: "missing", Message: "设计系统已关闭，此检查按旧品牌与旧质量范围，不能当作新门槛已验证"}
}

func lookupExact(id string) (BrandProfile, bool) {
	brandMu.RLock()
	defer brandMu.RUnlock()
	b, ok := brandStore[id]
	return b, ok
}

func BrandForSpec(spec Spec) BrandProfile {
	return brandForSpec(spec)
}

func brandForSpec(spec Spec) BrandProfile {
	if !officeDesignEnabled() {
		return DefaultBrand()
	}
	if id := strings.TrimSpace(spec.BrandID); id != "" && id != DefaultBrandID {
		return LookupBrand(id)
	}
	if id := strings.TrimSpace(spec.TemplateID); id != "" {
		if b, ok := lookupExact(id); ok {
			return b
		}
	}
	return DefaultBrand()
}

type Theme struct {
	Navy, Teal, Gold, Paper, Ink, Muted, White, Soft string
	Emphasis, Risk, Group                            string
	Latin, East, Fallback                            string
	TitleSz, BodySz, NotesSz, WordBodySz             int
}

func ResolveTheme(brand BrandProfile) Theme {
	base := DefaultBrand()
	if brand.BrandID == "" {
		brand = base
	}
	color := func(key, fallback string) string {
		if brand.Colors != nil {
			if v := brand.Colors[key]; v != "" {
				return v
			}
		}
		if base.Colors != nil {
			if v := base.Colors[key]; v != "" {
				return v
			}
		}
		return fallback
	}
	latin, east := brand.Fonts.Latin, brand.Fonts.East
	if latin == "" {
		latin = base.Fonts.Latin
	}
	if east == "" {
		east = base.Fonts.East
	}
	titlePt, bodyPt, notesPt, wordBodyPt := brand.Fonts.TitlePt, brand.Fonts.BodyPt, brand.Fonts.NotesPt, brand.Fonts.WordBodyPt
	if titlePt == 0 {
		titlePt = base.Fonts.TitlePt
	}
	if bodyPt == 0 {
		bodyPt = base.Fonts.BodyPt
	}
	if notesPt == 0 {
		notesPt = base.Fonts.NotesPt
	}
	if wordBodyPt == 0 {
		wordBodyPt = base.Fonts.WordBodyPt
	}
	return Theme{
		Navy: color("navy", "0B1F3A"), Teal: color("teal", "0D9488"), Gold: color("gold", "C9A227"),
		Paper: color("paper", "F4F6F8"), Ink: color("ink", "1F2937"), Muted: color("muted", "64748B"),
		White: color("white", "FFFFFF"), Soft: color("soft", "E2E8F0"),
		Emphasis: color("emphasis", "0D9488"), Risk: color("risk", "B45309"), Group: color("group", "E2E8F0"),
		Latin: latin, East: east, Fallback: firstNonEmpty(brand.Fonts.Fallback, base.Fonts.Fallback),
		TitleSz: titlePt * 100, BodySz: bodyPt * 100, NotesSz: notesPt * 100,
		WordBodySz: wordBodyPt * 2,
	}
}

func FontEmbedPolicy(fonts BrandFonts) string {
	_ = fonts
	return "do-not-embed-system; fallback-is-name-list; Office 字体名为引用，不把 Microsoft YaHei 或系统字体打包分发"
}

func WithBrandLogoIssues(insp Inspection, brand BrandProfile) Inspection {
	if insp.Kind != PPTX {
		return insp
	}
	kept := make([]Issue, 0, len(insp.Issues))
	for _, issue := range insp.Issues {
		if issue.Code != "OFFICE_LOGO_SAFE_AREA" {
			kept = append(kept, issue)
		}
	}
	insp.Issues = append(kept, LogoSafeAreaIssues(insp.Nodes, brand)...)
	return insp
}

func LogoSafeAreaIssues(nodes []Node, brand BrandProfile) []Issue {
	inset := brand.Logo.SafeInsetEMU
	if inset <= 0 {
		inset = DefaultBrand().Logo.SafeInsetEMU
	}
	var issues []Issue
	for _, n := range nodes {
		if n.Image == nil {
			continue
		}
		if n.Image.X < inset || n.Image.Y < inset {
			issues = append(issues, Issue{
				Code: "OFFICE_LOGO_SAFE_AREA", Severity: "warning",
				Message: "图片进入品牌安全区，未改数字或正文。",
				Part:    n.Part, NodeID: n.ID,
			})
		}
	}
	return issues
}
