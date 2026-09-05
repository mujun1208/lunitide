package videounderstand

import (
	"net/url"
	"regexp"
	"strings"
)

type Platform string

const (
	PlatformBilibili Platform = "bilibili"
	PlatformDouyin   Platform = "douyin"
	PlatformTencent  Platform = "tencent"
	PlatformYouTube  Platform = "youtube"
)

const Disclaimer = "这不是逐帧看完视频。根据公开字幕/页面简介整理。"

var shareHosts = map[string]Platform{
	"bilibili.com":       PlatformBilibili,
	"www.bilibili.com":   PlatformBilibili,
	"m.bilibili.com":     PlatformBilibili,
	"b23.tv":             PlatformBilibili,
	"douyin.com":         PlatformDouyin,
	"www.douyin.com":     PlatformDouyin,
	"v.douyin.com":       PlatformDouyin,
	"iesdouyin.com":      PlatformDouyin,
	"www.iesdouyin.com":  PlatformDouyin,
	"v.qq.com":           PlatformTencent,
	"video.qq.com":       PlatformTencent,
	"m.v.qq.com":         PlatformTencent,
	"youtube.com":        PlatformYouTube,
	"www.youtube.com":    PlatformYouTube,
	"m.youtube.com":      PlatformYouTube,
	"youtu.be":           PlatformYouTube,
}

var (
	httpURLRe = regexp.MustCompile(`(?i)https?://[^\s<>"'，。；、]+`)
	bareURLRe = regexp.MustCompile(`(?i)(?:^|[\s])((?:b23\.tv|v\.douyin\.com|youtu\.be|(?:www\.|m\.)?bilibili\.com|(?:www\.)?douyin\.com|(?:www\.)?iesdouyin\.com|v\.qq\.com|video\.qq\.com|m\.v\.qq\.com|(?:www\.|m\.)?youtube\.com)/[^\s<>"'，。；、]+)`)
)

// DetectShareURL finds the first allowlisted video share URL in goal.
func DetectShareURL(goal string) (canonical string, platform Platform, ok bool) {
	for _, raw := range findURLCandidates(goal) {
		canon, plat, hit := ClassifyShareURL(raw)
		if hit {
			return canon, plat, true
		}
	}
	return "", "", false
}

func findURLCandidates(goal string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		raw = trimURLJunk(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, raw)
	}
	for _, m := range httpURLRe.FindAllString(goal, -1) {
		add(m)
	}
	for _, m := range bareURLRe.FindAllStringSubmatch(goal, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}
	return out
}

func trimURLJunk(raw string) string {
	raw = strings.TrimSpace(raw)
	return strings.TrimRight(raw, ".,;!?)]}>。，；！？、」』】")
}

// ClassifyShareURL reports whether raw is an allowlisted share URL.
func ClassifyShareURL(raw string) (canonical string, platform Platform, ok bool) {
	u, err := parseShareURL(raw)
	if err != nil {
		return "", "", false
	}
	plat, ok := SharePlatform(u.Hostname())
	if !ok {
		return "", "", false
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), plat, true
}

func parseShareURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errBadURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errBadURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, errBadURL
	}
	return u, nil
}

var errBadURL = errString("invalid video url")

type errString string

func (e errString) Error() string { return string(e) }

// SharePlatform matches an exact share host. No wildcard suffixes.
func SharePlatform(host string) (Platform, bool) {
	host = canonicalHost(host)
	plat, ok := shareHosts[host]
	return plat, ok
}

func canonicalHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, ok := strings.Cut(host, ":"); ok && h != "" {
		// hostname without port; IPv6 left as-is and will miss the table
		if !strings.Contains(host, "]") {
			return h
		}
	}
	return host
}

// CaptionHostOK reports whether a second-hop subtitle URL may be fetched.
func CaptionHostOK(host string) bool {
	host = canonicalHost(host)
	if _, ok := SharePlatform(host); ok {
		return true
	}
	return strings.HasSuffix(host, ".hdslb.com")
}
