package videounderstand

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestUnderstandPageMetaAndCaptions(t *testing.T) {
	t.Parallel()
	og := readTestdata(t, "og_only.html")
	got := Understand(context.Background(), "https://www.bilibili.com/video/BV1xx", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		if strings.Contains(raw, "hdslb.com") {
			t.Fatal("og-only page must not fetch captions")
		}
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/html", Body: []byte(og)}, nil
	})
	if !got.OK || got.Source != "page_meta" || got.Title != "月食观测指南" {
		t.Fatalf("page_meta = %+v", got)
	}
	if strings.Contains(got.Format(), "我看完了") || !strings.Contains(got.Format(), "根据标题和简介") {
		t.Fatalf("page_meta must speak from 简介: %q", got.Format())
	}

	html := readTestdata(t, "captions_bilibili.html")
	got = Understand(context.Background(), "https://www.bilibili.com/video/BV1cap", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		if strings.Contains(raw, "aisubtitle.hdslb.com") {
			return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: []byte(`[{"content":"字幕一句"}]`)}, nil
		}
		return networkpolicy.FetchResult{FinalURL: "https://www.bilibili.com/video/BV1cap", Status: 200, ContentType: "text/html", Body: []byte(html)}, nil
	})
	if !got.OK || got.Source != "mixed" || !strings.Contains(got.Captions, "字幕一句") {
		t.Fatalf("captions = %+v", got)
	}
}

func TestUnderstandEmptyAndLogin(t *testing.T) {
	t.Parallel()
	empty := Understand(context.Background(), "https://v.douyin.com/empty", fetchHTML(readTestdata(t, "empty_spa.html")))
	if empty.OK || empty.Reason != "empty_page" {
		t.Fatalf("empty = %+v", empty)
	}
	if !strings.Contains(empty.Format(), "不能假装看完") {
		t.Fatalf("empty format = %q", empty.Format())
	}
	wall := Understand(context.Background(), "https://www.bilibili.com/video/BVlogin", fetchHTML(readTestdata(t, "login_wall.html")))
	if wall.OK || wall.Reason != "login_wall" {
		t.Fatalf("login = %+v", wall)
	}
	titled := Understand(context.Background(), "https://www.douyin.com/video/1", fetchHTML(
		`<!doctype html><html><head><title>登录 - 抖音</title></head><body><p>请登录后继续观看</p></body></html>`,
	))
	if titled.OK || titled.Reason != "login_wall" {
		t.Fatalf("titled login wall must not become page_meta: %+v", titled)
	}
	if !strings.Contains(titled.Format(), "请用户贴文案") || strings.Contains(titled.Format(), "我看完了") && !strings.Contains(titled.Format(), "禁止") {
		t.Fatalf("login wall format = %q", titled.Format())
	}
}

func TestUnderstandFourPlatformsPageMetaSpeakHint(t *testing.T) {
	t.Parallel()
	pages := []struct {
		url  string
		html string
	}{
		{"https://www.bilibili.com/video/BV1xx", `<meta property="og:title" content="B站标题"><meta property="og:description" content="B站简介">`},
		{"https://www.douyin.com/video/1", `<meta property="og:title" content="抖音标题"><meta property="og:description" content="抖音简介">`},
		{"https://v.qq.com/x/page/n001.html", `<meta property="og:title" content="腾讯标题"><meta property="og:description" content="腾讯简介">`},
		{"https://www.youtube.com/watch?v=dQw4w9wgGc", `<meta property="og:title" content="YT标题"><meta property="og:description" content="YT简介">`},
	}
	for _, tc := range pages {
		got := Understand(context.Background(), tc.url, fetchHTML(tc.html))
		if !got.OK || got.Source != "page_meta" {
			t.Fatalf("%s = %+v", tc.url, got)
		}
		out := got.Format()
		if !strings.Contains(out, "根据标题和简介") || strings.Contains(out, "我看完了") {
			t.Fatalf("%s format=%q", tc.url, out)
		}
	}
}

func TestUnderstandRedirectGuards(t *testing.T) {
	t.Parallel()
	ssrf := Understand(context.Background(), "https://v.douyin.com/x", func(context.Context, string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{}, &networkpolicy.Error{Code: networkpolicy.CodeSSRFBlocked, Op: "validate fetch URL"}
	})
	if ssrf.OK || ssrf.Reason != "ssrf_blocked" {
		t.Fatalf("ssrf = %+v", ssrf)
	}
	off := Understand(context.Background(), "https://v.douyin.com/x", func(context.Context, string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: "https://example.com/watch", Status: 200, ContentType: "text/html", Body: []byte(`<title>x</title>`)}, nil
	})
	if off.OK || off.Reason != "unsupported_host" {
		t.Fatalf("off-allowlist = %+v", off)
	}
	degrade := Understand(context.Background(), "https://www.bilibili.com/video/BVcap", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		if strings.Contains(raw, "evil.example") {
			t.Fatal("must not follow off-allowlist caption host")
		}
		html := `<script>window.__INITIAL_STATE__={"videoData":{"title":"有简介","desc":"d","subtitle":{"list":[{"subtitle_url":"https://evil.example/sub.json"}]}}};</script>`
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/html", Body: []byte(html)}, nil
	})
	if !degrade.OK || degrade.Source != "page_meta" || degrade.Captions != "" {
		t.Fatalf("caption host miss should degrade to page_meta: %+v", degrade)
	}
}

func fetchHTML(body string) FetchFunc {
	return func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/html", Body: []byte(body)}, nil
	}
}
