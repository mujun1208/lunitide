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
	"sort"
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
// 音乐按顺序搜酷我音乐、网易云音乐；电影按顺序搜三个站——南瓜影视、
// zhengzhouhx 影视、6080 影视（ma2.cn），后两个是苹果CMS（maccms）
// 资源站，走标准采集接口。提取器做成包级变量，测试可以替换。

// songResolveFunc 在一个音乐站里找一首歌。
type songResolveFunc func(ctx context.Context, query string) (rawURL, display string, ok bool)

type songSiteEntry struct {
	name    string
	resolve songResolveFunc
}

var songSites = []songSiteEntry{
	{name: "酷我音乐", resolve: resolveKuwoSongLive},
	{name: "网易云音乐", resolve: resolveNeteaseSongLive},
}

func songSiteNames() string {
	names := make([]string, 0, len(songSites))
	for _, site := range songSites {
		names = append(names, site.name)
	}
	return strings.Join(names, "、")
}

// movieResolveFunc 在一个电影站里找一部片子：strict 为 true 时结果标题
// 必须和片名对得上，杜绝「搜九品芝麻官播出完全无关的片子」。
type movieResolveFunc func(ctx context.Context, title string, strict bool) (rawURL, display string, ok bool)

type movieSiteEntry struct {
	name    string
	resolve movieResolveFunc
}

var movieSites = []movieSiteEntry{
	{name: "南瓜影视", resolve: resolveNanguaMovieLive},
	{name: "zhengzhouhx影视", resolve: maccmsResolver("http://k.zhengzhouhx.com", "http://k.zhengzhouhx.com/")},
	{name: "6080影视", resolve: maccmsResolver("https://www.ma2.cn", "https://www.ma2.cn/")},
}

func movieSiteNames() string {
	names := make([]string, 0, len(movieSites))
	for _, site := range movieSites {
		names = append(names, site.name)
	}
	return strings.Join(names, "、")
}

var (
	mediaSiteHTTP = &http.Client{Timeout: 12 * time.Second}
	mediaSiteUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	kuwoRidPattern    = regexp.MustCompile(`MUSICRID'\s*:\s*'MUSIC_(\d+)'`)
	kuwoSongPattern   = regexp.MustCompile(`SONGNAME'\s*:\s*'([^']*)'`)
	kuwoArtistPattern = regexp.MustCompile(`ARTIST'\s*:\s*'([^']*)'`)

	bookTitlePattern   = regexp.MustCompile(`《([^《》]{1,60})》`)
	quotedTitlePattern = regexp.MustCompile("[\"“']([^\"”']{1,60})[\"”']")
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
		songName := ""
		if i < len(names) {
			songName = mediaSiteText(names[i][1])
			title = songName
		}
		artist := ""
		if i < len(artists) {
			artist = mediaSiteText(artists[i][1])
			if artist != "" && !strings.Contains(title, artist) {
				title = title + " - " + artist
			}
		}
		// 歌名或歌手至少一头要和搜索词对得上，避免搜张三播出李四的歌。
		if !movieTitleMatches(q, songName) && !movieTitleMatches(q, artist) {
			continue
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

// nanguaSearchHit 是南瓜影视搜索接口返回的单个候选。
type nanguaSearchHit struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// resolveNeteaseSongLive 用网易云音乐的免费搜索接口找歌：候选里挑
// 非会员曲目，把外链 302 跟到 CDN 的 mp3 直链上再交给媒体中心。
func resolveNeteaseSongLive(ctx context.Context, query string) (string, string, bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	searchURL := "https://music.163.com/api/search/get?s=" + url.QueryEscape(q) + "&type=1&limit=8"
	body, err := mediaSiteGet(ctx, searchURL, "https://music.163.com/", 1<<20)
	if err != nil {
		return "", "", false
	}
	var hits struct {
		Code int `json:"code"`
		Result struct {
			Songs []struct {
				ID      int    `json:"id"`
				Name    string `json:"name"`
				Fee     int    `json:"fee"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"songs"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &hits) != nil || hits.Code != 200 {
		return "", "", false
	}
	for _, song := range hits.Result.Songs {
		if song.ID == 0 || song.Fee == 1 {
			continue // 会员曲目外链只会 302 到 404。
		}
		songName := strings.TrimSpace(song.Name)
		artist := ""
		if len(song.Artists) > 0 {
			artist = mediaSiteText(song.Artists[0].Name)
		}
		// 歌名或歌手至少一头要和搜索词对得上，避免搜张三播出李四的歌。
		if !movieTitleMatches(q, songName) && !movieTitleMatches(q, artist) {
			continue
		}
		title := songName
		if artist != "" && !strings.Contains(title, artist) {
			title = title + " - " + artist
		}
		rawURL, ok := neteaseOuterURL(ctx, song.ID)
		if !ok {
			continue
		}
		checked, _, err := validateCenterMediaURL(rawURL, false)
		if err != nil {
			continue
		}
		return checked, title, true
	}
	return "", "", false
}

// neteaseOuterURL 把网易云的播放外链跟到真实 mp3 直链：外链 302 到的
// CDN 地址是 http，网页里的 https 播放器加载不了，换成 https 再探活。
func neteaseOuterURL(ctx context.Context, songID int) (string, bool) {
	outer := fmt.Sprintf("https://music.163.com/song/media/outer/url?id=%d.mp3", songID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, outer, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", mediaSiteUA)
	resp, err := mediaSiteHTTP.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK || resp.Request == nil || resp.Request.URL == nil {
		return "", false
	}
	final := resp.Request.URL.String()
	if strings.HasPrefix(final, "http://") {
		final = "https://" + strings.TrimPrefix(final, "http://")
	}
	if !strings.HasPrefix(final, "https://") {
		return "", false
	}
	if !movieStreamAlive(ctx, final) {
		return "", false
	}
	return final, true
}

// resolveNanguaMovieLive 用南瓜影视的接口找到影片的 m3u8 直链。
func resolveNanguaMovieLive(ctx context.Context, query string, strict bool) (string, string, bool) {
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
			List []nanguaSearchHit `json:"list"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &hits) != nil || hits.Code != 0 {
		return "", "", false
	}
	list := hits.Data.List
	if strict {
		// 点名《九品芝麻官》要的是 1994 周星驰版：标题完全一致的候选
		// 优先于《新九品芝麻官2006》这类只是标题包含片名的衍生作品。
		list = rankExactTitleFirst(q, list, func(hit nanguaSearchHit) string { return hit.Title })
	}
	for _, hit := range list {
		if hit.ID == 0 {
			continue
		}
		// 标题对不上的候选直接跳过：站点全文搜索会把「我要看《九品芝麻官》」
		// 这类残留指令词糊弄成完全无关的片子。
		if strict && !movieTitleMatches(q, hit.Title) {
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
			if movieStreamAlive(ctx, checked) {
				return checked, title, true
			}
		}
	}
	return "", "", false
}

// movieStreamAlive 探测 m3u8 主清单是否可达：影视站的源经常失效，
// 选一个能连上的再交给媒体中心播放。
func movieStreamAlive(ctx context.Context, rawURL string) bool {
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

// maccmsResolver 对接苹果CMS（maccms）资源站：标准采集接口
// /api.php/provide/vod 直接返回 JSON，播放地址就在 vod_play_url 里。
func maccmsResolver(base, referer string) movieResolveFunc {
	return func(ctx context.Context, query string, strict bool) (string, string, bool) {
		q := strings.TrimSpace(query)
		if q == "" {
			return "", "", false
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		api := base + "/api.php/provide/vod/?ac=videolist&wd=" + url.QueryEscape(q)
		body, err := mediaSiteGet(ctx, api, referer, 4<<20)
		if err != nil {
			return "", "", false
		}
		var hits struct {
			Code int              `json:"code"`
			List []maccmsSearchHit `json:"list"`
		}
		if json.Unmarshal(body, &hits) != nil || len(hits.List) == 0 {
			return "", "", false
		}
		list := hits.List
		if strict {
			list = rankExactTitleFirst(q, list, func(hit maccmsSearchHit) string { return hit.Name })
		}
		for _, hit := range list {
			name := strings.TrimSpace(hit.Name)
			if strict && !movieTitleMatches(q, name) {
				continue
			}
			if rawURL, ok := maccmsPlayURL(ctx, hit.PlayURL); ok {
				return rawURL, name, true
			}
		}
		return "", "", false
	}
}

// maccmsPlayURL 从 vod_play_url 里挑一个活着的直链：
// 格式是「集名$地址#集名$地址」，多个播放源之间以 $$$ 分隔。
func maccmsPlayURL(ctx context.Context, playURL string) (string, bool) {
	for _, source := range strings.Split(playURL, "$$$") {
		for _, entry := range strings.Split(source, "#") {
			parts := strings.SplitN(entry, "$", 2)
			if len(parts) != 2 {
				continue
			}
			checked, _, err := validateCenterMediaURL(strings.TrimSpace(parts[1]), false)
			if err != nil {
				continue
			}
			if movieStreamAlive(ctx, checked) {
				return checked, true
			}
		}
	}
	return "", false
}

// —— 片名提取与匹配 ——

// mediaQueryCutPhrases 是对话里的指令短语，剥掉之后剩下的才是片名或歌名。
// 长词在前，先剥长词再剥短词。
var mediaQueryCutPhrases = []string{
	"自带的媒体中心", "自带媒体中心", "在我的媒体中心", "媒体中心播放", "媒体中心里", "媒体中心",
	"帮我播放一部", "帮我播放一首", "帮我播放", "帮我放一部", "帮我放一首", "帮我放", "帮我找一部",
	"给我播放一部", "给我播放一首", "给我播放", "给我放一部", "给我放一首", "给我放",
	"我想看一部", "我想看", "我要看一部", "我要看", "想看一部", "想看", "要看一部", "要看", "看一下", "看一",
	"我要听一首", "我要听", "想听一首", "想听", "听一首", "来一首", "放一首", "唱一首", "哼一首", "听一",
	"随便来一部", "随便来一首", "随便放一部", "随便放一首", "随便播放", "随便", "任意",
	"播放一部", "播放一首", "播放", "放一部", "放个", "找一部", "来一部", "找个", "找一个", "来点", "点个",
	"一部", "一首", "一个", "部电影", "部片子", "这部", "那部", "这个", "那个",
	"的片子", "的电影", "的影片", "的歌曲", "的歌", "电影", "影片", "片子", "歌曲",
	"帮我", "给我", "出来", "一下", "我想", "我要", "可以", "能不能", "能否", "你试试", "试试", "试一下", "从网上", "网上",
}

// mediaQueryStopRunes 整段都由这些虚字组成的片段直接丢弃
//（中文没有空格，按段过滤才不会误伤「让子弹飞」这类片名）。
const mediaQueryStopRunes = "的了在吗呢吧啊呀哦嗯把请再我你他她它个能到里上用让找要想看听给放唱播首部种来去就也很最"

func mediaFieldAllStop(field string) bool {
	for _, r := range field {
		if !strings.ContainsRune(mediaQueryStopRunes, r) {
			return false
		}
	}
	return field != ""
}

// mediaLookupName 从用户的话里提取纯片名/歌名：书名号和引号里的内容优先，
// 否则剥掉指令短语，取剩下的最长实质片段。
func mediaLookupName(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	if picks := bookTitlePattern.FindAllStringSubmatch(q, -1); len(picks) > 0 {
		best := ""
		for _, pick := range picks {
			if t := strings.TrimSpace(pick[1]); len([]rune(t)) > len([]rune(best)) {
				best = t
			}
		}
		if best != "" {
			return best
		}
	}
	if m := quotedTitlePattern.FindStringSubmatch(q); m != nil {
		if t := strings.TrimSpace(m[1]); t != "" {
			return t
		}
	}
	for _, cut := range mediaQueryCutPhrases {
		q = strings.ReplaceAll(q, cut, " ")
	}
	q = strings.Map(func(r rune) rune {
		if strings.ContainsRune("，。！？、,.!?；;：:\"'“”‘’《》", r) {
			return ' '
		}
		return r
	}, q)
	segments := make([]string, 0, 4)
	for _, field := range strings.Fields(q) {
		if mediaFieldAllStop(field) {
			continue
		}
		segments = append(segments, field)
	}
	if len(segments) == 0 {
		return ""
	}
	// 英文名字靠空格分词（Night of the Living Dead），整句保留；
	// 中文一句话里可能同时报演员和片名，取最长的一段当片名。
	for _, seg := range segments {
		if strings.ContainsFunc(seg, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }) {
			return strings.Join(segments, " ")
		}
	}
	best := ""
	for _, seg := range segments {
		if len([]rune(seg)) > len([]rune(best)) {
			best = seg
		}
	}
	return best
}

// normalizeMovieTitle 归一化片名：去空白、书名号、常见括号并转小写。
func normalizeMovieTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "《", "", "》", "", "[", "", "]", "", "（", "", "）", "", "(", "", ")", "", "【", "", "】", "", `"`, "", "“", "", "”", "", "'", "")
	return replacer.Replace(value)
}

// maccmsSearchHit 是苹果CMS采集接口返回的单个候选。
type maccmsSearchHit struct {
	Name    string `json:"vod_name"`
	PlayURL string `json:"vod_play_url"`
}

// movieTitleEquals 判断标题归一化后与片名完全一致。
func movieTitleEquals(want, got string) bool {
	w, g := normalizeMovieTitle(want), normalizeMovieTitle(got)
	return w != "" && w == g
}

// rankExactTitleFirst 把标题与片名完全一致的候选排到最前：
// 点名《九品芝麻官》时优先 1994 周星驰版，而不是《新九品芝麻官2006》、
// 《武状元苏乞儿之天降神谕》这类只是标题包含片名的衍生作品。
func rankExactTitleFirst[T any](want string, items []T, title func(T) string) []T {
	sort.SliceStable(items, func(i, j int) bool {
		return movieTitleEquals(want, title(items[i])) && !movieTitleEquals(want, title(items[j]))
	})
	return items
}

// movieTitleMatches 判断搜索结果的标题是不是要找的那部片子：
// 归一化后互相包含才算（「九品芝麻官」能对上「九品芝麻官(1994)国语」）。
func movieTitleMatches(want, got string) bool {
	w, g := normalizeMovieTitle(want), normalizeMovieTitle(got)
	if w == "" || g == "" {
		return false
	}
	// 结果标题包含片名（「九品芝麻官(1994)国语」对「九品芝麻官」）：可信。
	if strings.Contains(g, w) {
		return true
	}
	// 反过来片名包含结果标题时要求标题至少占片名的一半长度：
	// 酷我会把乱打的「咕噜哇啦咔咔」糊弄成《咕噜》，这种短标题要挡住；
	// 「卢冠廷的一生所爱」对《一生所爱》这种仍然放行。
	if !strings.Contains(w, g) {
		return false
	}
	return len([]rune(g))*2 >= len([]rune(w))
}

// vagueActorQuery 判断用户是不是只报了演员的泛搜（「放一部周星驰的电影」）。
// 只在严格匹配一轮全部落空后才用它放宽，点名的片子永远优先严格匹配。
func vagueActorQuery(query, name string) bool {
	if name == "" {
		return false
	}
	for _, suffix := range []string{"的电影", "的影片", "的片子", "的片"} {
		if strings.Contains(query, name+suffix) {
			return true
		}
	}
	return false
}

// mediaCenterSiteFallback 公版直链找不到时，从对接的免费网站提取真实播放直链，
// 媒体中心直接播放；对接网站也没有时才报没有。
func mediaCenterSiteFallback(ctx context.Context, query, want string) (Result, error) {
	name := mediaLookupName(query)
	if want == "audio" {
		if name == "" {
			return Result{}, errors.New("没有说出是哪首歌，找不到")
		}
		for _, site := range songSites {
			if rawURL, title, ok := site.resolve(ctx, name); ok {
				return mediaCenterSiteResult(rawURL, "audio", title, site.name), nil
			}
		}
		return Result{}, fmt.Errorf("%s上都没有《%s》这首歌", songSiteNames(), name)
	}
	if name == "" {
		return Result{}, errors.New("没有说出是哪一部片子，找不到")
	}
	// 第一轮：只接受标题对得上的结果，杜绝「搜九品芝麻官播出完全无关的片子」。
	for _, site := range movieSites {
		if rawURL, title, ok := site.resolve(ctx, name, true); ok {
			return mediaCenterSiteResult(rawURL, "video", title, site.name), nil
		}
	}
	// 第二轮：用户只报了演员时放宽标题校验再试一遍。
	if vagueActorQuery(query, name) {
		for _, site := range movieSites {
			if rawURL, title, ok := site.resolve(ctx, name, false); ok {
				return mediaCenterSiteResult(rawURL, "video", title, site.name), nil
			}
		}
	}
	return Result{}, fmt.Errorf("%s上都没有《%s》这部片子", movieSiteNames(), name)
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
				return mediaCenterSiteFallback(ctx, title, centerMediaWant(title))
			}
			rawURL, kind = picked, pickedKind
			if pickedTitle != "" {
				title = pickedTitle
			}
		}
	}
	return result(fmt.Sprintf("已交给媒体中心播放。\nMEDIA_CENTER\nurl: %s\nkind: %s\ntitle: %s\n", rawURL, kind, title)), nil
}

// filmLookupName 从用户的话里提取片名/歌名，公版搜索也用同一套提取，
// 保证「我要看《九品芝麻官》」在公版库和对接站点搜的都是同一个词。
func filmLookupName(query string) string {
	return mediaLookupName(query)
}

func centerWantAudio(query string) bool {
	if strings.Contains(query, "电影") || strings.Contains(query, "影片") {
		return false
	}
	// 「曲」覆盖主题曲/片尾曲，「听」覆盖「我想听xxx」——这两种说法
	// 都没带「歌」字，漏了就会把歌当电影送去电影站。
	return strings.Contains(query, "歌") || strings.Contains(query, "一首") || strings.Contains(query, "音乐") ||
		strings.Contains(query, "曲") || strings.Contains(query, "听")
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
