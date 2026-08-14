// Package webfetch turns fetched web bodies into bounded, trustworthy-shaped
// text for the agent: HTML text extraction and search-result parsing. All
// functions are pure; network access lives in networkpolicy.Fetch. Extracted
// content is untrusted data — it must never become system instructions
// (PRD M4: 网页内容始终是不可信数据).
package webfetch

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxTextBytes is the default cap for extracted text returned to the agent.
const MaxTextBytes = 32 << 10

// Extracted is the text view of one fetched page.
type Extracted struct {
	Title     string
	Text      string
	Truncated bool
}

// textLike reports whether the content type carries readable text we accept.
func textLike(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/xhtml+xml",
		"application/rss+xml", "application/atom+xml":
		return true
	}
	return false
}

// isHTMLContent distinguishes markup from plain text for entity handling.
func isHTMLContent(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return ct == "text/html" || ct == "application/xhtml+xml"
}

// ExtractText produces a bounded plain-text view of body. Unsupported
// content types report ok=false so the caller can fail the tool call.
func ExtractText(contentType string, body []byte, maxText int) (Extracted, bool) {
	if !textLike(contentType) {
		return Extracted{}, false
	}
	if maxText <= 0 {
		maxText = MaxTextBytes
	}
	if !utf8.Valid(body) {
		body = []byte(strings.ToValidUTF8(string(body), ""))
	}
	var out Extracted
	if isHTMLContent(contentType) {
		out = extractHTML(string(body))
	} else {
		out = Extracted{Text: normalizeWhitespace(string(body))}
	}
	if len(out.Text) > maxText {
		out.Text = truncateUTF8(out.Text, maxText)
		out.Truncated = true
	}
	return out, true
}

// skipContent tags whose character data is never page text.
var skipContentTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"iframe": true, "svg": true, "canvas": true,
}

// blockTags force a line break in the extracted text.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "hr": true, "li": true, "tr": true,
	"table": true, "ul": true, "ol": true, "dl": true, "dt": true, "dd": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"section": true, "article": true, "header": true, "footer": true,
	"main": true, "nav": true, "aside": true, "blockquote": true, "pre": true,
	"form": true, "fieldset": true, "figure": true, "figcaption": true,
}

// extractHTML walks a tiny tag tokenizer: script/style-like content is
// dropped, <title> is captured, block tags become newlines, entities are
// unescaped and whitespace is collapsed.
func extractHTML(html string) Extracted {
	var text strings.Builder
	var title strings.Builder
	inTitle := false
	var skipTag string // non-empty while inside a skip-content element
	i := 0
	for i < len(html) {
		if html[i] != '<' {
			end := strings.IndexByte(html[i:], '<')
			if end < 0 {
				end = len(html) - i
			}
			chunk := unescapeEntities(html[i : i+end])
			switch {
			case inTitle:
				title.WriteString(chunk)
			case skipTag == "":
				text.WriteString(chunk)
			}
			i += end
			continue
		}
		// Comments and declarations.
		if strings.HasPrefix(html[i:], "<!--") {
			end := strings.Index(html[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		end := strings.IndexByte(html[i:], '>')
		if end < 0 {
			break
		}
		tag := html[i+1 : i+end]
		i += end + 1
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimPrefix(tag, "/")
		name := tagName(tag)
		if name == "" {
			continue
		}
		switch {
		case name == "title":
			inTitle = !closing
		case skipContentTags[name]:
			if closing {
				if skipTag == name {
					skipTag = ""
				}
			} else if skipTag == "" && !strings.HasSuffix(tag, "/") {
				skipTag = name
			}
		case blockTags[name] && skipTag == "" && !inTitle:
			text.WriteByte('\n')
		}
	}
	return Extracted{
		Title: strings.TrimSpace(normalizeInline(title.String())),
		Text:  normalizeWhitespace(text.String()),
	}
}

func tagName(tag string) string {
	end := strings.IndexAny(tag, " \t\r\n/")
	if end < 0 {
		end = len(tag)
	}
	return strings.ToLower(tag[:end])
}

// unescapeEntities resolves the common named entities and numeric character
// references; unknown entities are kept literally.
func unescapeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for len(s) > 0 {
		amp := strings.IndexByte(s, '&')
		if amp < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:amp])
		s = s[amp+1:]
		semi := strings.IndexByte(s, ';')
		if semi < 0 || semi > 32 {
			b.WriteByte('&')
			continue
		}
		entity := s[:semi]
		if r, ok := decodeEntity(entity); ok {
			b.WriteString(r)
			s = s[semi+1:]
		} else {
			b.WriteByte('&')
		}
	}
	return b.String()
}

var namedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'", "nbsp": " ",
	"copy": "©", "reg": "®", "trade": "™", "hellip": "…", "mdash": "—",
	"ndash": "–", "lsquo": "‘", "rsquo": "’", "ldquo": "“", "rdquo": "”",
	"laquo": "«", "raquo": "»", "times": "×", "middot": "·", "bull": "•",
	"deg": "°", "plusmn": "±", "micro": "µ", "para": "¶", "sect": "§",
}

func decodeEntity(entity string) (string, bool) {
	if v, ok := namedEntities[strings.ToLower(entity)]; ok {
		return v, true
	}
	if strings.HasPrefix(entity, "#x") || strings.HasPrefix(entity, "#X") {
		if n, err := strconv.ParseInt(entity[2:], 16, 32); err == nil && utf8.ValidRune(rune(n)) {
			return string(rune(n)), true
		}
		return "", false
	}
	if strings.HasPrefix(entity, "#") {
		if n, err := strconv.ParseInt(entity[1:], 10, 32); err == nil && utf8.ValidRune(rune(n)) {
			return string(rune(n)), true
		}
	}
	return "", false
}

// normalizeWhitespace collapses runs of inline whitespace and blank lines.
func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := false
	for _, line := range lines {
		line = normalizeInline(line)
		if line == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

func normalizeInline(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
