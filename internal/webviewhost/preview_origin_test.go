package webviewhost

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The preview origin exists so a generated page can run its own scripts. That
// makes its URL parser a security boundary, not a convenience: a ticket names a
// folder of the user's workspace, and anything that escapes the folder or forges
// a ticket reads files it was never granted. Table-drive the rejections.
func TestParsePreviewRequestRejectsEverythingButOurOwnShape(t *testing.T) {
	const token = "PzQ1c2VydGlja2V0MDAwMDAx"
	base := PreviewOrigin + PreviewPathPrefix + token
	for _, raw := range []string{
		"http://" + PreviewVirtualHost + PreviewPathPrefix + token + "/index.html",        // plain http
		"https://" + PreviewVirtualHost + ".evil" + PreviewPathPrefix + token + "/x.html", // host suffix
		"https://user:pass@" + PreviewVirtualHost + PreviewPathPrefix + token + "/x.html", // userinfo
		"https://" + PreviewVirtualHost + ":444" + PreviewPathPrefix + token + "/x.html",  // port
		PreviewOrigin + "/other/" + token + "/index.html",                                 // wrong prefix
		PreviewOrigin + PreviewPathPrefix + "short/index.html",                            // token too short
		PreviewOrigin + PreviewPathPrefix + token + "!/index.html",                        // token charset
		base + "/../../etc/hosts",                                                // traversal
		base + "/..%2f..%2fsecrets.env",                                          // encoded traversal
		base + "/%2e%2e/%2e%2e/secrets.env",                                      // encoded dot-dot
		base + "/sub/../../outside.html",                                         // traversal after descent
		base + "//etc/hosts",                                                     // empty segment
		base + "/C:/Windows/win.ini",                                             // drive-ish absolute
		base + "/assets\\app.js",                                                 // backslash
		base + "/" + strings.Repeat("a", previewRelMax+1),                        // absurd length
		"https://app.lunitide.local" + PreviewPathPrefix + token + "/index.html", // app origin
	} {
		if _, _, ok := ParsePreviewRequest(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestParsePreviewRequestAcceptsDocumentAndSiblingAssets(t *testing.T) {
	const token = "PzQ1c2VydGlja2V0MDAwMDAx"
	base := PreviewOrigin + PreviewPathPrefix + token
	cases := map[string]string{
		base + "/":                             "",
		base:                                   "",
		base + "/index.html":                   "index.html",
		base + "/assets/app.js":                "assets/app.js",
		base + "/assets/./app.js":              "assets/app.js",
		base + "/sub/deep/../style.css":        "sub/style.css",
		base + "/index.html?tab=customers":     "index.html",
		base + "/?view=dashboard":              "",
		base + "/data/%E5%AE%A2%E6%88%B7.json": "data/客户.json",
	}
	for raw, wantRel := range cases {
		gotToken, rel, ok := ParsePreviewRequest(raw)
		if !ok {
			t.Errorf("rejected %q", raw)
			continue
		}
		if gotToken != token {
			t.Errorf("%q token = %q, want %q", raw, gotToken, token)
		}
		if rel != wantRel {
			t.Errorf("%q rel = %q, want %q", raw, rel, wantRel)
		}
	}
}

// A page that can run scripts must not be able to send anything anywhere, and it
// must not be able to grant itself more than the host allowed. Both properties
// live in this header string, so pin them.
func TestPreviewResponseHeadersCannotBeWidenedByTheDocument(t *testing.T) {
	headers := PreviewResponseHeaders("text/html; charset=utf-8")
	for _, want := range []string{
		"Content-Type: text/html; charset=utf-8",
		"X-Content-Type-Options: nosniff",
		"Cache-Control: no-store",
		"Content-Security-Policy: ",
	} {
		if !strings.Contains(headers, want) {
			t.Errorf("headers missing %q:\n%s", want, headers)
		}
	}
	// Exfiltration is the risk that matters once scripts run: no network sink of
	// any kind, and no form post either.
	for _, want := range []string{"connect-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-src 'none'", "object-src 'none'"} {
		if !strings.Contains(PreviewContentPolicy, want) {
			t.Errorf("policy missing %q: %s", want, PreviewContentPolicy)
		}
	}
	// And it may only ever be framed by us.
	if !strings.Contains(PreviewContentPolicy, "frame-ancestors "+TrustedOrigin) {
		t.Errorf("policy does not pin frame-ancestors: %s", PreviewContentPolicy)
	}
	// The whole point of the origin: scripts and inline styles are permitted.
	if !strings.Contains(PreviewContentPolicy, "script-src 'self' 'unsafe-inline'") || !strings.Contains(PreviewContentPolicy, "style-src 'self' 'unsafe-inline'") {
		t.Errorf("policy does not let the page behave like a page: %s", PreviewContentPolicy)
	}
	// Headers are CRLF-joined for CreateWebResourceResponse; a bare LF would
	// merge two headers into one and silently drop the policy.
	if strings.Contains(strings.ReplaceAll(headers, "\r\n", ""), "\n") {
		t.Errorf("headers contain a bare LF:\n%q", headers)
	}
}

func TestPreviewContentTypeNeverGuessesExecutable(t *testing.T) {
	if got := PreviewContentType("app.js"); got != "text/javascript; charset=utf-8" {
		t.Errorf("app.js = %q", got)
	}
	if got := PreviewContentType("index.HTML"); got != "text/html; charset=utf-8" {
		t.Errorf("index.HTML = %q", got)
	}
	// Unknown extensions must not be sniffable into markup or script.
	for _, name := range []string{"data.bin", "archive.zip", "noext"} {
		if got := PreviewContentType(name); got != "application/octet-stream" {
			t.Errorf("%s = %q, want octet-stream", name, got)
		}
	}
}

func TestPreviewDocumentURLRoundTrips(t *testing.T) {
	const token = "PzQ1c2VydGlja2V0MDAwMDAx"
	url := PreviewDocumentURL(token, "reports/index.html")
	gotToken, rel, ok := ParsePreviewRequest(url)
	if !ok || gotToken != token || rel != "reports/index.html" {
		t.Fatalf("round trip failed: %q -> %q %q %v", url, gotToken, rel, ok)
	}
	if PreviewDocumentURL("", "index.html") != "" {
		t.Error("a missing ticket must not produce a URL")
	}
}

// frame-ancestors pins the preview inside our document, and the shell's own
// frame-src decides whether it may be framed at all. Both have to agree, and they
// live in different languages in different trees, so assert them together.
func TestShellPolicyFramesThePreviewOrigin(t *testing.T) {
	shell, err := os.ReadFile(filepath.Join("..", "..", "web", "index.html"))
	if err != nil {
		t.Skipf("renderer shell not present: %v", err)
	}
	policy := regexp.MustCompile(`<meta http-equiv="Content-Security-Policy" content="([^"]+)"`).FindSubmatch(shell)
	if policy == nil {
		t.Fatal("web/index.html lost its Content-Security-Policy meta tag")
	}
	frameSrc := ""
	for _, part := range strings.Split(string(policy[1]), ";") {
		if part = strings.TrimSpace(part); strings.HasPrefix(part, "frame-src ") {
			frameSrc = part
		}
	}
	if !strings.Contains(frameSrc, PreviewOrigin) {
		t.Fatalf("shell frame-src does not allow the preview origin (%s): %q", PreviewOrigin, frameSrc)
	}
}

// Only our application document may pull from this origin.
func TestPreviewRequestAllowedRequiresTrustedSource(t *testing.T) {
	const token = "PzQ1c2VydGlja2V0MDAwMDAx"
	uri := PreviewDocumentURL(token, "index.html")
	if !PreviewRequestAllowed(TrustedOrigin+"/", uri) {
		t.Error("trusted application document was refused")
	}
	for _, source := range []string{"https://evil.example/", "http://app.lunitide.local/", PreviewOrigin + "/", ""} {
		if PreviewRequestAllowed(source, uri) {
			t.Errorf("accepted source %q", source)
		}
	}
}

func TestInjectPreviewCiteOnceBeforeBodyEnd(t *testing.T) {
	page := []byte("<html><body><button onclick=\"save()\">保存</button></body></html>")
	out := injectPreviewCite(page)
	if !bytes.Contains(out, []byte("data-lunitide-cite")) || !bytes.Contains(out, []byte("lunitide-preview")) {
		t.Fatalf("cite bridge missing: %s", out)
	}
	if !bytes.HasSuffix(bytes.ToLower(out), []byte("</body></html>")) {
		t.Fatalf("script landed after the document: %s", out)
	}
	again := injectPreviewCite(out)
	if bytes.Count(again, []byte("data-lunitide-cite")) != 1 {
		t.Fatal("cite script was injected twice")
	}
	if bytes.Count(again, []byte("data-lunitide-boot")) != 1 {
		t.Fatal("storage boot script was injected twice")
	}
}

func TestPreviewBootRunsBeforeThePageScript(t *testing.T) {
	page := []byte("<html><head><title>NexaCRM</title></head><body><nav id=\"nav\"></nav><script>load();render();</script></body></html>")
	out := injectPreviewCite(page)
	boot := bytes.Index(out, []byte("data-lunitide-boot"))
	pageScript := bytes.Index(out, []byte("load();render();"))
	if boot < 0 || pageScript < 0 || boot > pageScript {
		t.Fatalf("boot=%d pageScript=%d", boot, pageScript)
	}
	if !bytes.Contains(out, []byte("localStorage")) {
		t.Fatal("boot script does not guard localStorage")
	}
}
