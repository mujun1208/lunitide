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
	if strings.Contains(got.Format(), "我看完了") {
		t.Fatal("must not claim watched")
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
