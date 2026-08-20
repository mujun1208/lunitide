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
