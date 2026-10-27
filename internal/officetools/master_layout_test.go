package officetools

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// The design layer (master, layouts, theme) must regenerate from one theme
// source: changing the brand must change every design part consistently.

func TestStudioMasterAndLayoutsCarryBrandTheme(t *testing.T) {
	theme := ClassicSlideTheme()
	theme.Paper = "F7F7F2"
	theme.Ink = "1D2733"
	theme.Muted = "6C7A89"
	theme.Latin = "Georgia"
	theme.East = "SimSun"
	theme.TitleSz = 3600
	theme.BodySz = 1700
	data, err := GenStudioPptxThemed("品牌母版", []SlideSpec{
		{Title: "封面", Layout: "cover"},
		{Title: "内容", Layout: "content", Bullets: []string{"一"}},
		{Title: "结束", Layout: "closing"},
	}, nil, theme)
	if err != nil {
		t.Fatal(err)
	}
	master := zipPartBody(t, data, "ppt/slideMasters/slideMaster1.xml")
	if !strings.Contains(master, `val="F7F7F2"`) {
		t.Fatalf("master background is not the brand paper: %.200s", master)
	}
	if !strings.Contains(master, `val="1D2733"`) || !strings.Contains(master, `val="6C7A89"`) {
		t.Fatalf("master text styles are not brand ink/muted: %.200s", master)
	}
	if !strings.Contains(master, `typeface="Georgia"`) || !strings.Contains(master, `typeface="SimSun"`) {
		t.Fatalf("master styles are not brand fonts: %.200s", master)
	}
	if got := strings.Count(master, "<p:sldLayoutId "); got != len(studioLayoutDefs) {
		t.Fatalf("master lists %d layouts, want %d", got, len(studioLayoutDefs))
	}
	masterRels := zipPartBody(t, data, "ppt/slideMasters/_rels/slideMaster1.xml.rels")
	if got := strings.Count(masterRels, "slideLayouts/slideLayout"); got != len(studioLayoutDefs) {
		t.Fatalf("master rels reference %d layouts, want %d", got, len(studioLayoutDefs))
	}
	for i := 1; i <= len(studioLayoutDefs); i++ {
		if !strings.Contains(masterRels, fmt.Sprintf("slideLayout%d.xml", i)) {
			t.Fatalf("master rels missing layout %d", i)
		}
	}
	if !strings.Contains(masterRels, "theme/theme1.xml") {
		t.Fatal("master rels must reference the theme")
	}
	types := zipPartBody(t, data, "[Content_Types].xml")
	if got := strings.Count(types, "slideLayout+xml"); got != len(studioLayoutDefs) {
		t.Fatalf("content types declare %d layouts, want %d", got, len(studioLayoutDefs))
	}
}

func TestStudioSlidesPointAtSemanticLayoutParts(t *testing.T) {
	data, err := GenStudioPptxThemed("版式引用", []SlideSpec{
		{Title: "封面", Layout: "cover"},
		{Title: "章节", Layout: "section"},
		{Title: "两栏", Layout: "two-column", Bullets: []string{"左"}},
		{Title: "结论", Layout: "conclusion"},
	}, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	for i, layout := range []string{"cover", "section", "two-column", "conclusion"} {
		rels := zipPartBody(t, data, fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1))
		want := fmt.Sprintf("slideLayout%d.xml", StudioLayoutIndex(layout))
		if !strings.Contains(rels, want) {
			t.Fatalf("%s slide must use %s: %s", layout, want, rels)
		}
		idx := strings.Index(rels, "slideLayouts/")
		if idx < 0 {
			t.Fatalf("slide rels missing layout target: %s", rels)
		}
		target := strings.SplitN(rels[idx:], "\"", 2)[0]
		body := zipPartBody(t, data, "ppt/"+target)
		if !strings.Contains(body, `<p:ph type="title"/>`) {
			t.Fatalf("layout %s lacks a title placeholder: %s", target, body)
		}
		if !strings.Contains(body, `<p:ph type="body" idx="1"/>`) {
			t.Fatalf("layout %s lacks a body placeholder: %s", target, body)
		}
	}
}

func TestStudioThemeChangeRegeneratesDesignLayer(t *testing.T) {
	slides := []SlideSpec{{Title: "封面", Layout: "cover"}, {Title: "内容", Layout: "content", Bullets: []string{"一"}}}
	base, err := GenStudioPptxThemed("换品牌", slides, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	alt := ClassicSlideTheme()
	alt.Navy = "16324F"
	alt.Paper = "F7F7F2"
	alt.Ink = "1D2733"
	other, err := GenStudioPptxThemed("换品牌", slides, nil, alt)
	if err != nil {
		t.Fatal(err)
	}
	masterA := zipPartBody(t, base, "ppt/slideMasters/slideMaster1.xml")
	masterB := zipPartBody(t, other, "ppt/slideMasters/slideMaster1.xml")
	if bytes.Equal([]byte(masterA), []byte(masterB)) {
		t.Fatal("changing the brand theme must regenerate the master part")
	}
	if !strings.Contains(masterB, `val="F7F7F2"`) {
		t.Fatalf("new master must carry the new paper color: %.200s", masterB)
	}
	layoutA := zipPartBody(t, base, "ppt/slideLayouts/slideLayout3.xml")
	layoutB := zipPartBody(t, other, "ppt/slideLayouts/slideLayout3.xml")
	if layoutA == "" || layoutB == "" {
		t.Fatal("appended layout parts are missing")
	}
	themeB := zipPartBody(t, other, "ppt/theme/theme1.xml")
	if !strings.Contains(themeB, `val="16324F"`) {
		t.Fatal("theme part must carry the new navy")
	}
}

func TestStudioDeckGenerationIsDeterministic(t *testing.T) {
	slides := []SlideSpec{
		{Title: "封面", Layout: "cover", Subtitle: "副标题"},
		{Title: "议程", Layout: "agenda", Bullets: []string{"一", "二"}},
		{Title: "指标", Layout: "metrics", Metrics: []SlideMetric{{Label: "毛利", Value: "18", Unit: "%"}}},
		{Title: "结束", Layout: "closing"},
	}
	first, err := GenStudioPptxThemed("确定性", slides, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenStudioPptxThemed("确定性", slides, nil, ClassicSlideTheme())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same theme and slides must produce byte-identical decks")
	}
}
