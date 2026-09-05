package videounderstand

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

type FetchFunc func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)

type Result struct {
	OK                 bool
	Platform           Platform
	Source             string
	Reason             string
	Title              string
	Author             string
	FinalURL           string
	Description        string
	Captions           string
	CaptionsTruncated  bool
	CoverDescribed     bool
	Disclaimer         string
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
	} else if r.Source == "page_meta" || r.Source == "empty" {
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
	canon, plat, ok := ClassifyShareURL(rawURL)
	if !ok {
		return fail("", "unsupported_host", "")
	}
	if fetch == nil {
		return fail(plat, "fetch_failed", canon)
	}
	page, err := fetch(ctx, canon)
	if err != nil {
		if networkpolicy.ErrorCode(err) == networkpolicy.CodeSSRFBlocked {
			return fail(plat, "ssrf_blocked", canon)
		}
		return fail(plat, "fetch_failed", canon)
	}
	final := strings.TrimSpace(page.FinalURL)
	if final == "" {
		final = canon
	}
	if u, err := url.Parse(final); err != nil || u.Host == "" {
		return fail(plat, "unsupported_host", final)
	} else if _, shareOK := SharePlatform(u.Hostname()); !shareOK {
		return fail(plat, "unsupported_host", final)
	}
	parsed := ParseHTML(string(page.Body))
	out := Result{
		Platform:   plat,
		Title:      parsed.Title,
		Author:     parsed.Author,
		FinalURL:   final,
		Description: parsed.Description,
		Disclaimer: Disclaimer,
	}
	if parsed.CaptionURL != "" {
		if capRes, capErr := fetchCaption(ctx, parsed.CaptionURL, fetch); capErr == nil {
			out.Captions = capRes.text
			out.CaptionsTruncated = capRes.truncated
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
		return captionFetch{}, errors.New("bad caption url")
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if !CaptionHostOK(u.Hostname()) {
		return captionFetch{}, errors.New("caption host not allowed")
	}
	page, err := fetch(ctx, u.String())
	if err != nil {
		return captionFetch{}, err
	}
	text, trunc := ParseCaptions(page.ContentType, page.Body)
	if text == "" {
		return captionFetch{}, errors.New("empty captions")
	}
	return captionFetch{text: text, truncated: trunc}, nil
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
