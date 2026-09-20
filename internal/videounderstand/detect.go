package videounderstand

import (
	"net/url"
	"regexp"
	"strings"
)

type Platform string

const (
	PlatformBilibili  Platform = "bilibili"
	PlatformDouyin    Platform = "douyin"
	PlatformKuaishou  Platform = "kuaishou"
	PlatformTencent   Platform = "tencent"
	PlatformYouTube   Platform = "youtube"
	PlatformWeixin    Platform = "weixin"
	PlatformDirect    Platform = "direct_video"
)

const Disclaimer = "这不是逐帧看完视频。根据公开字幕/页面简介整理。"

var shareHosts = map[string]Platform{
	"bilibili.com":           PlatformBilibili,
	"www.bilibili.com":       PlatformBilibili,
	"m.bilibili.com":         PlatformBilibili,
	"b23.tv":                 PlatformBilibili,
	"douyin.com":             PlatformDouyin,
	"www.douyin.com":         PlatformDouyin,
	"v.douyin.com":           PlatformDouyin,
	"iesdouyin.com":          PlatformDouyin,
	"www.iesdouyin.com":      PlatformDouyin,
	"v.qq.com":               PlatformTencent,
	"video.qq.com":           PlatformTencent,
	"m.v.qq.com":             PlatformTencent,
	"youtube.com":            PlatformYouTube,
	"www.youtube.com":        PlatformYouTube,
	"m.youtube.com":          PlatformYouTube,
	"youtu.be":               PlatformYouTube,
	"channels.weixin.qq.com": PlatformWeixin,
	"kuaishou.com":           PlatformKuaishou,
	"www.kuaishou.com":       PlatformKuaishou,
	"m.kuaishou.com":         PlatformKuaishou,
	"v.kuaishou.com":         PlatformKuaishou,
	"live.kuaishou.com":      PlatformKuaishou,
	"video.kuaishou.com":     PlatformKuaishou,
	"kwai.com":               PlatformKuaishou,
	"s.kwai.com":             PlatformKuaishou,
	"c.kuaishou.com":         PlatformKuaishou,
}

var (
	httpURLRe = regexp.MustCompile(`(?i)https?://[^\s<>"'，。；、]+`)
	bareURLRe = regexp.MustCompile(`(?i)(?:^|[\s])((?:b23\.tv|v\.douyin\.com|youtu\.be|(?:www\.|m\.)?bilibili\.com|(?:www\.)?douyin\.com|(?:www\.)?iesdouyin\.com|v\.qq\.com|video\.qq\.com|m\.v\.qq\.com|(?:www\.|m\.)?youtube\.com|(?:www\.)?weixin\.qq\.com|channels\.weixin\.qq\.com|(?:www\.|m\.|v\.|c\.|live\.|video\.)?kuaishou\.com|(?:s\.)?kwai\.com)/[^\s<>"'，。；、]+)`)
)

// DetectShareURL finds the first allowlisted video share URL in goal.
func DetectShareURL(goal string) (canonical string, platform Platform, ok bool) {
	for _, raw := range findURLCandidates(goal) {
		if canon, hit := ClassifyDirectURL(raw); hit {
			return canon, PlatformDirect, true
		}
		canon, plat, hit := ClassifyShareURL(raw)
		if hit {
			return canon, plat, true
		}
	}
	return "", "", false
}

// ClassifyDirectURL recognizes ordinary media files only. It does not discover
// hidden streams or accept playlists; the fetch transport enforces public IPs.
func ClassifyDirectURL(raw string) (string, bool) {
	u, err := parseShareURL(raw)
	if err != nil {
		return "", false
	}
	path := strings.ToLower(u.Path)
	for _, ext := range []string{".mp4", ".mov", ".webm", ".mkv"} {
		if strings.HasSuffix(path, ext) {
			u.Fragment, u.RawFragment = "", ""
			return u.String(), true
		}
	}
	return "", false
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
	plat, ok := weixinShare(u)
	if !ok {
		plat, ok = SharePlatform(u.Hostname())
	}
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
	if err != nil || u.Host == "" || u.User != nil {
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

func weixinShare(u *url.URL) (Platform, bool) {
	host := canonicalHost(u.Hostname())
	path := strings.ToLower(u.Path)
	if host == "channels.weixin.qq.com" {
		return PlatformWeixin, true
	}
	if host == "weixin.qq.com" || host == "www.weixin.qq.com" {
		if strings.Contains(path, "/sph/") || strings.HasPrefix(path, "/sph") {
			return PlatformWeixin, true
		}
	}
	return "", false
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
	if host == "weixin.qq.com" || host == "www.weixin.qq.com" {
		return true
	}
	if strings.HasSuffix(host, ".hdslb.com") {
		return true
	}
	return strings.HasSuffix(host, ".kuaishou.com") || strings.HasSuffix(host, ".kwai.com")
}
