package webfetch

import (
	"fmt"
	"html"
	"net/url"
	"strings"
)

// SearchEndpoint is the fixed, keyless HTML search frontend the runtime uses.
// It is fetched through the same SSRF-pinned transport as any other URL.
const SearchEndpoint = "https://lite.duckduckgo.com/lite/"

// BingSearchEndpoint and BingCNSearchEndpoint are keyless HTML fallbacks when
// DuckDuckGo Lite is slow or blocked (common on some networks).
const BingSearchEndpoint = "https://www.bing.com/search"
const BingCNSearchEndpoint = "https://cn.bing.com/search"

// SearchURL builds the search GET URL for query.
func SearchURL(query string) string {
	return SearchEndpoint + "?q=" + url.QueryEscape(query)
}

// BingSearchURL builds the international Bing HTML search URL.
func BingSearchURL(query string) string {
	return BingSearchEndpoint + "?q=" + url.QueryEscape(query) + "&setlang=zh-CN"
}

// BingCNSearchURL builds the cn.bing.com HTML search URL.
func BingCNSearchURL(query string) string {
	return BingCNSearchEndpoint + "?q=" + url.QueryEscape(query)
}

// SearchResult is one parsed organic result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// ParseSearchResults extracts organic results from a DuckDuckGo Lite page.
// Result links are /l/?uddg=<encoded> redirect wrappers; the wrapped target
// is decoded and must be an absolute http(s) URL. Parsing is tolerant: when
// the expected classes are absent the page yields zero results rather than an
// error, so a markup change degrades to "no results" instead of a crash.
func ParseSearchResults(html string, max int) []SearchResult {
	if max <= 0 {
		max = 5
	}
	var results []SearchResult
	rest := html
	for len(results) < max {
		anchor := strings.Index(rest, "result-link")
		if anchor < 0 {
			break
		}
		// Back up to the opening '<a ' of this anchor tag.
		open := strings.LastIndex(rest[:anchor], "<a")
		if open < 0 {
			break
		}
		tagEnd := strings.Index(rest[anchor:], ">")
		if tagEnd < 0 {
			break
		}
		tag := rest[open : anchor+tagEnd+1]
		closeEnd := strings.Index(rest[anchor+tagEnd:], "</a>")
		if closeEnd < 0 {
			break
		}
		text := rest[anchor+tagEnd+1 : anchor+tagEnd+closeEnd]
		rest = rest[anchor+tagEnd+closeEnd+4:]

		href := attrValue(tag, "href")
		target := unwrapSearchRedirect(href)
		if target == "" {
			continue
		}
		title := strings.TrimSpace(unescapeEntities(stripTags(text)))
		snippet := ""
		if idx := strings.Index(rest, "result-snippet"); idx >= 0 {
			if cell := strings.Index(rest[idx:], ">"); cell >= 0 {
				raw := rest[idx+cell+1:]
				if end := strings.Index(raw, "</td>"); end >= 0 {
					snippet = strings.TrimSpace(unescapeEntities(stripTags(raw[:end])))
				}
			}
		}
		results = append(results, SearchResult{Title: title, URL: target, Snippet: snippet})
	}
	return results
}

// ParseBingResults extracts organic results from a Bing HTML results page.
func ParseBingResults(page string, max int) []SearchResult {
	if max <= 0 {
		max = 5
	}
	var results []SearchResult
	rest := page
	for len(results) < max {
		idx := strings.Index(rest, "b_algo")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		next := strings.Index(rest[1:], "b_algo")
		block := rest
		if next >= 0 {
			block = rest[:1+next]
			rest = rest[1+next:]
		} else {
			rest = ""
		}
		href, title := firstHTTPAnchor(block)
		if href == "" || isBingNavURL(href) {
			continue
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     href,
			Snippet: firstParagraph(block),
		})
	}
	return results
}

// RenderSearchHTML builds a dark, network-blocked preview of ranked results
// for the in-app workspace browser tab.
func RenderSearchHTML(query string, results []SearchResult) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>`)
	b.WriteString(html.EscapeString(query))
	b.WriteString(`</title><style>
body{margin:0;padding:22px 24px;font:14px/1.55 -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b111a;color:#e8eef7}
h1{margin:0 0 16px;font-size:16px;font-weight:650}
ol{margin:0;padding:0;list-style:none;display:grid;gap:14px}
a{color:#8ec5ff;text-decoration:none}
small{display:block;margin:4px 0;color:#6f8aad;word-break:break-all}
p{margin:0;color:#c5d0de}
.empty{color:#8a9bb0}
</style></head><body><h1>搜索结果 · `)
	b.WriteString(html.EscapeString(query))
	b.WriteString(`</h1>`)
	if len(results) == 0 {
		b.WriteString(`<p class="empty">没有找到结果。</p></body></html>`)
		return b.String()
	}
	b.WriteString(`<ol class="serp">`)
	for i, hit := range results {
		fmt.Fprintf(&b, `<li class="serp-hit"><a href="%s">%d. %s</a><small>%s</small><p>%s</p></li>`,
			html.EscapeString(hit.URL), i+1, html.EscapeString(hit.Title), html.EscapeString(hit.URL), html.EscapeString(hit.Snippet))
	}
	b.WriteString(`</ol></body></html>`)
	return b.String()
}

// RenderExtractHTML builds a dark preview of a fetched page's extracted text.
func RenderExtractHTML(title, finalURL, body string) string {
	if title == "" {
		title = finalURL
	}
	return `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>` +
		html.EscapeString(title) + `</title><style>
body{margin:0;padding:22px 24px;font:14px/1.6 -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:#0b111a;color:#e8eef7}
h1{margin:0 0 8px;font-size:16px}
small{display:block;margin:0 0 16px;color:#6f8aad;word-break:break-all}
pre{margin:0;white-space:pre-wrap;color:#c5d0de}
</style></head><body><h1>` + html.EscapeString(title) + `</h1><small>` +
		html.EscapeString(finalURL) + `</small><pre>` + html.EscapeString(body) + `</pre></body></html>`
}

func firstHTTPAnchor(block string) (href, title string) {
	rest := block
	for {
		open := strings.Index(strings.ToLower(rest), "<a")
		if open < 0 {
			return "", ""
		}
		tagEnd := strings.Index(rest[open:], ">")
		if tagEnd < 0 {
			return "", ""
		}
		tag := rest[open : open+tagEnd+1]
		closeEnd := strings.Index(strings.ToLower(rest[open+tagEnd:]), "</a>")
		if closeEnd < 0 {
			return "", ""
		}
		text := rest[open+tagEnd+1 : open+tagEnd+closeEnd]
		rest = rest[open+tagEnd+closeEnd+4:]
		target := unwrapSearchRedirect(attrValue(tag, "href"))
		if target == "" {
			continue
		}
		title = strings.TrimSpace(unescapeEntities(stripTags(text)))
		if title == "" {
			continue
		}
		return target, title
	}
}

func firstParagraph(block string) string {
	lower := strings.ToLower(block)
	idx := strings.Index(lower, "<p")
	if idx < 0 {
		return ""
	}
	cell := strings.Index(block[idx:], ">")
	if cell < 0 {
		return ""
	}
	raw := block[idx+cell+1:]
	end := strings.Index(strings.ToLower(raw), "</p>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(unescapeEntities(stripTags(raw[:end])))
}

func isBingNavURL(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if host == "bing.com" || strings.HasSuffix(host, ".bing.com") || host == "microsoft.com" || strings.HasSuffix(host, ".microsoft.com") || host == "microsoftonline.com" || strings.HasSuffix(host, ".microsoftonline.com") {
		path := strings.ToLower(u.Path)
		if path == "" || path == "/" || strings.HasPrefix(path, "/account") || strings.HasPrefix(path, "/images") || strings.HasPrefix(path, "/videos") || strings.HasPrefix(path, "/maps") || strings.HasPrefix(path, "/shop") || strings.HasPrefix(path, "/translator") {
			return true
		}
	}
	return false
}

// unwrapSearchRedirect decodes a //duckduckgo.com/l/?uddg= wrapper to its
// target; direct absolute http(s) links pass through. Everything else
// (relative links, internal navigation, non-web schemes) is dropped.
func unwrapSearchRedirect(href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if (host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com")) && strings.HasPrefix(u.Path, "/l/") {
		target := u.Query().Get("uddg")
		if target == "" {
			return ""
		}
		u, err = url.Parse(target)
		if err != nil {
			return ""
		}
	}
	if !u.IsAbs() || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return u.String()
}

// attrValue extracts a double- or single-quoted attribute from a tag.
func attrValue(tag, attr string) string {
	lower := strings.ToLower(tag)
	for _, quote := range []byte{'"', '\''} {
		needle := attr + "=" + string(quote)
		idx := strings.Index(lower, needle)
		if idx < 0 {
			continue
		}
		start := idx + len(needle)
		if end := strings.IndexByte(tag[start:], quote); end >= 0 {
			return tag[start : start+end]
		}
	}
	return ""
}

// stripTags removes markup from a small HTML fragment.
func stripTags(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
