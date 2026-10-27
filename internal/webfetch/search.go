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

// SearchUserAgent is the browser-style agent search pages are fetched with.
// Keyless search frontends degrade non-browser agents: DuckDuckGo Lite times
// them out entirely, and Bing serves overseas keyless requests a canned page
// of unrelated popular results. A real browser agent gets the genuine SERP
// from the same endpoints, so the search fetch sends one while ordinary
// evidence fetches keep the honest "local agent" identity.
const SearchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// BingSearchEndpoint and BingCNSearchEndpoint are keyless HTML fallbacks when
// DuckDuckGo Lite is slow or blocked (common on some networks).
const BingSearchEndpoint = "https://www.bing.com/search"
const BingCNSearchEndpoint = "https://cn.bing.com/search"

// BaiduSearchEndpoint and SogouSearchEndpoint are the domestic keyless HTML
// search sources. Overseas engines (DuckDuckGo, Bing mirrors) can be slow or
// entirely unreachable on mainland connections, which used to leave a
// Chinese-language query with zero sources when the network degraded; Baidu
// and Sogou are served from inside the mainland, so they close that gap.
const BaiduSearchEndpoint = "https://www.baidu.com/s"
const SogouSearchEndpoint = "https://www.sogou.com/web"

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

// BaiduSearchURL builds the Baidu HTML search URL.
func BaiduSearchURL(query string) string {
	return BaiduSearchEndpoint + "?wd=" + url.QueryEscape(query)
}

// SogouSearchURL builds the Sogou HTML search URL.
func SogouSearchURL(query string) string {
	return SogouSearchEndpoint + "?query=" + url.QueryEscape(query)
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
		// A result may have no snippet. Do not attach the next result's text
		// to this URL: it changes the apparent source of search evidence.
		snippetBlock := rest
		if next := strings.Index(snippetBlock, "result-link"); next >= 0 {
			snippetBlock = snippetBlock[:next]
		}
		if idx := strings.Index(snippetBlock, "result-snippet"); idx >= 0 {
			if cell := strings.Index(snippetBlock[idx:], ">"); cell >= 0 {
				raw := snippetBlock[idx+cell+1:]
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
		idx := indexBingBlock(rest)
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		next := indexBingBlock(rest[1:])
		block := rest
		if next >= 0 {
			block = rest[:1+next]
			rest = rest[1+next:]
		} else {
			rest = ""
		}
		href, title := bingTitleAnchor(block)
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

// indexBingBlock returns the offset of the next b_algo result token in s, or
// -1. A bare substring search would also hit longer class names such as
// "b_algoReadMore" (the read-more link inside a result's own caption), which
// would cut the result's block at its snippet and drop the text after it.
func indexBingBlock(s string) int {
	for off := 0; off < len(s); {
		idx := strings.Index(s[off:], "b_algo")
		if idx < 0 {
			return -1
		}
		at := off + idx
		if at+6 >= len(s) || !isWordByte(s[at+6]) {
			return at
		}
		off = at + 6
	}
	return -1
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// bingTitleAnchor returns the headline anchor of one result block. Degraded
// keyless-UA markups lead each block with a b_tpcn attribution pill whose
// anchor text glues the site domain to the cite URL
// ("baidu.comhttps://aiqicha.baidu.com"); the real headline is the <h2>
// anchor. The pill is not a title, so prefer <h2> and only fall back to the
// first usable anchor of the block.
func bingTitleAnchor(block string) (href, title string) {
	if h := strings.Index(strings.ToLower(block), "<h2"); h >= 0 {
		if href, title = firstHTTPAnchor(block[h:]); href != "" {
			return href, title
		}
	}
	return firstHTTPAnchor(block)
}

// ParseBaiduResults extracts organic results from a Baidu HTML results page.
// Organic containers mirror the real target URL in their mu attribute; the
// headline anchor instead points at a /link?url= redirect wrapper, so the
// parser prefers mu and falls back to the wrapper (a later fetch follows the
// redirect). Parsing is tolerant: when the expected markup is absent the
// page yields zero results rather than an error.
func ParseBaiduResults(page string, max int) []SearchResult {
	if max <= 0 {
		max = 5
	}
	var results []SearchResult
	rest := page
	for len(results) < max {
		idx := indexClassBlock(rest, "result")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		next := indexClassBlock(rest[1:], "result")
		block := rest
		if next >= 0 {
			block = rest[:1+next]
			rest = rest[1+next:]
		} else {
			rest = ""
		}
		href := unwrapSearchRedirect(firstAttrValue(block, "mu"))
		title := ""
		if aHref, aTitle := baiduTitleAnchor(block); aTitle != "" {
			title = aTitle
			if href == "" {
				href = aHref
			}
		}
		if href == "" || title == "" || isBaiduNavURL(href) {
			continue
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     href,
			Snippet: baiduSnippet(block),
		})
	}
	return results
}

// indexClassBlock returns the offset of the next class token (class="token"
// boundaries only, so "result" matches "result c-container" and
// "result-op" but not "results" or "result-list") in s, or -1.
func indexClassBlock(s, token string) int {
	needle := `class="` + token
	needleLen := len(needle)
	for off := 0; off < len(s); {
		idx := strings.Index(s[off:], needle)
		if idx < 0 {
			return -1
		}
		at := off + idx
		if at+needleLen >= len(s) || !isWordByte(s[at+needleLen]) {
			return at
		}
		off = at + needleLen
	}
	return -1
}

// baiduTitleAnchor returns the headline anchor of one Baidu result block.
// The href may be a site-relative /link redirect wrapper, which is
// absolutized against www.baidu.com so a later fetch can follow it.
func baiduTitleAnchor(block string) (href, title string) {
	h := strings.Index(strings.ToLower(block), "<h3")
	if h < 0 {
		return "", ""
	}
	seg := block[h:]
	open := strings.Index(strings.ToLower(seg), "<a")
	if open < 0 {
		return "", ""
	}
	tagEnd := strings.Index(seg[open:], ">")
	if tagEnd < 0 {
		return "", ""
	}
	tag := seg[open : open+tagEnd+1]
	closeEnd := strings.Index(strings.ToLower(seg[open+tagEnd:]), "</a>")
	if closeEnd < 0 {
		return "", ""
	}
	text := seg[open+tagEnd+1 : open+tagEnd+closeEnd]
	href = normalizeBaiduHref(attrValue(tag, "href"))
	title = strings.TrimSpace(unescapeEntities(stripTags(text)))
	return href, title
}

func normalizeBaiduHref(href string) string {
	if strings.HasPrefix(href, "/") {
		href = "https://www.baidu.com" + href
	}
	return unwrapSearchRedirect(href)
}

// isBaiduNavURL filters Baidu's own homepage/directory links. The /link
// redirect wrappers are organic results, not navigation, so they stay.
func isBaiduNavURL(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if host == "hao.baidu.com" {
		return true
	}
	if host == "baidu.com" || strings.HasSuffix(host, ".baidu.com") {
		if p := strings.ToLower(u.Path); p == "" || p == "/" || p == "/more" {
			return true
		}
	}
	return false
}

// baiduSnippet reads the abstract of one result block: the classic
// c-abstract div first, then the new content-right_* span markup, then any
// first paragraph.
func baiduSnippet(block string) string {
	if s := snippetAfterToken(block, "c-abstract", "</div>"); s != "" {
		return s
	}
	if s := snippetAfterToken(block, "content-right_", "</span>"); s != "" {
		return s
	}
	return firstParagraph(block)
}

// ParseSogouResults extracts organic results from a Sogou HTML results page.
// Modern results live in vrwrap containers (classic ones in class="rb"),
// and headline anchors point at /link?url= redirect wrappers relative to
// www.sogou.com which are absolutized so a later fetch can follow them.
// Parsing is tolerant: unknown markup yields zero results.
func ParseSogouResults(page string, max int) []SearchResult {
	if max <= 0 {
		max = 5
	}
	var results []SearchResult
	rest := page
	for len(results) < max {
		idx := indexSogouBlock(rest)
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		next := indexSogouBlock(rest[1:])
		block := rest
		if next >= 0 {
			block = rest[:1+next]
			rest = rest[1+next:]
		} else {
			rest = ""
		}
		href, title := sogouTitleAnchor(block)
		if href == "" || title == "" || isSogouNavURL(href) {
			continue
		}
		results = append(results, SearchResult{
			Title:   title,
			URL:     href,
			Snippet: sogouSnippet(block),
		})
	}
	return results
}

func indexSogouBlock(s string) int {
	vr := indexClassBlock(s, "vrwrap")
	rb := indexClassBlock(s, "rb")
	switch {
	case vr < 0:
		return rb
	case rb < 0:
		return vr
	case vr < rb:
		return vr
	default:
		return rb
	}
}

// sogouTitleAnchor returns the headline anchor of one Sogou result block.
func sogouTitleAnchor(block string) (href, title string) {
	h := strings.Index(strings.ToLower(block), "<h3")
	if h < 0 {
		return "", ""
	}
	seg := block[h:]
	open := strings.Index(strings.ToLower(seg), "<a")
	if open < 0 {
		return "", ""
	}
	tagEnd := strings.Index(seg[open:], ">")
	if tagEnd < 0 {
		return "", ""
	}
	tag := seg[open : open+tagEnd+1]
	closeEnd := strings.Index(strings.ToLower(seg[open+tagEnd:]), "</a>")
	if closeEnd < 0 {
		return "", ""
	}
	text := seg[open+tagEnd+1 : open+tagEnd+closeEnd]
	href = normalizeSogouHref(attrValue(tag, "href"))
	title = strings.TrimSpace(unescapeEntities(stripTags(text)))
	return href, title
}

func normalizeSogouHref(href string) string {
	switch {
	case strings.HasPrefix(href, "//"):
		href = "https:" + href
	case strings.HasPrefix(href, "/"):
		href = "https://www.sogou.com" + href
	}
	return unwrapSearchRedirect(href)
}

// isSogouNavURL filters Sogou's own homepage links. The /link redirect
// wrappers are organic results, not navigation, so they stay.
func isSogouNavURL(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if host == "sogou.com" || strings.HasSuffix(host, ".sogou.com") {
		if p := strings.ToLower(u.Path); p == "" || p == "/" || p == "/web" {
			return true
		}
	}
	return false
}

// sogouSnippet reads the abstract of one result block: the modern
// str-text-info div, then the classic fz-mid/space-txt div, then any first
// paragraph.
func sogouSnippet(block string) string {
	if s := snippetAfterToken(block, "str-text-info", "</div>"); s != "" {
		return s
	}
	if s := snippetAfterToken(block, "space-txt", "</div>"); s != "" {
		return s
	}
	return firstParagraph(block)
}

// snippetAfterToken extracts the text of the element opened by the first
// occurrence of token in block up to closeTag, tags stripped and entities
// unescaped. Returns "" when token is absent or the element is empty.
func snippetAfterToken(block, token, closeTag string) string {
	idx := strings.Index(block, token)
	if idx < 0 {
		return ""
	}
	seg := block[idx:]
	gt := strings.Index(seg, ">")
	if gt < 0 {
		return ""
	}
	raw := seg[gt+1:]
	end := strings.Index(raw, closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(unescapeEntities(stripTags(raw[:end])))
}

// firstAttrValue extracts the value of attr from the first tag in s that
// carries it, double- or single-quoted.
func firstAttrValue(s, attr string) string {
	lower := strings.ToLower(s)
	for _, quote := range []byte{'"', '\''} {
		needle := attr + "=" + string(quote)
		idx := strings.Index(lower, needle)
		if idx < 0 {
			continue
		}
		start := idx + len(needle)
		if end := strings.IndexByte(s[start:], quote); end >= 0 {
			return s[start : start+end]
		}
	}
	return ""
}

// SearchAttempt is one keyless HTML search endpoint to try, in fallback
// order, tagged with the source its results are parsed by.
type SearchAttempt struct {
	URL    string
	Source string
}

// containsCJK reports whether s carries Han runes — a query written in
// Chinese almost always originates on a mainland network, where the
// domestic engines are the reliable path and the overseas ones are the
// uncertain fallback.
func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// SearchAttempts returns the fallback ladder for query. A Chinese-language
// query leads with the domestic engines (Baidu, Sogou) because overseas
// engines are the sources that degrade on mainland networks; anything else
// keeps the original overseas order with the domestic engines as last-resort
// tails. The ladder is shared by the tool runtime and the agent-run evidence
// path so both degrade the same way when one source is slow, blocked or
// serving a challenge page.
func SearchAttempts(query string) []SearchAttempt {
	if containsCJK(query) {
		return []SearchAttempt{
			{URL: BaiduSearchURL(query), Source: "baidu"},
			{URL: SogouSearchURL(query), Source: "sogou"},
			{URL: BingCNSearchURL(query), Source: "bing"},
			{URL: SearchURL(query), Source: "duckduckgo"},
			{URL: BingSearchURL(query), Source: "bing"},
		}
	}
	return []SearchAttempt{
		{URL: SearchURL(query), Source: "duckduckgo"},
		{URL: BingCNSearchURL(query), Source: "bing"},
		{URL: BingSearchURL(query), Source: "bing"},
		{URL: BaiduSearchURL(query), Source: "baidu"},
		{URL: SogouSearchURL(query), Source: "sogou"},
	}
}

// ParseSearchSource extracts organic results from a search page body by
// the source tag of the attempt that fetched it.
func ParseSearchSource(source, page string, max int) []SearchResult {
	switch source {
	case "bing":
		return ParseBingResults(page, max)
	case "baidu":
		return ParseBaiduResults(page, max)
	case "sogou":
		return ParseSogouResults(page, max)
	}
	return ParseSearchResults(page, max)
}

// ChallengePage reports whether a search page body is a bot challenge
// (captcha or anomaly interstitial) instead of real results, so callers
// fall through to the next search source instead of parsing garbage.
func ChallengePage(body string) bool {
	// Match challenge markup, not incidental result text mentioning captcha.
	lower := strings.ToLower(body)
	for _, marker := range []string{
		`id="challenge-form"`, `id='challenge-form'`, `id="anomaly-modal"`, `class="anomaly-modal`, `id="b_captcha"`, `id='b_captcha'`,
		// Sogou antispider interstitial (script/asset references plus the
		// verification form).
		`antispider`,
		// Baidu security verification / login-wall interstitials.
		`wappass.baidu.com`, `百度安全验证`,
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
