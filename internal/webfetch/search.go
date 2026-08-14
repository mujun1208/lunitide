package webfetch

import (
	"net/url"
	"strings"
)

// SearchEndpoint is the fixed, keyless HTML search frontend the runtime uses.
// It is fetched through the same SSRF-pinned transport as any other URL.
const SearchEndpoint = "https://lite.duckduckgo.com/lite/"

// SearchURL builds the search GET URL for query.
func SearchURL(query string) string {
	return SearchEndpoint + "?q=" + url.QueryEscape(query)
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
