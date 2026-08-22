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

func openHTTPURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return errors.New("url required")
	}
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "start", "", u).Start()
	}
	return exec.Command("xdg-open", u).Start()
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
	return executeMediaPlayWithCC(context.Background(), nil, "", args, unconfined, false)
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
	if !unconfined {
		return Result{}, errors.New("media.play requires full-disk full-access")
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		action = "play"
	}
	target := strings.ToLower(strings.TrimSpace(a.Target))
	switch action {
	case "play", "open_and_play":
		if (target == "foreground" || target == "app" || target == "desktop") && strings.TrimSpace(a.Query) != "" {
			return executeMediaPlayForeground(ctx, invoke, session, a.Query, foregroundAppHint(a.App, ""), approved, unconfined)
		}
		u := strings.TrimSpace(a.URL)
		if u == "" && strings.TrimSpace(a.Query) != "" {
			var err error
			u, err = buildMediaSearchURL(a.Target, a.Query)
			if err != nil {
				return Result{}, err
			}
		}
		if u != "" {
			if err := openHTTPURL(u); err != nil {
				return Result{}, err
			}
			time.Sleep(1800 * time.Millisecond)
		}
		if err := winexec.SendMediaKey("play"); err != nil {
			return Result{}, err
		}
		if u != "" {
			return result("opened " + u + " and sent play"), nil
		}
		return result("sent play to the active media app"), nil
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
		u := strings.TrimSpace(a.URL)
		if u == "" && strings.TrimSpace(a.Query) != "" {
			var err error
			u, err = buildMediaSearchURL(a.Target, a.Query)
			if err != nil {
				return Result{}, err
			}
		}
		if u == "" {
			return Result{}, errors.New("media.play open needs url or query")
		}
		if err := openHTTPURL(u); err != nil {
			return Result{}, err
		}
		return result("opened " + u), nil
	default:
		return Result{}, fmt.Errorf("unknown media.play action %q", action)
	}
}
