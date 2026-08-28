package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/winexec"
)

type ccInvoker func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error)

var mediaSleep = time.Sleep
var activateWindow = winexec.ActivateWindowMatching
var sendForegroundPlay = winexec.SendMediaKey
var openLaunchPath = openWithDefaultApp

func ccPress(ctx context.Context, invoke ccInvoker, session, key string, approved bool) error {
	raw, err := json.Marshal(map[string]string{"key": key})
	if err != nil {
		return err
	}
	_, err = invoke(ctx, session, ccapp.ToolPress, raw, approved)
	return err
}

func ccShortcut(ctx context.Context, invoke ccInvoker, session string, approved bool, keys ...string) error {
	raw, err := json.Marshal(map[string][]string{"keys": keys})
	if err != nil {
		return err
	}
	_, err = invoke(ctx, session, ccapp.ToolKeyboardShortcut, raw, approved)
	return err
}

func ccType(ctx context.Context, invoke ccInvoker, session, text string, approved bool) error {
	raw, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	_, err = invoke(ctx, session, ccapp.ToolKeyboardType, raw, approved)
	return err
}

func ccCall(ctx context.Context, invoke ccInvoker, session, tool string, args map[string]any, approved bool) (Result, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return Result{}, err
	}
	return invoke(ctx, session, tool, raw, approved)
}

func ccClickName(ctx context.Context, invoke ccInvoker, session, name string, clicks int, approved bool) error {
	name = clipMediaName(name)
	if name == "" {
		return errors.New("empty click target")
	}
	if clicks < 1 {
		clicks = 1
	}
	_, err := ccCall(ctx, invoke, session, ccapp.ToolMouseClick, map[string]any{
		"name": name, "clicks": clicks, "button": "left",
	}, approved)
	return err
}

func ccObserveNodes(ctx context.Context, invoke ccInvoker, session string, approved bool) ([]mediaUINode, string, error) {
	res, err := ccCall(ctx, invoke, session, ccapp.ToolObserveUI, map[string]any{"maxNodes": 80}, approved)
	if err != nil {
		return nil, "", err
	}
	return parseObserveNodes(res.Output), res.Output, nil
}

func ccWindowTitle(ctx context.Context, invoke ccInvoker, session string, approved bool) string {
	res, err := ccCall(ctx, invoke, session, ccapp.ToolGetActiveWindow, map[string]any{}, approved)
	if err != nil {
		return ""
	}
	return res.Output
}

func fillSearchField(ctx context.Context, invoke ccInvoker, session string, field mediaUINode, query string, approved bool) error {
	target := clipMediaName(field.Name)
	role := strings.ToLower(strings.TrimSpace(field.Role))
	if target != "" {
		// SetValue first so tests and native Win32 edits still see a value
		// write. Electron (汽水音乐) often reports success without filling
		// the box — always follow with click + type.
		_, _ = ccCall(ctx, invoke, session, ccapp.ToolSetValue, map[string]any{
			"target": target, "value": query,
		}, approved)
		if err := ccClickName(ctx, invoke, session, target, 1, approved); err != nil && role != "edit" && role != "combobox" {
			return err
		}
	}
	mediaSleep(180 * time.Millisecond)
	if role == "button" || role == "menuitem" {
		// Magnifying-glass / "搜索" button: wait for the edit to appear.
		mediaSleep(220 * time.Millisecond)
	}
	_ = ccShortcut(ctx, invoke, session, approved, "ctrl", "a")
	mediaSleep(80 * time.Millisecond)
	if err := ccType(ctx, invoke, session, query, approved); err != nil {
		return err
	}
	mediaSleep(120 * time.Millisecond)
	return ccPress(ctx, invoke, session, "enter", approved)
}

func tryOpenSearchWithShortcuts(ctx context.Context, invoke ccInvoker, session string, approved bool) ([]mediaUINode, error) {
	for _, keys := range [][]string{
		{"ctrl", "f"},
		{"ctrl", "k"},
		{"ctrl", "l"},
	} {
		_ = ccShortcut(ctx, invoke, session, approved, keys...)
		mediaSleep(220 * time.Millisecond)
		nodes, _, err := ccObserveNodes(ctx, invoke, session, approved)
		if err != nil {
			return nil, err
		}
		if pickSearchNode(nodes) != nil {
			return nodes, nil
		}
	}
	nodes, _, err := ccObserveNodes(ctx, invoke, session, approved)
	return nodes, err
}

func activateAnyWindow(hints []string) error {
	var last error
	tried := false
	for _, h := range hints {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		tried = true
		if err := activateWindow(h); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if !tried {
		return errors.New("no window matching music app")
	}
	return last
}

func ensureMusicAppForeground(app string) (string, error) {
	hints := musicWindowHints(app)
	if err := activateAnyWindow(hints); err == nil {
		return "", nil
	}
	path, _, err := pickLaunchTarget(app)
	if err != nil || path == "" {
		if err == nil {
			err = errors.New("not found")
		}
		return "", fmt.Errorf("未能打开桌面应用「%s」: %w", app, err)
	}
	if err := openLaunchPath(path); err != nil {
		return "", err
	}
	mediaSleep(2200 * time.Millisecond)
	hints = append(hints, path, filepath.Base(path))
	_ = activateAnyWindow(hints)
	return path, nil
}

func executeMediaPlayForeground(ctx context.Context, invoke ccInvoker, session, query, appHint string, approved, unconfined bool) (Result, error) {
	if !unconfined {
		return Result{}, errors.New("media.play foreground requires full-disk full-access")
	}
	if invoke == nil {
		return Result{}, errors.New("media.play foreground requires computer control (cc.*)")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return Result{}, errors.New("query required for foreground playback")
	}
	if !looksPrintableQuery(q) {
		return Result{}, errors.New("query required for foreground playback")
	}
	app := strings.TrimSpace(appHint)
	if app == "" {
		app = q
	}
	opened, err := ensureMusicAppForeground(app)
	if err != nil {
		return Result{}, err
	}
	focus := app
	if known, ok := matchKnownLaunchApp(app); ok {
		focus = known.Canonical
	}
	_, _ = ccCall(ctx, invoke, session, ccapp.ToolWindowFocus, map[string]any{"title": focus}, approved)
	mediaSleep(450 * time.Millisecond)
	res, err := playNamedTrackInForeground(ctx, invoke, session, q, focus, approved)
	if err != nil {
		return res, err
	}
	if opened != "" && strings.HasPrefix(res.Output, "verified") {
		res.Output = "opened " + opened + "; " + res.Output
	}
	return res, nil
}

func playNamedTrackInForeground(ctx context.Context, invoke ccInvoker, session, query, app string, approved bool) (Result, error) {
	label := strings.TrimSpace(app)
	if label == "" {
		label = "foreground app"
	}

	tryClick := func(name, how string) (Result, bool) {
		name = clipMediaName(name)
		if name == "" {
			return Result{}, false
		}
		if err := ccClickName(ctx, invoke, session, name, 2, approved); err != nil {
			return Result{}, false
		}
		mediaSleep(700 * time.Millisecond)
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		title := ccWindowTitle(ctx, invoke, session, approved)
		if !nowPlayingConfirmed(nodes, title, query) {
			return Result{}, false
		}
		return result(fmt.Sprintf("verified playing %q in %s (%s)", query, label, how)), true
	}

	acceptSearchPlayback := func(clicked, how string) (Result, bool) {
		clicked = clipMediaName(clicked)
		if clicked == "" || isMediaNavName(clicked) {
			return Result{}, false
		}
		if err := ccClickName(ctx, invoke, session, clicked, 2, approved); err != nil {
			return Result{}, false
		}
		mediaSleep(700 * time.Millisecond)
		nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
		title := ccWindowTitle(ctx, invoke, session, approved)
		if nowPlayingConfirmed(nodes, title, query) {
			return result(fmt.Sprintf("verified playing %q in %s (%s)", query, label, how)), true
		}
		if nowPlayingConfirmed(nodes, title, clicked) {
			return result(fmt.Sprintf("verified playing %q in %s (%s)", clicked, label, how)), true
		}
		// Media keys only after a search result was clicked — never as the
		// only action, and never before the search box was used.
		if queryLooksLikeArtist(query) {
			_ = sendForegroundPlay("play")
			return result(fmt.Sprintf("started playing a result for %q in %s (%s)", query, label, how)), true
		}
		return Result{}, false
	}

	mustSearch := queryMustSearchFirst(query)
	didSearch := false

	if !mustSearch && !isGenericMediaQuery(query) {
		if res, ok := tryClick(query, "clicked named track"); ok {
			return res, nil
		}
	}

	nodes, _, err := ccObserveNodes(ctx, invoke, session, approved)
	if err != nil {
		return Result{}, fmt.Errorf("observe music UI: %w", err)
	}
	if !mustSearch {
		if track := pickTrackNode(nodes, query); track != nil {
			if res, ok := tryClick(track.Name, "clicked list item"); ok {
				return res, nil
			}
		}
	}

	if search := pickSearchNode(nodes); search != nil && strings.EqualFold(search.Role, "button") {
		_ = ccClickName(ctx, invoke, session, clipMediaName(search.Name), 1, approved)
		mediaSleep(280 * time.Millisecond)
		if next, _, obsErr := ccObserveNodes(ctx, invoke, session, approved); obsErr == nil {
			nodes = next
			if edit := pickSearchNode(nodes); edit != nil {
				search = edit
			}
		}
	}
	if search := pickSearchNode(nodes); search == nil {
		if next, openErr := tryOpenSearchWithShortcuts(ctx, invoke, session, approved); openErr == nil {
			nodes = next
		}
	}
	if search := pickSearchNode(nodes); search != nil {
		if err := fillSearchField(ctx, invoke, session, *search, query, approved); err != nil {
			return Result{}, fmt.Errorf("type search query: %w", err)
		}
		didSearch = true
		mediaSleep(900 * time.Millisecond)
		nodes, _, err = ccObserveNodes(ctx, invoke, session, approved)
		if err != nil {
			return Result{}, fmt.Errorf("observe search results: %w", err)
		}
		if track := pickTrackNode(nodes, query); track != nil {
			if res, ok := tryClick(track.Name, "clicked search result"); ok {
				return res, nil
			}
			if res, ok := acceptSearchPlayback(track.Name, "clicked search result"); ok {
				return res, nil
			}
		}
		if !isGenericMediaQuery(query) {
			if res, ok := tryClick(query, "clicked named result after search"); ok {
				return res, nil
			}
		}
		if first := pickFirstPlayable(nodes); first != nil {
			if res, ok := acceptSearchPlayback(first.Name, "clicked first search result"); ok {
				return res, nil
			}
		}
	}

	visible := summarizeNodeNames(nodes, 14)
	if mustSearch && !didSearch {
		return Result{}, fmt.Errorf("未能在桌面「%s」里找到搜索框，无法搜索「%s」。可见控件：%s", label, query, visible)
	}
	return Result{}, fmt.Errorf("未能核对正在播放「%s」。没有点到同名列表项，也没有用系统播放键（以免继续播当前曲）。可见控件：%s", query, visible)
}

func foregroundAppHint(app, path string) string {
	app = strings.TrimSpace(app)
	if app != "" {
		return app
	}
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return base
}
