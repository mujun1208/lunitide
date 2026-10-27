package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

// Kimi-style starter palettes: every advertised brandId must resolve to its
// own palette (not silently fall back to ops-clear), and the marketing list
// in the office.generate tool description must stay in sync with reality.
func TestStarterBrandsResolveDistinct(t *testing.T) {
	ids := []string{
		"ops-clear", "brand-pitch", "editorial-report",
		"consulting-blue", "tech-petrol", "finance-green",
		"gov-red", "startup-orange", "academic-slate",
	}
	seen := map[string]string{}
	for _, id := range ids {
		brand := StarterBrand(id)
		if brand.BrandID != id {
			t.Fatalf("StarterBrand(%q) returned brand %q", id, brand.BrandID)
		}
		registered := LookupBrand(id)
		if registered.BrandID != id {
			t.Fatalf("LookupBrand(%q) returned brand %q; starter not registered", id, registered.BrandID)
		}
		theme := ResolveTheme(brand)
		if theme.Navy == "" || theme.Ink == "" || theme.Paper == "" || theme.Teal == "" {
			t.Fatalf("brand %q missing core palette entries: %+v", id, theme)
		}
		if theme.Paper == theme.Ink {
			t.Fatalf("brand %q has unreadable contrast: paper=%s ink=%s", id, theme.Paper, theme.Ink)
		}
		key := theme.Navy + "|" + theme.Teal + "|" + theme.Gold
		if other, dup := seen[key]; dup {
			t.Fatalf("brands %q and %q share the same palette", other, id)
		}
		seen[key] = id
	}
}

func TestUnknownBrandFallsBackToDefault(t *testing.T) {
	if got := LookupBrand("no-such-brand"); got.BrandID != DefaultBrandID {
		t.Fatalf("unknown brand must fall back to the default, got %q", got.BrandID)
	}
}

// The design layer is generated from the brand: switching brandId must
// regenerate master, layouts, and theme consistently in one pass.
func TestBrandSwitchRegeneratesDesignLayer(t *testing.T) {
	masterFor := func(brandID string) []byte {
		t.Helper()
		spec := Spec{
			SchemaVersion: 1, Kind: PPTX, Title: "品牌设计层",
			BrandID: brandID,
			Slides: []Slide{
				{Title: "封面", Layout: "cover"},
				{Title: "内容", Layout: "content", Bullets: []string{"要点"}},
			},
		}
		data, err := Generate(spec)
		if err != nil {
			t.Fatalf("brand %q failed to generate: %v", brandID, err)
		}
		p, err := readPackage(data)
		if err != nil {
			t.Fatal(err)
		}
		master, ok := p.parts["ppt/slideMasters/slideMaster1.xml"]
		if !ok {
			t.Fatalf("brand %q deck missing master part", brandID)
		}
		return master
	}
	consulting := masterFor("consulting-blue")
	if !bytes.Contains(consulting, []byte(`val="F7F7F2"`)) {
		t.Fatalf("consulting-blue master must use its paper color: %.200s", consulting)
	}
	if !strings.Contains(string(consulting), `val="1D2733"`) {
		t.Fatalf("consulting-blue master must use its ink color: %.200s", consulting)
	}
	petrol := masterFor("tech-petrol")
	if !bytes.Contains(petrol, []byte(`val="F5F9FA"`)) {
		t.Fatalf("tech-petrol master must use its paper color: %.200s", petrol)
	}
	if bytes.Equal(consulting, petrol) {
		t.Fatal("switching brandId must regenerate the master design layer")
	}
}
