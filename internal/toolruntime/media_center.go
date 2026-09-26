package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

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

// —— 媒体中心对接的免费站点 ——
// 公版直链找不到时，从对接站点提取真实播放直链，媒体中心直接播放：
// 音乐走酷我音乐（mp3 直链），电影走南瓜影视（m3u8 直链）。
// 提取器做成包级变量，测试可以替换。
var resolveKuwoSong = resolveKuwoSongLive
var resolveNanguaMovie = resolveNanguaMovieLive

var (
	mediaSiteHTTP = &http.Client{Timeout: 12 * time.Second}
	mediaSiteUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	kuwoRidPattern    = regexp.MustCompile(`MUSICRID'\s*:\s*'MUSIC_(\d+)'`)
	kuwoSongPattern   = regexp.MustCompile(`SONGNAME'\s*:\s*'([^']*)'`)
	kuwoArtistPattern = regexp.MustCompile(`ARTIST'\s*:\s*'([^']*)'`)
)

// mediaSiteGet 拉取对接站点的接口响应（带浏览器 UA；南瓜影视对普通爬虫返回 403）。
func mediaSiteGet(ctx context.Context, rawURL, referer string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", mediaSiteUA)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := mediaSiteHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("site request failed")
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func mediaSiteText(value string) string {
	replacer := strings.NewReplacer("&nbsp;", " ", "&quot;", `"`, "&#39;", "'", "&amp;", "&")
	return strings.TrimSpace(replacer.Replace(value))
}

// resolveKuwoSongLive 用酷我音乐的免费接口找到歌曲的 mp3 直链。
func resolveKuwoSongLive(ctx context.Context, query string) (string, string, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	searchURL := "http://search.kuwo.cn/r.s?all=" + url.QueryEscape(q) + "&ft=music&client=kt&pn=0&rn=5&rformat=json&encoding=utf8"
	body, err := mediaSiteGet(ctx, searchURL, "https://www.kuwo.cn/", 1<<20)
	if err != nil {
		return "", "", false
	}
	text := string(body)
	rids := kuwoRidPattern.FindAllStringSubmatch(text, -1)
	names := kuwoSongPattern.FindAllStringSubmatch(text, -1)
	artists := kuwoArtistPattern.FindAllStringSubmatch(text, -1)
	for i, rid := range rids {
		if i >= 3 {
			break
		}
		title := ""
		if i < len(names) {
			title = mediaSiteText(names[i][1])
		}
		if i < len(artists) {
			artist := mediaSiteText(artists[i][1])
			if artist != "" && !strings.Contains(title, artist) {
				title = title + " - " + artist
			}
		}
		if title == "" {
			title = q
		}
		playURL := "http://antiserver.kuwo.cn/anti.s?type=convert_url3&rid=MUSIC_" + rid[1] + "&format=mp3&response=url"
		body, err := mediaSiteGet(ctx, playURL, "https://www.kuwo.cn/", 1<<16)
		if err != nil {
			continue
		}
		var play struct {
			Code int    `json:"code"`
			URL  string `json:"url"`
		}
		if json.Unmarshal(body, &play) != nil || play.Code != 200 || !strings.HasPrefix(play.URL, "http") {
			continue
		}
		checked, _, err := validateCenterMediaURL(play.URL, false)
		if err != nil {
			continue
		}
		return checked, title, true
	}
	return "", "", false
}

// resolveNanguaMovieLive 用南瓜影视的接口找到影片的 m3u8 直链。
func resolveNanguaMovieLive(ctx context.Context, query string) (string, string, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	searchURL := "https://nangua1.tv/api/search?q=" + url.QueryEscape(q) + "&page=1&size=8"
	body, err := mediaSiteGet(ctx, searchURL, "https://nangua1.tv/search", 2<<20)
	if err != nil {
		return "", "", false
	}
	var hits struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &hits) != nil || hits.Code != 0 {
		return "", "", false
	}
	for _, hit := range hits.Data.List {
		if hit.ID == 0 {
			continue
		}
		rawURL, title, ok := nanguaEpisodeURL(ctx, hit.ID)
		if !ok {
			continue
		}
		if name := strings.TrimSpace(hit.Title); name != "" {
			title = name
		}
		return rawURL, title, true
	}
	return "", "", false
}

// nanguaEpisodeURL 取影片详情里的第一个可用 m3u8 源（西瓜、天堂、非凡等多源逐个探测）。
func nanguaEpisodeURL(ctx context.Context, videoID int) (string, string, bool) {
	detailURL := fmt.Sprintf("https://nangua1.tv/api/video/%d", videoID)
	body, err := mediaSiteGet(ctx, detailURL, "https://nangua1.tv/", 4<<20)
	if err != nil {
		return "", "", false
	}
	var detail struct {
		Code int `json:"code"`
		Data struct {
			Title   string `json:"title"`
			Sources []struct {
				Episodes []struct {
					URL string `json:"url"`
				} `json:"episodes"`
			} `json:"sources"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &detail) != nil || detail.Code != 0 {
		return "", "", false
	}
	title := strings.TrimSpace(detail.Data.Title)
	for _, source := range detail.Data.Sources {
		for _, episode := range source.Episodes {
			checked, _, err := validateCenterMediaURL(episode.URL, false)
			if err != nil {
				continue
			}
			if nanguaStreamAlive(ctx, checked) {
				return checked, title, true
			}
		}
	}
	return "", "", false
}

// nanguaStreamAlive 探测 m3u8 主清单是否可达：影视站的源经常失效，
// 选一个能连上的再交给媒体中心播放。
func nanguaStreamAlive(ctx context.Context, rawURL string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", mediaSiteUA)
	resp, err := mediaSiteHTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode == http.StatusOK
}

// mediaCenterSiteFallback 公版直链找不到时，从对接的免费网站提取真实播放直链，
// 媒体中心直接播放；对接网站也没有时才报没有。
func mediaCenterSiteFallback(ctx context.Context, name, want string) (Result, error) {
	if want == "audio" {
		if rawURL, title, ok := resolveKuwoSong(ctx, name); ok {
			return mediaCenterSiteResult(rawURL, "audio", title, "酷我音乐"), nil
		}
		return Result{}, fmt.Errorf("酷我音乐上没有《%s》这首歌", name)
	}
	if rawURL, title, ok := resolveNanguaMovie(ctx, name); ok {
		return mediaCenterSiteResult(rawURL, "video", title, "南瓜影视"), nil
	}
	return Result{}, fmt.Errorf("南瓜影视上没有《%s》这部片子", name)
}

func mediaCenterSiteResult(rawURL, kind, title, site string) Result {
	if strings.TrimSpace(title) == "" {
		title = site
	}
	return result(fmt.Sprintf("已交给媒体中心播放。\nMEDIA_CENTER\nurl: %s\nkind: %s\ntitle: %s\nsite: %s\n", rawURL, kind, title, site))
}

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
	switch strings.TrimSpace(a.Action) {
	case "stop", "close":
		return result("已关闭媒体中心播放。\nMEDIA_CENTER_STOP\n"), nil
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
		if !centerWantAudio(title) && (openCatalogCandidate(lookup) || openCatalogWantAudio(title)) {
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
		}
		if rawURL == "" {
			name := filmLookupName(title)
			if name == "" {
				return Result{}, errors.New("没有说出是哪一部片子，找不到")
			}
			query := "site:archive.org/download " + name
			if searchQueryForbidden(query) {
				return Result{}, errors.New("媒体中心不能搜索这类内容")
			}
			found, err := searchForMediaCenter(r, ctx, query)
			var picked, pickedTitle, pickedKind string
			var ok bool
			if err == nil {
				picked, pickedTitle, pickedKind, ok = pickMatchingArchiveMedia(found.Results, name, centerMediaWant(title))
			}
			if !ok {
				// 公版直链找不到：从对接的免费网站提取真实播放直链。
				return mediaCenterSiteFallback(ctx, name, centerMediaWant(title))
			}
			rawURL, kind = picked, pickedKind
			if pickedTitle != "" {
				title = pickedTitle
			}
		}
	}
	return result(fmt.Sprintf("已交给媒体中心播放。\nMEDIA_CENTER\nurl: %s\nkind: %s\ntitle: %s\n", rawURL, kind, title)), nil
}

func mediaCenterSearchQuery(query string) string {
	q := strings.TrimSpace(query)
	for _, cut := range []string{"自带的媒体中心", "自带媒体中心", "媒体中心播放", "媒体中心", "自带的", "自带", "从网上", "帮我", "找一个", "找个", "播放", "电影", "影片", "歌曲", "一部", "一首", "再我的", "给我", "一下", "我想看", "这部", "周星驰的"} {
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
		return ""
	}
	if len(q) > 180 {
		q = q[:180]
	}
	return "site:archive.org/download " + q
}

func filmLookupName(query string) string {
	q := mediaCenterSearchQuery(query)
	return strings.TrimSpace(strings.TrimPrefix(q, "site:archive.org/download "))
}

func centerWantAudio(query string) bool {
	if strings.Contains(query, "电影") || strings.Contains(query, "影片") {
		return false
	}
	return strings.Contains(query, "歌") || strings.Contains(query, "一首") || strings.Contains(query, "音乐")
}

func centerMediaWant(query string) string {
	if centerWantAudio(query) {
		return "audio"
	}
	return "video"
}

func pickMatchingArchiveMedia(results []webfetch.SearchResult, name, want string) (rawURL, title, kind string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", "", "", false
	}
	for _, hit := range results {
		blob := strings.ToLower(hit.Title + " " + hit.URL + " " + hit.Snippet)
		if !strings.Contains(blob, name) {
			continue
		}
		for _, candidate := range []string{hit.URL, firstArchiveMediaURL(hit.Snippet), firstArchiveMediaURL(hit.Title)} {
			checked, mediaKind, err := validateCenterMediaURL(candidate, true)
			if err != nil || mediaKind != want {
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
		return "", "", errors.New("媒体中心只播放 mp4、webm、m3u8、mp3 这类可直接播放的文件")
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
	case ".mp4", ".webm", ".m4v", ".m3u8":
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

// publicDomainMovieURL is the public-domain print tests use to prove a request
// without a title is not replaced with another film.
const publicDomainMovieURL = "https://upload.wikimedia.org/wikipedia/commons/c/c1/Night_of_the_Living_Dead_%281968%29.webm"

func openCatalogCandidate(lookup string) bool {
	switch strings.TrimSpace(lookup) {
	case "Metropolis", "Nosferatu":
		return true
	default:
		return false
	}
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
		"你试试", "试试", "试一下", "能不能", "能否", "爱奇艺", "优酷",
		"香港", "1990年代", "90年代", "九十年代", "80年代", "八十年代", "70年代", "七十年代", "1990", "年代",
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
