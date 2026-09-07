package videounderstand

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestCaptionFormatsRejectErrorBodiesAndKeepActualCues(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, contentType, body, want string }{
		{"api-error", "application/json", `{"code":-403,"message":"请登录后再试"}`, ""},
		{"html-captcha", "text/html", `<html><title>验证码</title><body>请验证</body></html>`, ""},
		{"mislabeled-html", "text/plain", `<html><body>not subtitles</body></html>`, ""},
		{"json-body", "application/json", `{"body":[{"content":"第一句"},{"content":"第二句"}]}`, "第一句\n第二句"},
		{"xml-transcript", "text/xml", `<transcript><text start="0">Tom &amp; Jerry</text><text start="1">下一句</text></transcript>`, "Tom & Jerry\n下一句"},
		{"ttml", "application/ttml+xml", `<tt xmlns="http://www.w3.org/ns/ttml"><body><div><p begin="0s"><span>真实</span>字幕<br/>换行</p></div></body></tt>`, "真实字幕\n换行"},
		{"webvtt", "text/vtt", "WEBVTT\n\nNOTE editor metadata\nnot spoken\n\n1\n00:00:00.000 --> 00:00:01.000\n第一句\n\n2\n00:00:01.000 --> 00:00:02.000\n<v speaker>第二句</v>\n", "第一句\n第二句"},
		{"srt", "text/plain", "1\n00:00:00,000 --> 00:00:01,000\n123 是正文\n\n2\n00:00:01,000 --> 00:00:02,000\n下一句\n", "123 是正文\n下一句"},
		{"broken-xml", "text/xml", `<transcript><text>unfinished`, ""},
		{"unknown-binary", "application/octet-stream", "not a supported subtitle document", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := ParseCaptions(tc.contentType, []byte(tc.body))
			if got != tc.want || truncated {
				t.Fatalf("got %q truncated=%v, want %q", got, truncated, tc.want)
			}
		})
	}
}

func TestUnderstandCaptionFailureDoesNotBecomeVideoEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, finalURL, contentType, body, reason string
		status                                    int
	}{
		{"status", "https://aisubtitle.hdslb.com/sub.json", "application/json", `[{"content":"must not trust a 403"}]`, "http_403", 403},
		{"error-json", "https://aisubtitle.hdslb.com/sub.json", "application/json", `{"code":-403,"message":"login required"}`, "no_readable_captions", 200},
		{"login-html", "https://aisubtitle.hdslb.com/sub.json", "text/html", `<title>登录</title>`, "no_readable_captions", 200},
		{"redirect", "https://example.com/error.json", "application/json", `[{"content":"offsite"}]`, "unsupported_host", 200},
		{"redirect-scheme", "ftp://aisubtitle.hdslb.com/sub.json", "application/json", `[{"content":"ftp"}]`, "unsupported_host", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Understand(context.Background(), "https://www.bilibili.com/video/BVfixture", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
				if strings.Contains(raw, "hdslb") {
					return networkpolicy.FetchResult{FinalURL: tc.finalURL, Status: tc.status, ContentType: tc.contentType, Body: []byte(tc.body)}, nil
				}
				return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/html", Body: []byte(readTestdata(t, "captions_bilibili.html"))}, nil
			})
			if !got.OK || got.Source != "page_meta" || got.Captions != "" || got.CaptionReason != tc.reason {
				t.Fatalf("bad subtitle must fall back honestly to metadata: %+v", got)
			}
			if !strings.Contains(got.Format(), "没有公开字幕") || !strings.Contains(got.Format(), "captionsUnavailable: "+tc.reason) {
				t.Fatal(got.Format())
			}
		})
	}
}

func TestUnderstandHTTPErrorCannotBecomePageMetadata(t *testing.T) {
	t.Parallel()
	got := Understand(context.Background(), "https://v.douyin.com/error", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 429, Body: []byte(`<title>服务忙</title>`)}, nil
	})
	if got.OK || got.Reason != "http_429" || got.Title != "" {
		t.Fatalf("HTTP error: %+v", got)
	}
}

func TestUnderstandSharesDeadlineAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	var first time.Time
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := Understand(ctx, "https://www.bilibili.com/video/BVfixture", func(ctx context.Context, raw string) (networkpolicy.FetchResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("unbounded video fetch")
		}
		calls++
		if calls == 1 {
			first = deadline
			return networkpolicy.FetchResult{FinalURL: raw, Status: 200, Body: []byte(readTestdata(t, "captions_bilibili.html"))}, nil
		}
		if !deadline.Equal(first) {
			t.Fatal("subtitle restarted timeout")
		}
		return networkpolicy.FetchResult{}, context.DeadlineExceeded
	})
	if calls != 2 || got.CaptionReason != "timeout" {
		t.Fatalf("calls=%d result=%+v", calls, got)
	}
	cancel()
	got = Understand(ctx, "https://v.douyin.com/cancelled", func(context.Context, string) (networkpolicy.FetchResult, error) {
		t.Fatal("fetch called after cancellation")
		return networkpolicy.FetchResult{}, nil
	})
	if got.Reason != "cancelled" {
		t.Fatal(got.Reason)
	}
}

func TestCaptionTruncationPreservesUTF8AndTransportFlag(t *testing.T) {
	t.Parallel()
	text, trunc := ParseCaptions("text/plain", []byte(strings.Repeat("字", MaxCaptionBytes)))
	if !trunc || len(text) > MaxCaptionBytes || !utf8.ValidString(text) {
		t.Fatal("invalid bound")
	}
	got, err := fetchCaption(context.Background(), "https://aisubtitle.hdslb.com/sub.txt", func(_ context.Context, raw string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "text/plain", Body: []byte("partial caption"), Truncated: true}, nil
	})
	if err != nil || !got.truncated {
		t.Fatalf("truncated lost: %+v %v", got, err)
	}
}
