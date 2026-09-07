package videounderstand

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

type FetchFunc func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)

const (
	understandTimeout = 30 * time.Second
	maxPageBytes      = 1 << 20
)

type Result struct {
	OK                bool
	Platform          Platform
	Source            string
	Reason            string
	Title             string
	Author            string
	FinalURL          string
	Description       string
	Captions          string
	CaptionReason     string
	CaptionsTruncated bool
	CoverDescribed    bool
	Disclaimer        string
}

func (r Result) Format() string {
	var b strings.Builder
	if r.OK {
		b.WriteString("ok: true\n")
	} else {
		b.WriteString("ok: false\n")
	}
	if r.Platform != "" {
		b.WriteString("platform: " + string(r.Platform) + "\n")
	}
	if r.Source != "" {
		b.WriteString("source: " + r.Source + "\n")
	}
	if r.Reason != "" {
		b.WriteString("reason: " + r.Reason + "\n")
	}
	if r.CaptionReason != "" {
		b.WriteString("captionsUnavailable: " + r.CaptionReason + "\n")
	}
	if r.Title != "" {
		b.WriteString("title: " + r.Title + "\n")
	}
	if r.Author != "" {
		b.WriteString("author: " + r.Author + "\n")
	}
	if r.FinalURL != "" {
		b.WriteString("finalUrl: " + r.FinalURL + "\n")
	}
	if r.CaptionsTruncated {
		b.WriteString("captionsTruncated: true\n")
	}
	if r.CoverDescribed {
		b.WriteString("coverDescribed: true\n")
	}
	disclaimer := r.Disclaimer
	if disclaimer == "" {
		disclaimer = Disclaimer
	}
	b.WriteString("disclaimer: " + disclaimer + "\n")
	if r.Description != "" {
		b.WriteString("\n根据标题和简介：\n" + r.Description + "\n")
	}
	if r.Captions == "" && r.OK {
		b.WriteString("\n根据标题和简介整理，没有公开字幕。禁止假装看完全片。\n")
	}
	if r.Captions != "" {
		b.WriteString("\n公开字幕：\n" + r.Captions + "\n")
	}
	if !r.OK && r.Reason == "empty_page" {
		b.WriteString("\n页面没有公开字幕或简介，不能假装看完视频。请用户贴文案或换一条能打开的链接。\n")
	}
	if !r.OK && (r.Reason == "login_wall" || r.Reason == "captcha") {
		b.WriteString("\n需要登录或验证码，不要改用浏览器代点。请用户贴文案。\n")
	}
	return b.String()
}

func Understand(ctx context.Context, rawURL string, fetch FetchFunc) Result {
	// One budget covers the page and its subtitle, including redirects. Do not
	// restart a fresh timeout after a slow page fetch.
	ctx, cancel := context.WithTimeout(ctx, understandTimeout)
	defer cancel()
	canon, plat, ok := ClassifyShareURL(rawURL)
	if !ok {
		return fail("", "unsupported_host", "")
	}
	if fetch == nil {
		return fail(plat, "fetch_failed", canon)
	}
	if err := ctx.Err(); err != nil {
		return fail(plat, fetchFailureReason(err), canon)
	}
	page, err := fetch(ctx, canon)
	if err != nil {
		return fail(plat, fetchFailureReason(err), canon)
	}
	final := strings.TrimSpace(page.FinalURL)
	if final == "" {
		final = canon
	}
	if _, _, shareOK := ClassifyShareURL(final); !shareOK {
		return fail(plat, "unsupported_host", final)
	}
	if page.Status < 200 || page.Status >= 300 {
		return fail(plat, fmt.Sprintf("http_%d", page.Status), final)
	}
	if len(page.Body) > maxPageBytes {
		page.Body = page.Body[:maxPageBytes]
	}
	parsed := ParseHTML(string(page.Body))
	out := Result{
		Platform:    plat,
		Title:       truncateUTF8(parsed.Title, 1024),
		Author:      truncateUTF8(parsed.Author, 512),
		FinalURL:    final,
		Description: truncateUTF8(parsed.Description, 16<<10),
		Disclaimer:  Disclaimer,
	}
	if parsed.CaptionURL != "" {
		if capRes, capErr := fetchCaption(ctx, parsed.CaptionURL, fetch); capErr == nil {
			out.Captions = capRes.text
			out.CaptionsTruncated = capRes.truncated
		} else {
			out.CaptionReason = capErr.Error()
		}
	}
	out.Source, out.Reason, out.OK = classifySource(parsed, out.Captions)
	return out
}

func classifySource(page Page, captions string) (source, reason string, ok bool) {
	hasMeta := page.Title != "" || page.Description != ""
	hasCap := captions != ""
	switch {
	case hasCap && hasMeta:
		return "mixed", "", true
	case hasCap:
		return "captions", "", true
	case page.LoginWall:
		return "empty", "login_wall", false
	case page.Captcha:
		return "empty", "captcha", false
	case hasMeta:
		return "page_meta", "", true
	default:
		return "empty", "empty_page", false
	}
}

type captionFetch struct {
	text      string
	truncated bool
}

func fetchCaption(ctx context.Context, raw string, fetch FetchFunc) (captionFetch, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return captionFetch{}, errors.New("invalid_url")
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if !validCaptionURL(u) {
		return captionFetch{}, errors.New("unsupported_host")
	}
	page, err := fetch(ctx, u.String())
	if err != nil {
		return captionFetch{}, errors.New(fetchFailureReason(err))
	}
	if page.FinalURL != "" {
		final, err := url.Parse(page.FinalURL)
		if err != nil || !validCaptionURL(final) {
			return captionFetch{}, errors.New("unsupported_host")
		}
	}
	if page.Status < 200 || page.Status >= 300 {
		return captionFetch{}, fmt.Errorf("http_%d", page.Status)
	}
	if len(page.Body) > maxPageBytes {
		page.Body = page.Body[:maxPageBytes]
		page.Truncated = true
	}
	text, trunc := ParseCaptions(page.ContentType, page.Body)
	if text == "" {
		return captionFetch{}, errors.New("no_readable_captions")
	}
	return captionFetch{text: text, truncated: trunc || page.Truncated}, nil
}

func validCaptionURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && CaptionHostOK(u.Hostname())
}

func fetchFailureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case networkpolicy.ErrorCode(err) == networkpolicy.CodeSSRFBlocked:
		return "ssrf_blocked"
	default:
		return "fetch_failed"
	}
}

func fail(plat Platform, reason, final string) Result {
	return Result{
		OK:         false,
		Platform:   plat,
		Source:     "empty",
		Reason:     reason,
		FinalURL:   final,
		Disclaimer: Disclaimer,
	}
}

func FormatError(reason string) string {
	return fmt.Sprintf("ok: false\nreason: %s\ndisclaimer: %s\n", reason, Disclaimer)
}
