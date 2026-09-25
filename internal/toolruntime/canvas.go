package toolruntime

import (
	"fmt"
	"html"
	"strings"
)

type canvasDoc struct {
	Title    string          `json:"title"`
	Intro    string          `json:"intro"`
	Sections []canvasSection `json:"sections"`
	Bars     []canvasBar     `json:"bars"`
	HTML     string          `json:"html"`
}

type canvasSection struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

type canvasBar struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Max   float64 `json:"max"`
}

func renderCanvas(args []byte) (string, error) {
	var doc canvasDoc
	if strict(args, &doc) != nil || strings.TrimSpace(doc.Title) == "" {
		return "", fmt.Errorf("invalid arguments")
	}
	if len(doc.Sections) > 12 || len(doc.Bars) > 12 || len(doc.HTML) > 24000 || len(doc.Title) > 200 {
		return "", fmt.Errorf("invalid arguments")
	}
	if strings.TrimSpace(doc.Intro) == "" && len(doc.Sections) == 0 && len(doc.Bars) == 0 && strings.TrimSpace(doc.HTML) == "" {
		return "", fmt.Errorf("invalid arguments")
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(doc.Title))
	b.WriteString("</title><style>")
	b.WriteString(canvasCSS)
	b.WriteString("</style></head><body><article class=\"canvas\"><h1>")
	b.WriteString(html.EscapeString(doc.Title))
	b.WriteString("</h1>")
	if strings.TrimSpace(doc.Intro) != "" {
		b.WriteString("<p class=\"intro\">")
		b.WriteString(html.EscapeString(doc.Intro))
		b.WriteString("</p>")
	}
	for _, section := range doc.Sections {
		if strings.TrimSpace(section.Heading) == "" && strings.TrimSpace(section.Body) == "" {
			continue
		}
		b.WriteString("<section><h2>")
		b.WriteString(html.EscapeString(section.Heading))
		b.WriteString("</h2><p>")
		b.WriteString(html.EscapeString(section.Body))
		b.WriteString("</p></section>")
	}
	if len(doc.Bars) > 0 {
		b.WriteString("<section class=\"bars\">")
		for _, bar := range doc.Bars {
			max := bar.Max
			if max <= 0 {
				max = 10
			}
			pct := bar.Value / max * 100
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			b.WriteString("<div class=\"bar\"><span>")
			b.WriteString(html.EscapeString(bar.Label))
			fmt.Fprintf(&b, "</span><i style=\"width:%.1f%%\"></i><b>%.0f</b></div>", pct, bar.Value)
		}
		b.WriteString("</section>")
	}
	if fragment := stripCanvasScripts(doc.HTML); strings.TrimSpace(fragment) != "" {
		b.WriteString("<section class=\"extra\">")
		b.WriteString(fragment)
		b.WriteString("</section>")
	}
	b.WriteString("</article></body></html>")
	return b.String(), nil
}

func stripCanvasScripts(fragment string) string {
	lower := strings.ToLower(fragment)
	var b strings.Builder
	i := 0
	for {
		start := strings.Index(lower[i:], "<script")
		if start < 0 {
			b.WriteString(fragment[i:])
			break
		}
		start += i
		b.WriteString(fragment[i:start])
		end := strings.Index(lower[start:], "</script>")
		if end < 0 {
			break
		}
		i = start + end + len("</script>")
	}
	return b.String()
}

const canvasCSS = `
:root { color-scheme: dark; }
body { margin: 0; background: #0e1116; color: #e7ecf3; font: 16px/1.55 "Segoe UI", "Microsoft YaHei", sans-serif; }
.canvas { max-width: 880px; margin: 0 auto; padding: 36px 32px 64px; }
h1 { font-size: 32px; letter-spacing: -0.03em; margin: 0 0 12px; }
h2 { font-size: 18px; margin: 0 0 8px; }
.intro, section p { color: #c5ceda; }
section { margin: 22px 0; padding: 16px 18px; background: #171c24; border: 1px solid #2a3342; border-radius: 12px; }
.bar { display: grid; grid-template-columns: 112px 1fr 36px; gap: 10px; align-items: center; margin: 8px 0; }
.bar i { display: block; height: 10px; border-radius: 999px; background: linear-gradient(90deg, #f0a05a, #7eb6ff); }
.bar b { text-align: right; font-weight: 600; }
`
