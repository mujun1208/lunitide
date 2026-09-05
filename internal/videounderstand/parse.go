package videounderstand

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxCaptionBytes = 32 << 10

type Page struct {
	Title       string
	Author      string
	Description string
	ImageURL    string
	CaptionURL  string
	CaptionText string
	LoginWall   bool
	Captcha     bool
}

func ParseHTML(body string) Page {
	var page Page
	page.Title = firstNonEmpty(
		metaContent(body, "og:title"),
		metaContent(body, "twitter:title"),
		htmlTitle(body),
	)
	page.Description = firstNonEmpty(
		metaContent(body, "og:description"),
		metaContent(body, "twitter:description"),
	)
	page.ImageURL = metaContent(body, "og:image")
	if name, desc, author := jsonLDVideo(body); name != "" || desc != "" {
		page.Title = firstNonEmpty(page.Title, name)
		page.Description = firstNonEmpty(page.Description, desc)
		page.Author = firstNonEmpty(page.Author, author)
	}
	b := extractBilibiliState(body)
	if b.Title != "" || b.CaptionURL != "" {
		page.Title = firstNonEmpty(b.Title, page.Title)
		page.Description = firstNonEmpty(b.Description, page.Description)
		page.Author = firstNonEmpty(b.Author, page.Author)
		page.CaptionURL = firstNonEmpty(page.CaptionURL, b.CaptionURL)
	}
	y := extractYouTubePlayer(body)
	if y.Title != "" || y.CaptionURL != "" {
		page.Title = firstNonEmpty(y.Title, page.Title)
		page.Description = firstNonEmpty(y.Description, page.Description)
		page.Author = firstNonEmpty(y.Author, page.Author)
		page.CaptionURL = firstNonEmpty(page.CaptionURL, y.CaptionURL)
	}
	lower := strings.ToLower(body)
	hasMeta := page.Title != "" || page.Description != ""
	hasPlayer := (b.Title != "" || b.CaptionURL != "") || (y.Title != "" || y.CaptionURL != "")
	page.LoginWall = looksLikeLoginWall(body, lower, hasMeta, hasPlayer)
	if !hasPlayer && (strings.Contains(body, "验证码") || strings.Contains(lower, "captcha") || strings.Contains(lower, "recaptcha")) {
		page.Captcha = true
	}
	return page
}

func looksLikeLoginWall(body, lower string, hasMeta, hasPlayer bool) bool {
	strong := strings.Contains(body, "请登录后") || strings.Contains(lower, "login required")
	if strong && !hasPlayer {
		return true
	}
	if !hasMeta && (strings.Contains(body, "请登录") || strings.Contains(body, "passport.") || strings.Contains(lower, "login required")) {
		return true
	}
	return false
}

func ParseCaptions(contentType string, body []byte) (text string, truncated bool) {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return "", false
	}
	if strings.Contains(contentType, "json") || looksLikeJSON(raw) {
		text = joinCaptionJSON(raw)
	} else {
		text = stripMarkup(raw)
	}
	text = strings.TrimSpace(text)
	if len(text) > MaxCaptionBytes {
		return truncateUTF8(text, MaxCaptionBytes), true
	}
	return text, false
}

func metaContent(body, prop string) string {
	re := regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)=["']` + regexp.QuoteMeta(prop) + `["'][^>]*>`)
	m := re.FindString(body)
	if m == "" {
		re = regexp.MustCompile(`(?is)<meta[^>]+content=["'][^"']+["'][^>]+(?:property|name)=["']` + regexp.QuoteMeta(prop) + `["'][^>]*>`)
		m = re.FindString(body)
	}
	if m == "" {
		return ""
	}
	cm := regexp.MustCompile(`(?i)content=["']([^"']+)["']`).FindStringSubmatch(m)
	if len(cm) < 2 {
		return ""
	}
	return normalizeSpace(html.UnescapeString(cm[1]))
}

func htmlTitle(body string) string {
	m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return normalizeSpace(html.UnescapeString(stripMarkup(m[1])))
}

func jsonLDVideo(body string) (name, desc, author string) {
	re := regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	for _, m := range re.FindAllStringSubmatch(body, 8) {
		raw := strings.TrimSpace(m[1])
		var obj map[string]any
		if json.Unmarshal([]byte(raw), &obj) != nil {
			continue
		}
		types := stringify(obj["@type"])
		if !strings.Contains(strings.ToLower(types), "video") {
			continue
		}
		name = stringify(obj["name"])
		desc = stringify(obj["description"])
		switch a := obj["author"].(type) {
		case string:
			author = a
		case map[string]any:
			author = stringify(a["name"])
		}
		return normalizeSpace(name), normalizeSpace(desc), normalizeSpace(author)
	}
	return "", "", ""
}

func extractBilibiliState(body string) Page {
	raw := extractJSAssign(body, "__INITIAL_STATE__")
	if raw == "" {
		return Page{}
	}
	var root map[string]any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return Page{}
	}
	video, _ := root["videoData"].(map[string]any)
	if video == nil {
		if inner, ok := root["video"].(map[string]any); ok {
			video, _ = inner["view"].(map[string]any)
		}
	}
	if video == nil {
		return Page{}
	}
	var page Page
	page.Title = stringify(video["title"])
	page.Description = stringify(video["desc"])
	if owner, ok := video["owner"].(map[string]any); ok {
		page.Author = stringify(owner["name"])
	}
	if sub, ok := video["subtitle"].(map[string]any); ok {
		page.CaptionURL = firstSubtitleURL(sub["list"])
	}
	if page.CaptionURL == "" {
		if player, ok := root["subtitle"].(map[string]any); ok {
			page.CaptionURL = firstSubtitleURL(player["list"])
		}
	}
	return page
}

func extractYouTubePlayer(body string) Page {
	raw := extractJSAssign(body, "ytInitialPlayerResponse")
	if raw == "" {
		return Page{}
	}
	var root map[string]any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return Page{}
	}
	var page Page
	if details, ok := root["videoDetails"].(map[string]any); ok {
		page.Title = stringify(details["title"])
		page.Description = stringify(details["shortDescription"])
		page.Author = stringify(details["author"])
	}
	caps, _ := root["captions"].(map[string]any)
	if caps == nil {
		return page
	}
	tracklist, _ := caps["playerCaptionsTracklistRenderer"].(map[string]any)
	page.CaptionURL = firstCaptionTrackURL(tracklist["captionTracks"])
	return page
}

func extractJSAssign(body, name string) string {
	idx := strings.Index(body, name)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(name):]
	eq := strings.Index(rest, "=")
	if eq < 0 || eq > 16 {
		return ""
	}
	rest = strings.TrimSpace(rest[eq+1:])
	if !strings.HasPrefix(rest, "{") {
		return ""
	}
	return extractJSONObject(rest)
}

func extractJSONObject(src string) string {
	depth := 0
	inStr := false
	esc := false
	for i, r := range src {
		if inStr {
			if esc {
				esc = false
				continue
			}
			if r == '\\' {
				esc = true
				continue
			}
			if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[:i+1]
			}
		}
	}
	return ""
}

func firstSubtitleURL(list any) string {
	arr, ok := list.([]any)
	if !ok {
		return ""
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"subtitle_url", "subtitleUrl", "url"} {
			if u := stringify(m[key]); u != "" {
				return u
			}
		}
	}
	return ""
}

func firstCaptionTrackURL(list any) string {
	arr, ok := list.([]any)
	if !ok {
		return ""
	}
	var fallback string
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		u := stringify(m["baseUrl"])
		if u == "" {
			continue
		}
		lang := strings.ToLower(stringify(m["languageCode"]))
		if strings.HasPrefix(lang, "zh") || lang == "en" {
			return u
		}
		if fallback == "" {
			fallback = u
		}
	}
	return fallback
}

func joinCaptionJSON(raw string) string {
	var rows []map[string]any
	if json.Unmarshal([]byte(raw), &rows) == nil {
		var parts []string
		for _, row := range rows {
			if c := stringify(row["content"]); c != "" {
				parts = append(parts, c)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	var wrap map[string]any
	if json.Unmarshal([]byte(raw), &wrap) == nil {
		if body, ok := wrap["body"].([]any); ok {
			var parts []string
			for _, item := range body {
				m, _ := item.(map[string]any)
				if c := stringify(m["content"]); c != "" {
					parts = append(parts, c)
				}
			}
			return strings.Join(parts, "\n")
		}
	}
	return stripMarkup(raw)
}

func looksLikeJSON(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")
}

func stripMarkup(raw string) string {
	re := regexp.MustCompile(`(?s)<[^>]+>`)
	return re.ReplaceAllString(raw, " ")
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeSpace(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
