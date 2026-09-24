package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/webfetch"
)

// searchForMediaCenter is the only network call on the owned-player path.
// Tests replace it. The product never fetches the media bytes itself.
var searchForMediaCenter = func(r *Runtime, ctx context.Context, query string) (webSearchResponse, error) {
	if r == nil || r.fetchWeb == nil {
		return webSearchResponse{}, errors.New("web tools unavailable")
	}
	return r.searchWeb(ctx, query, 5)
}

var archiveMediaURL = regexp.MustCompile(`(?i)https://(?:[a-z0-9-]+\.)*archive\.org/download/[^\s"'<>]+?\.(?:mp4|webm|m4v|mp3|m4a|aac|flac|wav|ogg|oga)`)

func mediaCenterRequested(args json.RawMessage) bool {
	var a struct {
		Target string `json:"target"`
	}
	if json.Unmarshal(args, &a) != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(a.Target), "center")
}

func (r *Runtime) executeMediaCenter(ctx context.Context, args json.RawMessage) (Result, error) {
	var a struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Query  string `json:"query"`
		URL    string `json:"url"`
		App    string `json:"app"`
	}
	if json.Unmarshal(args, &a) != nil {
		return Result{}, errors.New("invalid arguments")
	}
	rawURL := strings.TrimSpace(a.URL)
	title := strings.TrimSpace(a.Query)
	kind := ""
	if rawURL != "" {
		checked, mediaKind, err := validateCenterMediaURL(rawURL, false)
		if err != nil {
			return Result{}, err
		}
		rawURL, kind = checked, mediaKind
		if title == "" {
			title = centerMediaTitle(rawURL)
		}
	} else {
		lookup := catalogLookupQuery(title)
		if lookup == "" {
			lookup = title
		}
		if picked, pickedTitle, pickedKind, ok := resolveOpenMedia(ctx, lookup); ok {
			checked, mediaKind, err := validateOpenCatalogURL(picked)
			if err == nil {
				rawURL, kind = checked, mediaKind
				if pickedTitle != "" {
					title = pickedTitle
				}
				if pickedKind == "audio" || pickedKind == "video" {
					kind = pickedKind
				}
			}
		}
		if rawURL == "" && !genericCenterMovie(title) {
			if openCatalogWantAudio(title) {
				return Result{}, errors.New("没有找到可在媒体中心直接播放的公版文件（已查 Internet Archive、维基共享资源、NASA）。请给出一个 https 直链（mp4、webm 或 mp3），或在媒体中心选择本机文件。")
			}
			rawURL, kind = publicDomainMovieURL, "video"
			title = publicDomainMovieTitle
		}
		if rawURL == "" {
			query := mediaCenterSearchQuery(title)
			if searchQueryForbidden(query) {
				return Result{}, errors.New("媒体中心不能搜索这类内容")
			}
			found, err := searchForMediaCenter(r, ctx, query)
			var picked, pickedTitle, pickedKind string
			var ok bool
			if err == nil {
				picked, pickedTitle, pickedKind, ok = pickArchiveMedia(found.Results)
			}
			if !ok {
				picked, pickedTitle, pickedKind, ok = publicDomainMovieFallback(title)
			}
			if !ok {
				if err != nil {
					return Result{}, fmt.Errorf("媒体中心没有搜到可播放文件：%w", err)
				}
				return Result{}, errors.New("没有找到可在媒体中心直接播放的公版文件（已查 Internet Archive、维基共享资源、NASA）。请给出一个 https 直链（mp4、webm 或 mp3），或在媒体中心选择本机文件。")
			}
			rawURL, kind = picked, pickedKind
			if pickedTitle != "" {
				title = pickedTitle
			}
			if title == "" {
				title = centerMediaTitle(rawURL)
			}
		}
	}
	return result(fmt.Sprintf("已交给媒体中心播放。\nMEDIA_CENTER\nurl: %s\nkind: %s\ntitle: %s\n", rawURL, kind, title)), nil
}

func mediaCenterSearchQuery(query string) string {
	q := strings.TrimSpace(query)
	for _, cut := range []string{"自带的媒体中心", "自带媒体中心", "媒体中心播放", "媒体中心", "自带的", "自带", "从网上", "帮我", "找一个", "找个", "播放", "电影", "歌曲", "一部", "一首", "再我的", "给我", "一下"} {
		q = strings.ReplaceAll(q, cut, " ")
	}
	q = strings.Map(func(r rune) rune {
		if strings.ContainsRune("，。！？、,.!?；;：:", r) {
			return ' '
		}
		return r
	}, q)
	stop := map[string]bool{"在": true, "的": true, "了": true, "把": true, "请": true, "再": true, "我": true, "个": true, "一个": true, "随便": true, "任意": true, "网上": true, "找": true, "要": true, "想": true, "能": true, "吗": true, "吧": true, "到": true, "里": true, "上": true, "用": true, "让": true, "给": true}
	kept := make([]string, 0, 4)
	for _, field := range strings.Fields(q) {
		if !stop[field] {
			kept = append(kept, field)
		}
	}
	q = strings.Join(kept, " ")
	if q == "" {
		return `site:archive.org/download "Night of the Living Dead"`
	}
	if len(q) > 180 {
		q = q[:180]
	}
	return "site:archive.org/download " + q
}

func pickArchiveMedia(results []webfetch.SearchResult) (rawURL, title, kind string, ok bool) {
	for _, hit := range results {
		for _, candidate := range []string{hit.URL, firstArchiveMediaURL(hit.Snippet), firstArchiveMediaURL(hit.Title)} {
			checked, mediaKind, err := validateCenterMediaURL(candidate, true)
			if err != nil {
				continue
			}
			return checked, strings.TrimSpace(hit.Title), mediaKind, true
		}
	}
	return "", "", "", false
}

func firstArchiveMediaURL(text string) string {
	return archiveMediaURL.FindString(text)
}

func validateCenterMediaURL(raw string, fromSearch bool) (string, string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", "", errors.New("媒体中心需要可直接播放的 https 地址")
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.User != nil {
		return "", "", errors.New("媒体中心不接受这个地址")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return "", "", errors.New("媒体中心只播放 https 直链，不打开网页")
	}
	if !publicMediaHost(parsed.Hostname()) {
		return "", "", errors.New("媒体中心不播放内网或本机地址")
	}
	kind := centerMediaKind(parsed.Path)
	if kind == "" {
		return "", "", errors.New("媒体中心只播放 mp4、webm、m4v、mp3 这类可直接打开的文件")
	}
	host := strings.ToLower(parsed.Hostname())
	archive := host == "archive.org" || strings.HasSuffix(host, ".archive.org")
	if fromSearch && (!archive || !strings.Contains(strings.ToLower(parsed.Path), "/download/")) {
		return "", "", errors.New("搜索到的文件必须是 Internet Archive 上的公版直链")
	}
	return u, kind, nil
}

func centerMediaKind(urlPath string) string {
	switch strings.ToLower(path.Ext(urlPath)) {
	case ".mp4", ".webm", ".m4v":
		return "video"
	case ".mp3", ".m4a", ".aac", ".flac", ".wav", ".ogg", ".oga":
		return "audio"
	default:
		return ""
	}
}

func centerMediaTitle(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "媒体中心"
	}
	base := path.Base(parsed.Path)
	if base == "" || base == "." || base == "/" {
		return "媒体中心"
	}
	return base
}

const publicDomainMovieURL = "https://upload.wikimedia.org/wikipedia/commons/transcoded/7/78/Nosferatu_%281922%29.webm/Nosferatu_%281922%29.webm.480p.vp9.webm"
const publicDomainMovieTitle = "Nosferatu (1922)"

func publicDomainMovieFallback(query string) (rawURL, title, kind string, ok bool) {
	if !genericCenterMovie(query) {
		return "", "", "", false
	}
	return publicDomainMovieURL, publicDomainMovieTitle, "video", true
}

func genericCenterMovie(query string) bool {
	lower := strings.ToLower(query)
	if (strings.Contains(query, "歌") || strings.Contains(lower, "song") || strings.Contains(lower, "music")) && !strings.Contains(query, "电影") && !strings.Contains(query, "视频") {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, cut := range []string{
		"自带的媒体中心", "自带媒体中心", "媒体中心播放", "媒体中心", "再我的", "从网上", "我看看",
		"找一个", "找到", "找个", "帮我", "给我", "出来", "可以", "适配", "一下", "播放", "电影", "影片", "视频", "歌曲",
		"一部", "一首", "一个", "随便", "任意", "网上", "爱情", "浪漫", "romance", "love", "movie", "film",
		"好看点", "好看", "比方", "比放", "找",
		"自带的", "自带", "的", "了", "在", "再", "我", "你", "个",
	} {
		q = strings.ReplaceAll(q, cut, "")
	}
	q = strings.Map(func(r rune) rune {
		if r == ' ' || strings.ContainsRune("，。！？、,.!?；;：:", r) {
			return -1
		}
		return r
	}, q)
	return q == ""
}

func publicMediaHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
	}
	return true
}
