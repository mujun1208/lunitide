package webfetch

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractTextRejectsUnsupportedContent(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{"application/octet-stream", "image/png", "application/pdf", "application/zip", ""} {
		if _, ok := ExtractText(ct, []byte("data"), 0); ok {
			t.Errorf("content type %q must not extract", ct)
		}
	}
}

func TestExtractTextContentTypes(t *testing.T) {
	t.Parallel()
	for _, ct := range []string{
		"text/plain", "text/html; charset=utf-8", "TEXT/HTML",
		"application/json", "application/xml", "application/xhtml+xml",
	} {
		if _, ok := ExtractText(ct, []byte("data"), 0); !ok {
			t.Errorf("content type %q must extract", ct)
		}
	}
}

func TestExtractHTMLDropsScriptStyleAndCapturesTitle(t *testing.T) {
	t.Parallel()
	html := `<!DOCTYPE html>
<html><head>
<title>  Example &amp; Title  </title>
<style>body { color: red }</style>
<script>alert("xss")</script>
</head><body>
<h1>Header</h1>
<p>First &lt;paragraph&gt; with&nbsp;space.</p>
<div>Second block</div>
<script>var evil = 1;</script>
<noscript>no js text</noscript>
</body></html>`
	out, ok := ExtractText("text/html", []byte(html), 0)
	if !ok {
		t.Fatal("html must extract")
	}
	if out.Title != "Example & Title" {
		t.Fatalf("title=%q", out.Title)
	}
	for _, banned := range []string{"alert", "xss", "color: red", "var evil", "no js text", "<h1>", "</p>"} {
		if strings.Contains(out.Text, banned) {
			t.Fatalf("text contains %q: %q", banned, out.Text)
		}
	}
	for _, want := range []string{"Header", "First <paragraph> with space.", "Second block"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("text missing %q: %q", want, out.Text)
		}
	}
}

func TestExtractTextNumericEntitiesAndUnknownKept(t *testing.T) {
	t.Parallel()
	out, ok := ExtractText("text/html", []byte(`<p>&#65;&#x42; &hellip; &bogus;</p>`), 0)
	if !ok {
		t.Fatal("html must extract")
	}
	if !strings.Contains(out.Text, "AB …") {
		t.Fatalf("numeric entities: %q", out.Text)
	}
	if !strings.Contains(out.Text, "&bogus;") {
		t.Fatalf("unknown entity must stay literal: %q", out.Text)
	}
}

func TestExtractTextTruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()
	// '界' is 3 bytes in UTF-8; a cap landing inside a rune must back off.
	body := []byte(strings.Repeat("ab", 10) + strings.Repeat("界", 10))
	out, ok := ExtractText("text/plain", body, 23)
	if !ok {
		t.Fatal("plain text must extract")
	}
	if !out.Truncated {
		t.Fatal("must report truncation")
	}
	if len(out.Text) > 23 || !utf8.ValidString(out.Text) {
		t.Fatalf("len=%d valid=%v", len(out.Text), utf8.ValidString(out.Text))
	}
	if strings.ContainsRune(out.Text, utf8.RuneError) {
		t.Fatalf("split rune: %q", out.Text)
	}
}

func TestExtractTextInvalidUTF8Sanitized(t *testing.T) {
	t.Parallel()
	out, ok := ExtractText("text/plain", []byte{'a', 0xff, 0xfe, 'b'}, 0)
	if !ok {
		t.Fatal("plain text must extract")
	}
	if !utf8.ValidString(out.Text) {
		t.Fatalf("output not valid UTF-8: %q", out.Text)
	}
}

func TestExtractTextCollapsesWhitespaceAndBlankLines(t *testing.T) {
	t.Parallel()
	out, ok := ExtractText("text/plain", []byte("a  \t b\n\n\n\nc\n   \nd"), 0)
	if !ok {
		t.Fatal("plain text must extract")
	}
	if out.Text != "a b\n\nc\n\nd" {
		t.Fatalf("normalized=%q", out.Text)
	}
}

func TestSearchURLBuildsEscapedQuery(t *testing.T) {
	t.Parallel()
	got := SearchURL(`golang "net http" &more`)
	if !strings.HasPrefix(got, SearchEndpoint+"?q=") {
		t.Fatalf("prefix: %q", got)
	}
	u, err := url.Parse(got)
	if err != nil || u.Query().Get("q") != `golang "net http" &more` {
		t.Fatalf("round trip: %q err=%v", got, err)
	}
}

// TestSearchUserAgentIsBrowserStyle pins the degraded-SERP fix contract: the
// search fetch must present a browser agent, because keyless search frontends
// time out or serve canned unrelated results to non-browser agents.
func TestSearchUserAgentIsBrowserStyle(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(SearchUserAgent, "Mozilla/5.0 (") || !strings.Contains(SearchUserAgent, "AppleWebKit/") {
		t.Fatalf("SearchUserAgent lost its browser identity: %q", SearchUserAgent)
	}
}

const ddgLitePage = `<html><body>
<table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Go &amp; Documentation</a></td></tr>
<tr><td class="result-snippet">The Go <b>programming</b> language</td></tr>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2F">Example</a></td></tr>
<tr><td class="result-snippet">Second result</td></tr>
<tr><td><a class="result-link" href="/settings">internal nav</a></td></tr>
<tr><td><a class="result-link" href="javascript:void(0)">js link</a></td></tr>
</table>
</body></html>`

func TestParseSearchResultsDecodesRedirects(t *testing.T) {
	t.Parallel()
	results := ParseSearchResults(ddgLitePage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d: %+v", len(results), results)
	}
	if results[0].URL != "https://go.dev/doc/" || results[0].Title != "Go & Documentation" {
		t.Fatalf("first=%+v", results[0])
	}
	if results[0].Snippet != "The Go programming language" {
		t.Fatalf("snippet=%q", results[0].Snippet)
	}
	if results[1].URL != "https://example.com/" || results[1].Title != "Example" {
		t.Fatalf("second=%+v", results[1])
	}
}

func TestParseSearchResultsHonorsMax(t *testing.T) {
	t.Parallel()
	results := ParseSearchResults(ddgLitePage, 1)
	if len(results) != 1 || results[0].URL != "https://go.dev/doc/" {
		t.Fatalf("results=%+v", results)
	}
}

func TestParseSearchResultsToleratesMarkupChange(t *testing.T) {
	t.Parallel()
	for _, html := range []string{"", "<html><body>no results here</body></html>", "<a class=\"result-link\""} {
		if got := ParseSearchResults(html, 5); len(got) != 0 {
			t.Fatalf("html %q yielded %+v", html, got)
		}
	}
}

func TestUnwrapSearchRedirectDropsNonWebTargets(t *testing.T) {
	t.Parallel()
	for _, href := range []string{
		"",
		"/relative/path",
		"javascript:alert(1)",
		"ftp://example.com/file",
		"//duckduckgo.com/l/?uddg=javascript%3Aalert(1)",
		"//duckduckgo.com/l/?uddg=",
		"//duckduckgo.com/l/?uddg=%3A%3A%3A",
		"//evil.com/l/?uddg=https%3A%2F%2Fexample.com%2F", // non-DDG wrapper: not unwrapped, passes as https direct? no — scheme-relative to https, host not ddg: kept as-is (https://evil.com/...) which is a valid web URL
	} {
		got := unwrapSearchRedirect(href)
		if href == "//evil.com/l/?uddg=https%3A%2F%2Fexample.com%2F" {
			if got != "https://evil.com/l/?uddg=https%3A%2F%2Fexample.com%2F" {
				t.Errorf("non-DDG absolute link must pass through: %q", got)
			}
			continue
		}
		if got != "" {
			t.Errorf("href %q must be dropped, got %q", href, got)
		}
	}
	if got := unwrapSearchRedirect("https://example.com/direct"); got != "https://example.com/direct" {
		t.Fatalf("direct link: %q", got)
	}
}

const bingPage = `<ol id="b_results">
<li class="b_algo"><h2><a href="https://news.example/jay">周杰伦最新消息</a></h2><p>演唱会与新专辑动态。</p></li>
<li class="b_algo"><h2><a href="javascript:void(0)">skip</a></h2></li>
<li class="b_algo"><h2><a href="https://music.example/jay">官方新闻</a></h2><div class="b_caption"><p>来源可靠。</p></div></li>
</ol>`

func TestParseBingResultsSkipsHomepageNav(t *testing.T) {
	t.Parallel()
	home := `<ol id="b_results">
<li class="b_algo"><h2><a href="https://www.bing.com/">Bing</a></h2><p>homepage</p></li>
<li class="b_algo"><h2><a href="https://www.microsoft.com/">Microsoft</a></h2><p>nav</p></li>
<li class="b_algo"><h2><a href="https://news.example/louis-koo">古天乐新闻</a></h2><p>电影与公开活动。</p></li>
</ol>`
	results := ParseBingResults(home, 10)
	if len(results) != 1 || results[0].URL != "https://news.example/louis-koo" {
		t.Fatalf("want organic news only, got %+v", results)
	}
}

func TestParseBingResultsExtractsOrganicLinks(t *testing.T) {
	t.Parallel()
	results := ParseBingResults(bingPage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d %+v", len(results), results)
	}
	if results[0].URL != "https://news.example/jay" || results[0].Title != "周杰伦最新消息" {
		t.Fatalf("first=%+v", results[0])
	}
	if results[0].Snippet != "演唱会与新专辑动态。" {
		t.Fatalf("snippet=%q", results[0].Snippet)
	}
	if results[1].URL != "https://music.example/jay" {
		t.Fatalf("second=%+v", results[1])
	}
}

// degradedBingPage mirrors the keyless-UA cn.bing.com markup: each b_algo
// block leads with a b_tpcn attribution pill whose anchor text glues the site
// domain to the cite URL, and the caption embeds a b_algoReadMore anchor.
const degradedBingPage = `<ol id="b_results">
<li class="b_algo" data-id iid=SERP.5317><div class="b_tpcn"><a class="tilk" aria-label="baidu.com" href="https://aiqicha.baidu.com/company_detail_62113756741329"><div class="tptxt"><div class="tptt">baidu.com</div><div class="tpmeta"><div class="b_attribution"><cite>https://aiqicha.baidu.com</cite></div></div></div></a></div><h2><a href="https://aiqicha.baidu.com/company_detail_62113756741329">中航材利顿航空科技股份有限公司 - 爱企查</a></h2><div class="b_caption"><p class="b_lineclamp2">3 天之前&ensp;&#0183;&ensp;中航材利顿航空科技股份有限公司是一家高新技术企业。<a class="b_algoReadMore" href="https://aiqicha.baidu.com/company_detail_62113756741329">阅读更多</a></p></div></li>
<li class="b_algo" data-id iid=SERP.5318><div class="b_tpcn"><a class="tilk" href="https://baike.baidu.com/item/x"><div class="tptxt"><div class="tptt">baike.baidu.com</div><div class="tpmeta"><cite>https://baike.baidu.com › item › x</cite></div></div></a></div><h2><a href="https://baike.baidu.com/item/x">中航材利顿航空科技有限公司_百度百科</a></h2><div class="b_caption"><p>成立于2005年。</p></div></li>
</ol>`

func TestParseBingResultsPrefersHeadlineOverAttributionPill(t *testing.T) {
	t.Parallel()
	results := ParseBingResults(degradedBingPage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d %+v", len(results), results)
	}
	first := results[0]
	if first.Title != "中航材利顿航空科技股份有限公司 - 爱企查" {
		t.Fatalf("title=%q", first.Title)
	}
	if first.URL != "https://aiqicha.baidu.com/company_detail_62113756741329" {
		t.Fatalf("url=%q", first.URL)
	}
	// b_algoReadMore must not be mistaken for the next result block: the
	// snippet keeps the text up to </p> instead of being cut empty.
	if !strings.Contains(first.Snippet, "3 天之前") || !strings.Contains(first.Snippet, "高新技术企业") {
		t.Fatalf("snippet=%q", first.Snippet)
	}
	if strings.Contains(first.Snippet, "&ensp;") || strings.Contains(first.Snippet, "&#0183;") {
		t.Fatalf("undecoded entities in snippet=%q", first.Snippet)
	}
	if results[1].Title != "中航材利顿航空科技有限公司_百度百科" {
		t.Fatalf("second title=%q", results[1].Title)
	}
}

func TestRenderSearchHTMLEscapesAndLists(t *testing.T) {
	t.Parallel()
	out := RenderSearchHTML(`jay <script>`, []SearchResult{{Title: `A&B`, URL: "https://ex.test/?q=1", Snippet: "<b>x</b>"}})
	for _, banned := range []string{"<script>", "<b>x</b>"} {
		if strings.Contains(out, banned) {
			t.Fatalf("unescaped %q in %s", banned, out)
		}
	}
	if !strings.Contains(out, "https://ex.test/?q=1") || !strings.Contains(out, "A&amp;B") {
		t.Fatalf("missing escaped content: %s", out)
	}
}

func TestSearchAttemptsLadderOrder(t *testing.T) {
	t.Parallel()
	attempts := SearchAttempts("hello world")
	if len(attempts) != 5 {
		t.Fatalf("attempts=%d: %+v", len(attempts), attempts)
	}
	if attempts[0].Source != "duckduckgo" || attempts[0].URL != SearchURL("hello world") {
		t.Fatalf("first=%+v", attempts[0])
	}
	if attempts[1].Source != "bing" || attempts[1].URL != BingCNSearchURL("hello world") {
		t.Fatalf("second=%+v", attempts[1])
	}
	if attempts[2].Source != "bing" || attempts[2].URL != BingSearchURL("hello world") {
		t.Fatalf("third=%+v", attempts[2])
	}
	// Overseas-first ladder keeps the domestic engines as last-resort tails.
	if attempts[3].Source != "baidu" || attempts[3].URL != BaiduSearchURL("hello world") {
		t.Fatalf("fourth=%+v", attempts[3])
	}
	if attempts[4].Source != "sogou" || attempts[4].URL != SogouSearchURL("hello world") {
		t.Fatalf("fifth=%+v", attempts[4])
	}
}

// TestSearchAttemptsChineseQueryLeadsWithDomesticEngines pins the
// determinism fix for mainland queries: when the query is written in
// Chinese, Baidu and Sogou answer first so an overseas network failure
// cannot zero out the search, and the overseas engines become fallbacks.
func TestSearchAttemptsChineseQueryLeadsWithDomesticEngines(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"中航材利顿公司信息", "查一下 公司 背景", "公司 info"} {
		attempts := SearchAttempts(query)
		if len(attempts) != 5 {
			t.Fatalf("query %q attempts=%d: %+v", query, len(attempts), attempts)
		}
		if attempts[0].Source != "baidu" || attempts[0].URL != BaiduSearchURL(query) {
			t.Fatalf("query %q first=%+v", query, attempts[0])
		}
		if attempts[1].Source != "sogou" || attempts[1].URL != SogouSearchURL(query) {
			t.Fatalf("query %q second=%+v", query, attempts[1])
		}
		var sawOverseas bool
		for _, a := range attempts[2:] {
			if a.Source == "duckduckgo" || a.Source == "bing" {
				sawOverseas = true
			}
		}
		if !sawOverseas {
			t.Fatalf("query %q lost overseas fallbacks: %+v", query, attempts)
		}
	}
}

const baiduPage = `<div id="content_left">
<div class="result c-container new-pmd" srcid="1599" tpl="se_com_default" mu="https://news.example/zhounian">
<h3 class="c-title t"><a href="https://www.baidu.com/link?url=aabb" target="_blank">公司<em>周年</em>报道</a></h3>
<div class="c-abstract">成立于2005年的<em>高新技术企业</em>，主营航空材料。</div>
</div>
<div class="result result-op c-container" mu="https://baike.baidu.com/item/x">
<h3 class="t c-title"><a href="/link?url=ccdd">公司_百度百科</a></h3>
<div class="c-abstract">百科词条收录企业基本信息。</div>
</div>
<div class="result c-container"><h3 class="c-title"><a href="https://www.baidu.com/">百度首页</a></h3><div class="c-abstract">nav</div></div>
</div>`

func TestParseBaiduResultsExtractsOrganicLinks(t *testing.T) {
	t.Parallel()
	results := ParseBaiduResults(baiduPage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d %+v", len(results), results)
	}
	// mu carries the real target URL, not the /link redirect wrapper.
	if results[0].URL != "https://news.example/zhounian" || results[0].Title != "公司周年报道" {
		t.Fatalf("first=%+v", results[0])
	}
	if results[0].Snippet != "成立于2005年的高新技术企业，主营航空材料。" {
		t.Fatalf("snippet=%q", results[0].Snippet)
	}
	// Site-relative /link hrefs are absolutized so a later fetch can follow
	// them when mu is absent... here mu is present, so the wrapper must not win.
	if results[1].URL != "https://baike.baidu.com/item/x" || results[1].Title != "公司_百度百科" {
		t.Fatalf("second=%+v", results[1])
	}
}

// baiduWrapperPage has no mu attributes: the /link redirect wrapper is the
// only URL each result carries, so it must pass through absolutized.
const baiduWrapperPage = `<div id="content_left">
<div class="result c-container"><h3 class="c-title t"><a href="/link?url=eeff">标题甲</a></h3><div class="c-abstract">摘要甲</div></div>
<div class="result c-container"><h3 class="c-title t"><a href="https://www.baidu.com/link?url=gghh">标题乙</a></h3><span class="content-right_8Zs40">摘要乙</span></div>
</div>`

func TestParseBaiduResultsFallsBackToLinkWrappers(t *testing.T) {
	t.Parallel()
	results := ParseBaiduResults(baiduWrapperPage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d %+v", len(results), results)
	}
	if results[0].URL != "https://www.baidu.com/link?url=eeff" || results[0].Title != "标题甲" {
		t.Fatalf("first=%+v", results[0])
	}
	if results[1].URL != "https://www.baidu.com/link?url=gghh" || results[1].Title != "标题乙" {
		t.Fatalf("second=%+v", results[1])
	}
	if results[1].Snippet != "摘要乙" {
		t.Fatalf("new-markup snippet=%q", results[1].Snippet)
	}
}

func TestParseBaiduResultsToleratesMarkupChange(t *testing.T) {
	t.Parallel()
	for _, page := range []string{"", "<html><body>无结果</body></html>", `<div class="resultList"><p>别的东西</p></div>`} {
		if got := ParseBaiduResults(page, 5); len(got) != 0 {
			t.Fatalf("page %q yielded %+v", page, got)
		}
	}
}

const sogouPage = `<div class="results">
<div class="vrwrap">
<h3 class="vr-title"><a id="uigs_1" href="/link?url=rr1" target="_blank">企业<em>信用</em>报告</a></h3>
<div class="str-text-info"><p class="str_info">公开的企业工商信息聚合。</p></div>
</div>
<div class="vrwrap">
<h3 class="vr-title"><a href="//www.sogou.com/link?url=rr2">另一条结果</a></h3>
<div class="fz-mid space-txt">第二条摘要。</div>
</div>
<div class="rb"><h3 class="pt"><a href="https://www.sogou.com/">搜狗首页</a></h3></div>
</div>`

func TestParseSogouResultsExtractsOrganicLinks(t *testing.T) {
	t.Parallel()
	results := ParseSogouResults(sogouPage, 10)
	if len(results) != 2 {
		t.Fatalf("results=%d %+v", len(results), results)
	}
	if results[0].URL != "https://www.sogou.com/link?url=rr1" || results[0].Title != "企业信用报告" {
		t.Fatalf("first=%+v", results[0])
	}
	if results[0].Snippet != "公开的企业工商信息聚合。" {
		t.Fatalf("snippet=%q", results[0].Snippet)
	}
	if results[1].URL != "https://www.sogou.com/link?url=rr2" || results[1].Title != "另一条结果" {
		t.Fatalf("second=%+v", results[1])
	}
	if results[1].Snippet != "第二条摘要。" {
		t.Fatalf("classic snippet=%q", results[1].Snippet)
	}
}

func TestParseSogouResultsToleratesMarkupChange(t *testing.T) {
	t.Parallel()
	for _, page := range []string{"", "<html><body>无结果</body></html>"} {
		if got := ParseSogouResults(page, 5); len(got) != 0 {
			t.Fatalf("page %q yielded %+v", page, got)
		}
	}
}

func TestParseSearchSourceDispatchesBySource(t *testing.T) {
	t.Parallel()
	ddg := ParseSearchSource("duckduckgo", ddgLitePage, 10)
	if len(ddg) != 2 || ddg[0].URL != "https://go.dev/doc/" {
		t.Fatalf("duckduckgo=%+v", ddg)
	}
	bing := ParseSearchSource("bing", bingPage, 10)
	if len(bing) != 2 || bing[0].URL != "https://news.example/jay" {
		t.Fatalf("bing=%+v", bing)
	}
	baidu := ParseSearchSource("baidu", baiduPage, 10)
	if len(baidu) != 2 || baidu[0].URL != "https://news.example/zhounian" {
		t.Fatalf("baidu=%+v", baidu)
	}
	sogou := ParseSearchSource("sogou", sogouPage, 10)
	if len(sogou) != 2 || sogou[0].URL != "https://www.sogou.com/link?url=rr1" {
		t.Fatalf("sogou=%+v", sogou)
	}
	// Unknown source tags fall back to the DuckDuckGo parser.
	if got := ParseSearchSource("other", ddgLitePage, 10); len(got) != 2 {
		t.Fatalf("fallback=%+v", got)
	}
}

func TestChallengePageDetectsBotInterstitials(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`<html><form id="challenge-form">prove you are human</form></html>`,
		`<html><form id='challenge-form'>prove you are human</form></html>`,
		`<html><div class="anomaly-modal">blocked</div></html>`,
		`<html><div id="b_captcha">captcha</div></html>`,
		// Sogou antispider interstitial.
		`<html><script src="https://www.sogou.com/antispider/script.js"></script></html>`,
		// Baidu security verification interstitial.
		`<html><head><title>百度安全验证</title></head><body>请输入验证码</body></html>`,
		`<html><a href="https://wappass.baidu.com/captcha/login">验证</a></html>`,
	} {
		if !ChallengePage(body) {
			t.Errorf("challenge not detected: %q", body)
		}
	}
	for _, body := range []string{
		"",
		`<html><body>results that merely mention captcha bypass</body></html>`,
		ddgLitePage,
		bingPage,
		baiduPage,
		sogouPage,
	} {
		if ChallengePage(body) {
			t.Errorf("false positive on %q", body)
		}
	}
}
