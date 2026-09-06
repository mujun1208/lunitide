package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

var openMediaURL = openHTTPURL

// validateMediaURL is the only door between a model-chosen address and the
// operating system's protocol handlers. Without the scheme gate media.play
// opens file:, the ms-* diagnostic handlers and UNC paths — every one of them
// a local program launch dressed up as "play this".
func validateMediaURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", errors.New("url required")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", errors.New("media.play 的 url 无法解析")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("media.play 只接受 http/https 链接，拒绝 %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("media.play 的 url 缺少主机名")
	}
	// Returned as given rather than re-serialized: the search addresses this
	// package builds are already encoded, and a round trip through String()
	// would be a silent chance to alter someone's query.
	return u, nil
}

// mediaOpenArgv keeps the launch shell-free. `cmd /c start` re-parses & | < >
// ^ in the address as command separators, and Go only quotes an argv element
// that holds a space or a quote, so a query string was enough to inject.
func mediaOpenArgv(u string) []string {
	if runtime.GOOS == "windows" {
		return []string{"rundll32.exe", "url.dll,FileProtocolHandler", u}
	}
	return []string{"xdg-open", u}
}

func openHTTPURL(raw string) error {
	u, err := validateMediaURL(raw)
	if err != nil {
		return err
	}
	argv := mediaOpenArgv(u)
	return exec.Command(argv[0], argv[1:]...).Start()
}

func queryIsKnownMusicApp(query string) bool {
	return CanonicalMusicApp(strings.TrimSpace(query)) != ""
}

func mediaPlayUsesDesktop(target, url string) bool {
	if strings.TrimSpace(url) != "" {
		return false
	}
	return strings.ToLower(strings.TrimSpace(target)) != "browser"
}

func resolveDesktopPlayApp(target, app string) string {
	app = strings.TrimSpace(app)
	if canon := CanonicalMusicApp(app); canon != "" {
		return canon
	}
	if from := CanonicalMusicAppFromText(app); from != "" {
		return from
	}
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "netease", "163", "cloudmusic":
		return "网易云音乐"
	case "qq", "qqmusic":
		return "QQ音乐"
	}
	if app != "" {
		return app
	}
	return FirstInstalledMusicApp()
}

func buildMediaSearchURL(target, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", errors.New("query required for search playback")
	}
	enc := url.QueryEscape(q)
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "netease", "cloudmusic", "163":
		return "https://music.163.com/#/search/m/?s=" + enc, nil
	case "qq", "qqmusic":
		return "https://y.qq.com/n/ryqq/search?w=" + enc, nil
	default:
		return "https://music.youtube.com/search?q=" + enc, nil
	}
}

func executeMediaPlay(args json.RawMessage, unconfined bool) (Result, error) {
	return executeMediaPlayWithCC(context.Background(), nil, "", args, unconfined, unconfined)
}

func executeMediaPlayWithCC(ctx context.Context, invoke ccInvoker, session string, args json.RawMessage, unconfined, approved bool) (Result, error) {
	var a struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		URL    string `json:"url"`
		Target string `json:"target"`
		App    string `json:"app"`
	}
	if strict(args, &a) != nil {
		return Result{}, errors.New("invalid arguments")
	}
	if err := requireDesktopAction(approved); err != nil {
		return Result{}, err
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		action = "play"
	}
	target := strings.ToLower(strings.TrimSpace(a.Target))
	switch action {
	case "play", "open_and_play":
		q := strings.TrimSpace(a.Query)
		if mediaPlayUsesDesktop(target, a.URL) && q != "" && !queryIsKnownMusicApp(q) {
			app := resolveDesktopPlayApp(target, a.App)
			if app == "" {
				return Result{}, errors.New("没有找到本机桌面播放器，无法搜索播放")
			}
			return executeMediaPlayForeground(ctx, invoke, session, q, foregroundAppHint(app, ""), approved, unconfined)
		}
		u := strings.TrimSpace(a.URL)
		if u == "" && q != "" && target == "browser" {
			var err error
			u, err = buildMediaSearchURL(a.Target, a.Query)
			if err != nil {
				return Result{}, err
			}
		}
		if u != "" {
			// Checked here, not only inside the opener, so a replaced opener
			// cannot become a way around the scheme gate.
			checked, err := validateMediaURL(u)
			if err != nil {
				return Result{}, err
			}
			u = checked
			if err := openMediaURL(u); err != nil {
				return Result{}, err
			}
			time.Sleep(1800 * time.Millisecond)
		}
		if err := winexec.SendMediaKey("play"); err != nil {
			return Result{}, err
		}
		if u != "" {
			return result(appendL0JSON("opened "+u+" and sent play", "url", true, false, u)), nil
		}
		return result(appendL0JSON("sent play to the active media app", "foreground", false, true, "no now-playing")), nil
	case "pause", "toggle":
		if err := winexec.SendMediaKey("pause"); err != nil {
			return Result{}, err
		}
		return result("sent pause/play toggle"), nil
	case "next", "skip":
		if err := winexec.SendMediaKey("next"); err != nil {
			return Result{}, err
		}
		return result("sent next track"), nil
	case "prev", "previous":
		if err := winexec.SendMediaKey("prev"); err != nil {
			return Result{}, err
		}
		return result("sent previous track"), nil
	case "stop":
		if err := winexec.SendMediaKey("stop"); err != nil {
			return Result{}, err
		}
		return result("sent stop"), nil
	case "open":
		q := strings.TrimSpace(a.Query)
		if mediaPlayUsesDesktop(target, a.URL) && q != "" {
			app := resolveDesktopPlayApp(target, a.App)
			if app == "" {
				return Result{}, errors.New("没有找到本机桌面播放器，无法搜索播放")
			}
			return executeMediaPlayForeground(ctx, invoke, session, q, foregroundAppHint(app, ""), approved, unconfined)
		}
		u := strings.TrimSpace(a.URL)
		if u == "" && q != "" && target == "browser" {
			var err error
			u, err = buildMediaSearchURL(a.Target, a.Query)
			if err != nil {
				return Result{}, err
			}
		}
		if u == "" {
			return Result{}, errors.New("media.play open needs url or query")
		}
		checked, err := validateMediaURL(u)
		if err != nil {
			return Result{}, err
		}
		u = checked
		if err := openMediaURL(u); err != nil {
			return Result{}, err
		}
		return result(appendL0JSON("opened "+u, "url", true, false, u)), nil
	default:
		return Result{}, fmt.Errorf("unknown media.play action %q", action)
	}
}
