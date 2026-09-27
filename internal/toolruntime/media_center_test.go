package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/webfetch"
)

// 对接站点直链的测试替身：让回退用例不依赖真实外网。
const (
	testKuwoMP3    = "https://kw-lv.kuwo.cn/resource/30106/trackmedia/test-song.mp3"
	testNanguaM3U8 = "https://vip.dytt-film.com/20250121/1309_test/index.m3u8"
)

func stubSiteResolvers(t *testing.T, song func(context.Context, string) (string, string, bool), movie movieResolveFunc) {
	t.Helper()
	prevSongs := songSites
	prevSites := movieSites
	if song != nil {
		songSites = []songSiteEntry{{name: "酷我音乐", resolve: song}}
	}
	if movie != nil {
		movieSites = []movieSiteEntry{{name: "南瓜影视", resolve: movie}}
	}
	t.Cleanup(func() {
		songSites = prevSongs
		movieSites = prevSites
	})
}

func songHit(_ context.Context, query string) (string, string, bool) {
	return testKuwoMP3, query + " - 测试歌手", true
}

func movieHit(_ context.Context, query string, strict bool) (string, string, bool) {
	return testNanguaM3U8, query + "（测试片源）", true
}

func TestMediaCenterDirectURLDoesNotOpenAnotherPlayer(t *testing.T) {
	opened := false
	openMediaURL = func(string) error {
		opened = true
		return nil
	}
	t.Cleanup(func() { openMediaURL = openHTTPURL })
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","url":"https://archive.org/download/night_of_the_living_dead/Night.mp4"}`))
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("owned media center must not open an external player or browser")
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, "kind: video") || !strings.Contains(out.Output, "https://archive.org/download/night_of_the_living_dead/Night.mp4") {
		t.Fatal(out.Output)
	}
}

func TestMediaCenterRejectsPagesAndPrivateHosts(t *testing.T) {
	for _, raw := range []string{
		"file:///C:/film.mp4",
		"https://archive.org/details/night_of_the_living_dead",
		"https://10.0.0.5/film.mp4",
		"https://localhost/film.mp4",
		"http://archive.org/download/night/Night.mp4",
	} {
		if _, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","url":"`+raw+`"}`)); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestSongAndMovieUseSeparateArchiveFiles(t *testing.T) {
	prev := searchForMediaCenter
	searchForMediaCenter = func(_ *Runtime, _ context.Context, query string) (webSearchResponse, error) {
		if strings.Contains(query, "生所爱") {
			return webSearchResponse{Results: []webfetch.SearchResult{
				{Title: "生所爱", URL: "https://archive.org/download/example/shengsuoai.mp3"},
				{Title: "生所爱", URL: "https://archive.org/download/example/shengsuoai.mp4"},
			}}, nil
		}
		if strings.Contains(query, "Night of the Living Dead") {
			return webSearchResponse{Results: []webfetch.SearchResult{
				{Title: "Night of the Living Dead", URL: "https://archive.org/download/night_of_the_living_dead/Night.mp3"},
				{Title: "Night of the Living Dead", URL: "https://archive.org/download/night_of_the_living_dead/Night.mp4"},
			}}, nil
		}
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() { searchForMediaCenter = prev })
	stubSiteResolvers(t, songHit, movieHit)
	song, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放一首生所爱"}`))
	if err != nil || !strings.Contains(song.Output, "MEDIA_CENTER") || !strings.Contains(song.Output, "kind: audio") || !strings.Contains(song.Output, "shengsuoai.mp3") || strings.Contains(song.Output, ".mp4") || strings.Contains(song.Output, "music.163.com") {
		t.Fatalf("song err=%v out=%s", err, song.Output)
	}
	film, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"Night of the Living Dead"}`))
	if err != nil || !strings.Contains(film.Output, "MEDIA_CENTER") || !strings.Contains(film.Output, "kind: video") || !strings.Contains(film.Output, "Night.mp4") || strings.Contains(film.Output, "Night.mp3") {
		t.Fatalf("film err=%v out=%s", err, film.Output)
	}
	missing, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"武状元苏乞儿"}`))
	if err != nil || !strings.Contains(missing.Output, "MEDIA_CENTER") || !strings.Contains(missing.Output, testNanguaM3U8) || !strings.Contains(missing.Output, "site: 南瓜影视") || strings.Contains(missing.Output, "archive.org/download") {
		t.Fatalf("missing err=%v out=%s", err, missing.Output)
	}
}

func TestMediaCenterPicksArchiveDownloadAndSkipsHTML(t *testing.T) {
	prev := searchForMediaCenter
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{Results: []webfetch.SearchResult{
			{Title: "details", URL: "https://archive.org/details/night_of_the_living_dead"},
			{Title: "Night of the Living Dead", URL: "https://archive.org/download/night_of_the_living_dead/Night.mp4"},
		}}, nil
	}
	t.Cleanup(func() { searchForMediaCenter = prev })
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"Night of the Living Dead"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "https://archive.org/download/night_of_the_living_dead/Night.mp4") || !strings.Contains(out.Output, "Night of the Living Dead") {
		t.Fatal(out.Output)
	}
}

func TestMediaCenterGenericMovieFallsBackWhenSearchHasNoFile(t *testing.T) {
	prev := searchForMediaCenter
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{Results: []webfetch.SearchResult{{Title: "page", URL: "https://archive.org/details/romance"}}}, nil
	}
	t.Cleanup(func() { searchForMediaCenter = prev })
	stubSiteResolvers(t, nil, movieHit)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"帮我找到一个爱情电影再媒体中心可以播放出来"}`))
	if err != nil || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "Metropolis") {
		t.Fatalf("a movie with no matching file must play the partner site's link, not another film: err=%v out=%s", err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "site: 南瓜影视") {
		t.Fatal(out.Output)
	}
}

func TestMediaCenterNamedTitlePlaysOpenCatalogFile(t *testing.T) {
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "https://upload.wikimedia.org/wikipedia/commons/a/a0/Example.webm", "Example", "video", true
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		t.Fatal("catalog hit must not fall through to web search")
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"Nosferatu"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, "https://upload.wikimedia.org/wikipedia/commons/a/a0/Example.webm") || !strings.Contains(out.Output, "title: Example") {
		t.Fatal(out.Output)
	}
}

func TestMediaCenterRejectsCatalogURLOutsideOpenLibraries(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "https://cdn.example/movie.mp4", "Movie", "video", true
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, nil, movieHit)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"Nosferatu"}`))
	if err != nil || strings.Contains(out.Output, "cdn.example") || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "iqiyi.com") {
		t.Fatalf("a rejected catalog URL must not play, and must not switch films: err=%v out=%s", err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "site: 南瓜影视") {
		t.Fatal(out.Output)
	}
	if len(*opened) != 0 {
		t.Fatalf("no web page should open: %v", *opened)
	}
}

func TestMediaCenterNamedTitleIgnoresUnlicensedWebHit(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{Results: []webfetch.SearchResult{
			{Title: "Night of the Living Dead", URL: "https://archive.org/download/night_of_the_living_dead/Night.mp4"},
		}}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, nil, movieHit)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"夜访吸血鬼"}`))
	if err != nil || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "archive.org/download/night_of_the_living_dead") || strings.Contains(out.Output, "iqiyi.com") || strings.Contains(out.Output, "youku.com") || strings.Contains(out.Output, "music.163.com") {
		t.Fatalf("named title must not play a search hit or another film: err=%v out=%s", err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "夜访吸血鬼") {
		t.Fatal(out.Output)
	}
	if len(*opened) != 0 {
		t.Fatalf("no web page should open: %v", *opened)
	}
}

func TestMediaCenterStopDoesNotStartAnotherFilm(t *testing.T) {
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"stop","target":"center"}`))
	if err != nil || !strings.Contains(out.Output, "MEDIA_CENTER_STOP") || strings.Contains(out.Output, "url:") {
		t.Fatalf("err=%v out=%s", err, out.Output)
	}
}

func stubOfficialOpen(t *testing.T) *[]string {
	t.Helper()
	opened := []string{}
	openMediaURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	t.Cleanup(func() { openMediaURL = openHTTPURL })
	return &opened
}

func TestMediaCenterNamedStephenChowFilmSkipsCatalog(t *testing.T) {
	called := false
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		called = true
		return "https://upload.wikimedia.org/wikipedia/commons/c/c1/Night_of_the_Living_Dead_%281968%29.webm", "Night", "video", true
	}
	t.Cleanup(func() { resolveOpenMedia = prevResolve })
	stubSiteResolvers(t, nil, movieHit)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","query":"播放一部周星驰的电影，九品芝麻官"}`))
	if err != nil || called || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "iqiyi.com") {
		t.Fatalf("a named film must not be replaced: called=%v err=%v out=%s", called, err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "九品芝麻官") {
		t.Fatal(out.Output)
	}
	if len(*opened) != 0 {
		t.Fatalf("no web page should open: %v", *opened)
	}
}

func TestMediaCenterNamedTitleDoesNotUseMovieFallback(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, nil, movieHit)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"我想看夜访吸血鬼这部电影"}`))
	if err != nil || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "iqiyi.com") || strings.Contains(out.Output, "youku.com") {
		t.Fatalf("named title must not play another film: err=%v out=%s", err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "夜访吸血鬼") {
		t.Fatal(out.Output)
	}
	if len(*opened) != 0 {
		t.Fatalf("opened %v", *opened)
	}
}

func TestFilmLookupNameKeepsTheAskedTitle(t *testing.T) {
	if got := filmLookupName("帮我在媒体中心播放一部电影"); got != "" {
		t.Fatal(got)
	}
	if got := filmLookupName("帮我从网上找个电影，再我的媒体中心播放"); got != "" {
		t.Fatal(got)
	}
	if got := filmLookupName("夜访吸血鬼"); got != "夜访吸血鬼" {
		t.Fatal(got)
	}
	if got := filmLookupName("帮我播放电影 武状元苏乞儿"); got != "武状元苏乞儿" {
		t.Fatal(got)
	}
	// 用户点名时书名号里的就是片名，指令词绝不能混进搜索词。
	if got := filmLookupName("我要看《九品芝麻官》"); got != "九品芝麻官" {
		t.Fatal(got)
	}
	if got := filmLookupName("帮我播放一部 周星驰的电影 武状元苏乞儿"); got != "武状元苏乞儿" {
		t.Fatal(got)
	}
}

func TestMovieTitleMatchesGuardsAgainstUnrelatedHits(t *testing.T) {
	// 站点全文搜索会把「九品芝麻官」糊弄成《我要你的性》——必须拒绝。
	if movieTitleMatches("九品芝麻官", "我要你的性") {
		t.Fatal("an unrelated title must not match")
	}
	if !movieTitleMatches("九品芝麻官", "九品芝麻官(1994)国语中字") {
		t.Fatal("year and language suffixes must still match")
	}
	if !movieTitleMatches("武状元苏乞儿", "武状元苏乞儿之天降神谕") {
		t.Fatal("derived titles must still match")
	}
	if movieTitleMatches("", "x") || movieTitleMatches("x", "") {
		t.Fatal("empty sides never match")
	}
}

func TestRankExactTitleFirstPrefersTheExactMovie(t *testing.T) {
	// 南瓜真实返回：衍生作品排在原版前面，必须把标题完全一致的排到最前。
	type entry struct{ name string }
	for _, tc := range []struct {
		want  string
		given []entry
		first string
	}{
		{"九品芝麻官", []entry{{"新九品芝麻官2006"}, {"九品芝麻官"}, {"新九品芝麻官"}}, "九品芝麻官"},
		{"武状元苏乞儿", []entry{{"武状元苏乞儿之天降神谕"}, {"武状元苏乞儿"}}, "武状元苏乞儿"},
		// 没有完全一致的候选时保持站点原有顺序。
		{"赌侠", []entry{{"赌侠2之上海滩赌圣"}, {"赌圣"}}, "赌侠2之上海滩赌圣"},
	} {
		got := rankExactTitleFirst(tc.want, tc.given, func(e entry) string { return e.name })
		if got[0].name != tc.first {
			t.Fatalf("want=%s first=%s", tc.want, got[0].name)
		}
	}
}

func TestCasualMovieRequestPlaysAndMetropolisUsesItsCatalogTitle(t *testing.T) {
	if !genericCenterMovie("帮我找一部好看点的电影在媒体中心比方出来") {
		t.Fatal("a casual movie request must use the public-domain film")
	}
	if got := catalogLookupQuery("在媒体中心播放《大都会》"); got != "Metropolis" {
		t.Fatalf("lookup=%q", got)
	}
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(_ context.Context, query string) (string, string, string, bool) {
		if query != "Metropolis" {
			return "", "", "", false
		}
		return "https://upload.wikimedia.org/wikipedia/commons/a/a0/Metropolis.webm", "Metropolis", "video", true
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, songHit, movieHit)
	casual, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"帮我找一部好看点的电影在媒体中心比方出来"}`))
	if err != nil || strings.Contains(casual.Output, publicDomainMovieURL) || strings.Contains(casual.Output, "Metropolis") {
		t.Fatalf("a movie without a title must not be replaced: err=%v out=%s", err, casual.Output)
	}
	if !strings.Contains(casual.Output, "MEDIA_CENTER") || !strings.Contains(casual.Output, testNanguaM3U8) || !strings.Contains(casual.Output, "site: 南瓜影视") {
		t.Fatalf("a movie without a title plays the partner site's link: err=%v out=%s", err, casual.Output)
	}
	named, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"在媒体中心播放《大都会》"}`))
	if err != nil || !strings.Contains(named.Output, "MEDIA_CENTER") || !strings.Contains(named.Output, "Metropolis.webm") {
		t.Fatalf("metropolis err=%v out=%s", err, named.Output)
	}
}

func TestSongRequestDoesNotPlayTheMatchingFilm(t *testing.T) {
	prevResolve := resolveOpenMedia
	resolveOpenMedia = func(_ context.Context, query string) (string, string, string, bool) {
		if query == "Metropolis" {
			return "https://upload.wikimedia.org/wikipedia/commons/a/a0/Metropolis.webm", "Metropolis", "video", true
		}
		return "", "", "", false
	}
	t.Cleanup(func() { resolveOpenMedia = prevResolve })
	stubSiteResolvers(t, songHit, nil)
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放一首大都会的歌"}`))
	if err != nil || strings.Contains(out.Output, "Metropolis") || strings.Contains(out.Output, "metropolis") {
		t.Fatalf("a song must not play the film of the same name: err=%v out=%s", err, out.Output)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testKuwoMP3) || !strings.Contains(out.Output, "site: 酷我音乐") {
		t.Fatalf("a missing song plays the partner site's mp3: %s", out.Output)
	}
}

func TestMovieRequestsPlayInTheMediaCenter(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, songHit, movieHit)
	named := []string{
		"武状元苏乞儿",
		"帮我播放电影 武状元苏乞儿",
		"我想看夜访吸血鬼这部电影",
		"播放一部周星驰的电影，九品芝麻官",
		"我让你播放的是电影 不要歌曲",
	}
	// 没有点名片名（解析后名字为空）的请求仍然如实报错，不猜片子。
	unnamed := []string{
		"播放电影",
	}
	for _, query := range named {
		raw, err := json.Marshal(map[string]string{"action": "play", "target": "center", "query": query})
		if err != nil {
			t.Fatal(err)
		}
		out, err := (&Runtime{}).executeMediaCenter(context.Background(), raw)
		t.Logf("%s -> %s", query, strings.ReplaceAll(out.Output, "\n", " | "))
		if err != nil || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "Night of the Living Dead") || strings.Contains(out.Output, "iqiyi.com") || strings.Contains(out.Output, "youku.com") || strings.Contains(out.Output, "music.163.com") {
			t.Fatalf("%s played something else: err=%v out=%s", query, err, out.Output)
		}
		if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "site: 南瓜影视") {
			t.Fatalf("%s must play the partner site's link: err=%v out=%s", query, err, out.Output)
		}
	}
	for _, query := range unnamed {
		raw, err := json.Marshal(map[string]string{"action": "play", "target": "center", "query": query})
		if err != nil {
			t.Fatal(err)
		}
		out, err := (&Runtime{}).executeMediaCenter(context.Background(), raw)
		t.Logf("%s -> err=%v", query, err)
		if err == nil || !strings.Contains(err.Error(), "没有说出是哪一部片子") {
			t.Fatalf("%s must ask which film: err=%v out=%s", query, err, out.Output)
		}
	}
	if len(*opened) != 0 {
		t.Fatalf("a movie must stay in the media center: %v", *opened)
	}
}

func TestUnnamedFilmPlaysInTheMediaCenterNotOnNetease(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, songHit, movieHit)
	for _, query := range []string{
		"帮我找一部香港90年代的电影播放",
		"你试试爱奇艺能不能播放",
	} {
		out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","query":"`+query+`"}`))
		t.Logf("%s -> %s", query, strings.ReplaceAll(out.Output, "\n", " | "))
		if err != nil || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "music.163.com") || strings.Contains(out.Output, "youku.com") || strings.Contains(out.Output, "iqiyi.com") {
			t.Fatalf("%s must not open a member site: err=%v out=%s", query, err, out.Output)
		}
		if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "site: 南瓜影视") {
			t.Fatalf("%s must play the partner site's link: err=%v out=%s", query, err, out.Output)
		}
	}
	if len(*opened) != 0 {
		t.Fatalf("unnamed film must stay in the media center: %v", *opened)
	}
}

func TestMediaCenterSiteFallbackPlaysDirectLinks(t *testing.T) {
	opened := stubOfficialOpen(t)
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
	stubSiteResolvers(t, songHit, movieHit)
	// 歌曲在公版库里找不到：从酷我音乐拿到 mp3 直链，媒体中心直接播放。
	song, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放一首生所爱"}`))
	if err != nil || !strings.Contains(song.Output, "MEDIA_CENTER") || !strings.Contains(song.Output, testKuwoMP3) || !strings.Contains(song.Output, "kind: audio") || !strings.Contains(song.Output, "site: 酷我音乐") {
		t.Fatalf("song err=%v out=%s", err, song.Output)
	}
	// 电影在公版库里找不到：从南瓜影视拿到 m3u8 直链，媒体中心直接播放。
	film, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放九品芝麻官"}`))
	if err != nil || !strings.Contains(film.Output, "MEDIA_CENTER") || !strings.Contains(film.Output, testNanguaM3U8) || !strings.Contains(film.Output, "kind: video") || !strings.Contains(film.Output, "site: 南瓜影视") {
		t.Fatalf("film err=%v out=%s", err, film.Output)
	}
	// 对接网站也没有：如实报没有，不播别的内容。
	stubSiteResolvers(t,
		func(context.Context, string) (string, string, bool) { return "", "", false },
		func(context.Context, string, bool) (string, string, bool) { return "", "", false },
	)
	if _, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放一首不存在的歌"}`)); err == nil || !strings.Contains(err.Error(), "酷我音乐") {
		t.Fatalf("a song missing everywhere must be reported: err=%v", err)
	}
	if _, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"播放一部不存在的电影"}`)); err == nil || !strings.Contains(err.Error(), "南瓜影视") {
		t.Fatalf("a film missing everywhere must be reported: err=%v", err)
	}
	// 用户直接给 m3u8 直链：媒体中心按视频直接播放。
	direct, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","url":"https://vip.dytt-film.com/20250121/1309_4476b929/index.m3u8"}`))
	if err != nil || !strings.Contains(direct.Output, "MEDIA_CENTER") || !strings.Contains(direct.Output, "kind: video") {
		t.Fatalf("m3u8 err=%v out=%s", err, direct.Output)
	}
	// 网页地址仍然拒绝。
	if _, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","url":"https://example.com/movie.mp4.html"}`)); err == nil {
		t.Fatal("a web page must still be rejected")
	}
	if len(*opened) != 0 {
		t.Fatalf("media must stay inside the media center, not the system browser: %v", *opened)
	}
}

func stubCatalogSearchMiss(t *testing.T) {
	t.Helper()
	prevResolve := resolveOpenMedia
	prevSearch := searchForMediaCenter
	resolveOpenMedia = func(context.Context, string) (string, string, string, bool) {
		return "", "", "", false
	}
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{}, nil
	}
	t.Cleanup(func() {
		resolveOpenMedia = prevResolve
		searchForMediaCenter = prevSearch
	})
}

func TestMediaCenterSiteFallbackRejectsMismatchedSiteHits(t *testing.T) {
	stubCatalogSearchMiss(t)
	prevSites := movieSites
	// 站点全文搜索会把「九品芝麻官」糊弄成《我要你的性》：
	// strict 一轮必须拒绝所有对不上的标题，最后如实报没有，绝不乱播。
	movieSites = []movieSiteEntry{
		{name: "南瓜影视", resolve: func(_ context.Context, _ string, strict bool) (string, string, bool) {
			if strict {
				return "", "", false
			}
			return testNanguaM3U8, "我要你的性", true
		}},
	}
	t.Cleanup(func() { movieSites = prevSites })
	_, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"我要看《九品芝麻官》"}`))
	if err == nil || !strings.Contains(err.Error(), "九品芝麻官") || !strings.Contains(err.Error(), "南瓜影视") {
		t.Fatalf("mismatched hits must be rejected and reported: err=%v", err)
	}
}

func TestVagueActorQueryPlaysAfterStrictMiss(t *testing.T) {
	stubCatalogSearchMiss(t)
	prevSites := movieSites
	// 「随便放一部周星驰的电影」没点名片名：严格一轮落空后，
	// 用演员名放宽再搜一遍，搜到就播。
	movieSites = []movieSiteEntry{
		{name: "南瓜影视", resolve: func(_ context.Context, query string, strict bool) (string, string, bool) {
			if strict || query != "周星驰" {
				return "", "", false
			}
			return testNanguaM3U8, "赌侠", true
		}},
	}
	t.Cleanup(func() { movieSites = prevSites })
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","query":"随便放一部周星驰的电影"}`))
	if err != nil || !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, testNanguaM3U8) || !strings.Contains(out.Output, "title: 赌侠") || !strings.Contains(out.Output, "site: 南瓜影视") {
		t.Fatalf("a vague actor query must play after the strict round misses: err=%v out=%s", err, out.Output)
	}
}

func TestSongRequestFallsThroughToNeteaseAndStaysAudio(t *testing.T) {
	stubCatalogSearchMiss(t)
	prevSongs := songSites
	prevSites := movieSites
	// 「帮我播放一首歌曲 一生所爱」是歌：酷我没有时接着搜网易云音乐，
	// 绝不能把歌送去电影站（曾经从南瓜影视播过完全无关的《我》）。
	songSites = []songSiteEntry{
		{name: "酷我音乐", resolve: func(context.Context, string) (string, string, bool) { return "", "", false }},
		{name: "网易云音乐", resolve: func(_ context.Context, query string) (string, string, bool) {
			if query != "一生所爱" {
				return "", "", false
			}
			return testKuwoMP3, "一生所爱 - 卢冠廷", true
		}},
	}
	movieSites = []movieSiteEntry{
		{name: "南瓜影视", resolve: func(context.Context, string, bool) (string, string, bool) {
			t.Fatal("a song request must never reach the movie sites")
			return "", "", false
		}},
	}
	t.Cleanup(func() {
		songSites = prevSongs
		movieSites = prevSites
	})
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","query":"帮我播放一首歌曲 一生所爱"}`))
	if err != nil || !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, "kind: audio") || !strings.Contains(out.Output, "title: 一生所爱 - 卢冠廷") || !strings.Contains(out.Output, "site: 网易云音乐") {
		t.Fatalf("a song must fall through kuwo to netease as audio: err=%v out=%s", err, out.Output)
	}
}

func TestCenterWantAudioCoversThemeSongAndListenPhrasing(t *testing.T) {
	for _, query := range []string{
		"帮我播放一首歌曲 一生所爱",
		"播放大话西游的主题曲",
		"我想听一生所爱",
		"放一首歌",
		"来点音乐",
	} {
		if !centerWantAudio(query) {
			t.Fatalf("%q must be audio", query)
		}
	}
	for _, query := range []string{
		"我要看《九品芝麻官》",
		"随便放一部周星驰的电影",
		"帮我播放一部影片",
	} {
		if centerWantAudio(query) {
			t.Fatalf("%q must be video", query)
		}
	}
}
