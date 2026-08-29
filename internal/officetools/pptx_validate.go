package officetools

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ValidatePptx fails closed on empty, fill-only, or unreadable decks.
// PowerPoint "已修复" empty navy canvases are this check: a slide with a
// dark background and no <a:t> text must never be treated as success.
func ValidatePptx(data []byte) error {
	if len(data) < 4 || !bytes.HasPrefix(data, []byte("PK")) {
		return fmt.Errorf("officetools: pptx is not a zip container")
	}
	names, err := zipEntryNames(data)
	if err != nil {
		return fmt.Errorf("officetools: pptx zip: %w", err)
	}
	var slides []string
	for _, name := range names {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "/_rels/") {
			slides = append(slides, name)
		}
	}
	if len(slides) == 0 {
		return fmt.Errorf("officetools: pptx has no slides")
	}
	sortSlideNames(slides)
	for i, name := range slides {
		body, err := readZipPart(data, name, 1<<20)
		if err != nil {
			return fmt.Errorf("officetools: slide %d: %w", i+1, err)
		}
		if err := validateSlideXML(i+1, body); err != nil {
			return err
		}
	}
	return nil
}

func validateSlideSpecs(title string, slides []SlideSpec) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("officetools: deck title is required")
	}
	for i, s := range slides {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("officetools: slide %d needs a visible title", i+1)
		}
	}
	return nil
}

func validateSlideXML(index int, body string) error {
	dec := xml.NewDecoder(strings.NewReader(body))
	for {
		if _, err := dec.Token(); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("officetools: slide %d XML is not well-formed: %w", index, err)
		}
	}
	var texts []string
	for _, m := range rePptxText.FindAllStringSubmatch(body, -1) {
		if t := strings.TrimSpace(xmlUnescape(m[1])); t != "" {
			texts = append(texts, t)
		}
	}
	if len(texts) == 0 {
		return fmt.Errorf("officetools: slide %d is empty (no visible text); refuse fill-only canvases", index)
	}
	if darkOnlyUnreadable(body) {
		return fmt.Errorf("officetools: slide %d is dark fill with no light text (unreadable)", index)
	}
	return nil
}

func darkOnlyUnreadable(body string) bool {
	bg := slideBgHex(body)
	if bg == "" || !hexIsDark(bg) {
		return false
	}
	return !hasLightTextRun(body)
}

func hasLightTextRun(body string) bool {
	for _, hex := range slideTextColors(body) {
		if !hexIsDark(hex) {
			return true
		}
	}
	return false
}

func slideBgHex(body string) string {
	const needle = `<p:bg>`
	i := strings.Index(body, needle)
	if i < 0 {
		return ""
	}
	chunk := body[i:]
	if end := strings.Index(chunk, `</p:bg>`); end > 0 {
		chunk = chunk[:end]
	}
	return firstSrgb(chunk)
}

func slideTextColors(body string) []string {
	var out []string
	for _, m := range rePptxText.FindAllStringIndex(body, -1) {
		start := m[0] - 240
		if start < 0 {
			start = 0
		}
		if hex := firstSrgb(body[start:m[0]]); hex != "" {
			out = append(out, hex)
		}
	}
	return out
}

func firstSrgb(chunk string) string {
	const tag = `<a:srgbClr val="`
	i := strings.Index(chunk, tag)
	if i < 0 {
		return ""
	}
	rest := chunk[i+len(tag):]
	end := strings.IndexByte(rest, '"')
	if end < 6 {
		return ""
	}
	return strings.ToUpper(rest[:end])
}

func hexIsDark(hex string) bool {
	if len(hex) != 6 {
		return false
	}
	r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	// Rec. 709 luminance; navy 0B1F3A is dark, FFFFFF / A5F3FC / C9A227 are not.
	y := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
	return y < 90
}

func sortSlideNames(names []string) {
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if slideIndex(names[j]) < slideIndex(names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
}

// DarkOnlySlideFixture builds an invalid navy fill-only slide for tests.
func DarkOnlySlideFixture() []byte {
	slide := xmlDecl + `
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld>` +
		pptxBg(clrNavy) + pptxSpTreeOpen() +
		pptxRect(2, "Fill", clrNavy, 0, 0, pptxW, pptxH) +
		pptxTreeClose()
	data, err := zipBytes([]zipPart{
		{"[Content_Types].xml", contentTypesPptx(1)},
		{"_rels/.rels", relsPptx},
		{"ppt/presentation.xml", presentationXML(1)},
		{"ppt/_rels/presentation.xml.rels", presentationRels(1)},
		{"ppt/slides/slide1.xml", slide},
		{"ppt/slides/_rels/slide1.xml.rels", xmlDecl + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
	})
	if err != nil {
		return nil
	}
	return data
}
