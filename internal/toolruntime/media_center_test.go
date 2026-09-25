package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/webfetch"
)

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

func TestMediaCenterPicksArchiveDownloadAndSkipsHTML(t *testing.T) {
	prev := searchForMediaCenter
	searchForMediaCenter = func(*Runtime, context.Context, string) (webSearchResponse, error) {
		return webSearchResponse{Results: []webfetch.SearchResult{
			{Title: "details", URL: "https://archive.org/details/night_of_the_living_dead"},
			{Title: "Night of the Living Dead", URL: "https://archive.org/download/night_of_the_living_dead/Night.mp4"},
		}}, nil
	}
	t.Cleanup(func() { searchForMediaCenter = prev })
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"随便找个电影"}`))
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
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"帮我找到一个爱情电影再媒体中心可以播放出来"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "MEDIA_CENTER") || !strings.Contains(out.Output, publicDomainMovieURL) || !strings.Contains(out.Output, "kind: video") {
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
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"Nosferatu"}`))
	if err != nil || strings.Contains(out.Output, "cdn.example") || strings.Contains(out.Output, publicDomainMovieURL) || !strings.Contains(out.Output, "https://www.iqiyi.com/so/q_Nosferatu") || !strings.Contains(out.Output, "https://so.youku.com/search_video/q_Nosferatu") {
		t.Fatalf("a catalog URL outside the open libraries must not play; the official pages should: err=%v out=%s", err, out.Output)
	}
	if len(*opened) != 2 {
		t.Fatalf("official pages were not opened: %v", *opened)
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
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"夜访吸血鬼"}`))
	if err != nil || strings.Contains(out.Output, "MEDIA_CENTER") || strings.Contains(out.Output, publicDomainMovieURL) || strings.Contains(out.Output, "night_of_the_living_dead") || !strings.Contains(out.Output, "https://www.iqiyi.com/so/q_%E5%A4%9C%E8%AE%BF%E5%90%B8%E8%A1%80%E9%AC%BC") || !strings.Contains(out.Output, "https://so.youku.com/search_video/q_%E5%A4%9C%E8%AE%BF%E5%90%B8%E8%A1%80%E9%AC%BC") {
		t.Fatalf("named title with no catalog file must open the official pages, not a search hit: err=%v out=%s", err, out.Output)
	}
	if len(*opened) != 2 {
		t.Fatalf("official pages were not opened: %v", *opened)
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
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"action":"play","target":"center","query":"播放一部周星驰的电影，九品芝麻官"}`))
	if err != nil || called {
		t.Fatalf("named film must not wait on a catalog lookup: called=%v err=%v", called, err)
	}
	if strings.Contains(out.Output, "Night of the Living Dead") || strings.Contains(out.Output, "MEDIA_CENTER\n") || !strings.Contains(out.Output, "iqiyi.com/so/q_") || !strings.Contains(out.Output, "youku.com/search_video/q_") || !strings.Contains(out.Output, "九品芝麻官") {
		t.Fatal(out.Output)
	}
	if len(*opened) != 2 || !strings.Contains((*opened)[0], "iqiyi.com/so/q_") || !strings.Contains((*opened)[1], "youku.com/search_video/q_") {
		t.Fatalf("official pages were not opened: %v", *opened)
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
	out, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"我想看夜访吸血鬼这部电影"}`))
	iqiyi, youku := memberFilmSearchURLs("我想看夜访吸血鬼这部电影")
	if err != nil || iqiyi == "" || !strings.Contains(out.Output, iqiyi) || !strings.Contains(out.Output, youku) || !strings.Contains(out.Output, memberLoginNote) || strings.Contains(out.Output, publicDomainMovieTitle) {
		t.Fatalf("named title with no file must open the official pages: err=%v out=%s", err, out.Output)
	}
	if len(*opened) != 2 || (*opened)[0] != iqiyi || (*opened)[1] != youku {
		t.Fatalf("opened %v want %s %s", *opened, iqiyi, youku)
	}
	if genericIQ, _ := memberFilmSearchURLs("帮我找一部好看点的电影在媒体中心比方出来"); genericIQ != "" {
		t.Fatal(genericIQ)
	}
}

func TestMediaCenterSearchQueryUsesAPublicDomainDefault(t *testing.T) {
	if got := mediaCenterSearchQuery("帮我在媒体中心播放一部电影"); !strings.Contains(got, "Night of the Living Dead") {
		t.Fatal(got)
	}
	if got := mediaCenterSearchQuery("帮我从网上找个电影，再我的媒体中心播放"); !strings.Contains(got, "Night of the Living Dead") {
		t.Fatal(got)
	}
	if got := mediaCenterSearchQuery("夜访吸血鬼"); got != "site:archive.org/download 夜访吸血鬼" {
		t.Fatal(got)
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
	casual, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"帮我找一部好看点的电影在媒体中心比方出来"}`))
	if err != nil || !strings.Contains(casual.Output, "MEDIA_CENTER") || !strings.Contains(casual.Output, publicDomainMovieURL) {
		t.Fatalf("casual play err=%v out=%s", err, casual.Output)
	}
	named, err := (&Runtime{}).executeMediaCenter(context.Background(), json.RawMessage(`{"target":"center","query":"在媒体中心播放《大都会》"}`))
	if err != nil || !strings.Contains(named.Output, "MEDIA_CENTER") || !strings.Contains(named.Output, "Metropolis.webm") {
		t.Fatalf("metropolis err=%v out=%s", err, named.Output)
	}
}
