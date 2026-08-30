package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
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

// mediaInputWindow is forwarded as cc.* window= so Space/Ctrl+F land in the
// player, not the companion overlay (OpenClaw: focus the target first).
var mediaInputWindow string

var imageCoordRe = regexp.MustCompile(`(?i)use image coordinates (\d+)x(\d+)`)

func ccFocusArgs(args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	w := strings.TrimSpace(mediaInputWindow)
	if w != "" && !strings.EqualFold(w, "foreground app") {
		args["window"] = w
	}
	return args
}

func ccPress(ctx context.Context, invoke ccInvoker, session, key string, approved bool) error {
	_, err := ccCall(ctx, invoke, session, ccapp.ToolPress, ccFocusArgs(map[string]any{"key": key}), approved)
	return err
}

func ccShortcut(ctx context.Context, invoke ccInvoker, session string, approved bool, keys ...string) error {
	_, err := ccCall(ctx, invoke, session, ccapp.ToolKeyboardShortcut, ccFocusArgs(map[string]any{"keys": keys}), approved)
	return err
}

func ccType(ctx context.Context, invoke ccInvoker, session, text string, approved bool) error {
	_, err := ccCall(ctx, invoke, session, ccapp.ToolKeyboardType, ccFocusArgs(map[string]any{"text": text}), approved)
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

func ccClickXY(ctx context.Context, invoke ccInvoker, session string, x, y, clicks int, approved bool) error {
	if clicks < 1 {
		clicks = 1
	}
	_, err := ccCall(ctx, invoke, session, ccapp.ToolMouseClick, map[string]any{
		"x": x, "y": y, "clicks": clicks, "button": "left",
	}, approved)
	return err
}

func parseImageSize(output string) (int, int) {
	m := imageCoordRe.FindStringSubmatch(output)
	if len(m) != 3 {
		return 0, 0
	}
	w, errW := strconv.Atoi(m[1])
	h, errH := strconv.Atoi(m[2])
	if errW != nil || errH != nil || w < 8 || h < 8 {
		return 0, 0
	}
	return w, h
}

func ccCaptureForeground(ctx context.Context, invoke ccInvoker, session string, approved bool) (int, int, error) {
	res, err := ccCall(ctx, invoke, session, ccapp.ToolScreenCapture, map[string]any{"target": "foreground"}, approved)
	if err != nil {
		return 0, 0, err
	}
	w, h := parseImageSize(res.Output)
	return w, h, nil
}

func ccWaitChange(ctx context.Context, invoke ccInvoker, session string, ms int, approved bool) bool {
	if ms < 1 {
		ms = 400
	}
	res, err := ccCall(ctx, invoke, session, ccapp.ToolWait, map[string]any{"ms": ms, "until": "change"}, approved)
	if err != nil {
		return false
	}
	out := strings.ToLower(res.Output)
	if out == "" || strings.Contains(out, "unchanged") {
		return false
	}
	return strings.Contains(out, "captured") || strings.Contains(out, "updated") || strings.Contains(out, "change")
}

func playClickPoints(w, h int) [][2]int {
	if w < 40 || h < 40 {
		return nil
	}
	return [][2]int{
		{w / 2, h * 92 / 100},
		{w * 45 / 100, h * 93 / 100},
	}
}

func snapshotMediaUI(ctx context.Context, invoke ccInvoker, session string, approved bool) ([]mediaUINode, string) {
	nodes, _, _ := ccObserveNodes(ctx, invoke, session, approved)
	return nodes, ccWindowTitle(ctx, invoke, session, approved)
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

// tryBlindKeyboardSearch types into a search box that MSAA cannot see
// (OpenClaw: accessibility empty → app hotkey + type). 汽水: Ctrl+F.
func tryBlindKeyboardSearch(ctx context.Context, invoke ccInvoker, session, query string, approved bool) error {
	query = strings.TrimSpace(query)
	if query == "" || isGenericMediaQuery(query) {
		return errors.New("no search query")
	}
	_ = ccShortcut(ctx, invoke, session, approved, "ctrl", "f")
	mediaSleep(250 * time.Millisecond)
	_ = ccShortcut(ctx, invoke, session, approved, "ctrl", "a")
	mediaSleep(80 * time.Millisecond)
	if err := ccType(ctx, invoke, session, query, approved); err != nil {
		return err
	}
	mediaSleep(120 * time.Millisecond)
	if err := ccPress(ctx, invoke, session, "enter", approved); err != nil {
		return err
	}
	mediaSleep(700 * time.Millisecond)
	return nil
}

func confirmGenericPlayback(nodes []mediaUINode, title, app, how string, pixelsChanged bool) (Result, bool) {
	label := strings.TrimSpace(app)
	if label == "" {
		label = "foreground app"
	}
	if playbackLooksPaused(nodes) {
		return Result{}, false
	}
	if playbackLooksActive(nodes, title, app) {
		return result(fmt.Sprintf("verified playing in %s (%s)", label, how)), true
	}
	if uiaTreeSparse(nodes) && pixelsChanged {
		return result(fmt.Sprintf("started playing in %s (%s; accessibility empty)", label, how)), true
	}
	return Result{}, false
}

// trySparseTreePlay is the OpenClaw ladder after UIA/MSAA is empty: the
// app's real hotkeys (Space), then screenshot-coordinate click on the
// player bar, then WM_APPCOMMAND play. Never claims success if 播放 is
// still visible (still paused).
func trySparseTreePlay(ctx context.Context, invoke ccInvoker, session, app string, approved bool) (Result, bool) {
	nodes, title := snapshotMediaUI(ctx, invoke, session, approved)
	if res, ok := confirmGenericPlayback(nodes, title, app, "already playing", false); ok {
		return res, true
	}

	changed := false
	markChange := func() {
		if ccWaitChange(ctx, invoke, session, 700, approved) {
			changed = true
		}
	}

	if playbackLooksPaused(nodes) || uiaTreeSparse(nodes) {
		_ = ccPress(ctx, invoke, session, "space", approved)
		mediaSleep(400 * time.Millisecond)
		markChange()
		nodes, title = snapshotMediaUI(ctx, invoke, session, approved)
		if res, ok := confirmGenericPlayback(nodes, title, app, "keyboard space", changed); ok {
			return res, true
		}
	}

	if uiaTreeSparse(nodes) || playbackLooksPaused(nodes) {
		if w, h, err := ccCaptureForeground(ctx, invoke, session, approved); err == nil {
			for _, pt := range playClickPoints(w, h) {
				if err := ccClickXY(ctx, invoke, session, pt[0], pt[1], 1, approved); err != nil {
					continue
				}
				mediaSleep(450 * time.Millisecond)
				markChange()
				nodes, title = snapshotMediaUI(ctx, invoke, session, approved)
				if res, ok := confirmGenericPlayback(nodes, title, app, "screenshot play click", changed); ok {
					return res, true
				}
				if playbackLooksPaused(nodes) {
					continue
				}
			}
		}
	}

	if playbackLooksPaused(nodes) || uiaTreeSparse(nodes) {
		_ = sendForegroundPlay("play")
		mediaSleep(500 * time.Millisecond)
		markChange()
		nodes, title = snapshotMediaUI(ctx, invoke, session, approved)
		if res, ok := confirmGenericPlayback(nodes, title, app, "media play key", changed); ok {
			return res, true
		}
	}
	return Result{}, false
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
	if err := requireDesktopAction(approved); err != nil {
		return Result{}, err
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
	// OpenClaw computer-use: focus → accessibility (MSAA/UIA) → app
	// hotkeys → screenshot click. Electron 汽水 often has an empty tree.
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
	prevWin := mediaInputWindow
	if label != "foreground app" {
		mediaInputWindow = label
	}
	defer func() { mediaInputWindow = prevWin }()

	generic := isGenericMediaQuery(query)

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
		if playbackLooksPaused(nodes) {
			return Result{}, false
		}
		if generic {
			return confirmGenericPlayback(nodes, title, label, how, false)
		}
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
		if playbackLooksPaused(nodes) {
			return Result{}, false
		}
		if generic {
			return confirmGenericPlayback(nodes, title, label, how, false)
		}
		if nowPlayingConfirmed(nodes, title, query) {
			return result(fmt.Sprintf("verified playing %q in %s (%s)", query, label, how)), true
		}
		if nowPlayingConfirmed(nodes, title, clicked) {
			return result(fmt.Sprintf("verified playing %q in %s (%s)", clicked, label, how)), true
		}
		// Media keys only after a search result was clicked for a short
		// artist query — never as the only action, and never for a song
		// title that simply failed now-playing verify.
		if queryMustSearchFirst(query) {
			_ = sendForegroundPlay("play")
			return result(fmt.Sprintf("started playing a result for %q in %s (%s)", query, label, how)), true
		}
		return Result{}, false
	}

	mustSearch := queryMustSearchFirst(query)
	didSearch := false

	nodes, title := snapshotMediaUI(ctx, invoke, session, approved)
	if generic && playbackLooksActive(nodes, title, label) {
		return result(fmt.Sprintf("verified already playing in %s", label)), nil
	}

	if !mustSearch && !generic {
		if res, ok := tryClick(query, "clicked named track"); ok {
			return res, nil
		}
	}

	if !mustSearch {
		if track := pickTrackNode(nodes, query); track != nil {
			if res, ok := tryClick(track.Name, "clicked list item"); ok {
				return res, nil
			}
		}
	}

	if generic {
		if play := pickPlayControl(nodes); play != nil {
			_ = ccClickName(ctx, invoke, session, clipMediaName(play.Name), 1, approved)
			mediaSleep(700 * time.Millisecond)
			n2, t2 := snapshotMediaUI(ctx, invoke, session, approved)
			if res, ok := confirmGenericPlayback(n2, t2, label, "clicked play", false); ok {
				return res, nil
			}
		}
		if rec := pickRecommendNav(nodes); rec != nil {
			_ = ccClickName(ctx, invoke, session, clipMediaName(rec.Name), 1, approved)
			mediaSleep(500 * time.Millisecond)
			n2, _ := snapshotMediaUI(ctx, invoke, session, approved)
			if first := pickFirstPlayable(n2); first != nil {
				if res, ok := acceptSearchPlayback(first.Name, "clicked recommend item"); ok {
					return res, nil
				}
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
	if search := pickSearchNode(nodes); search == nil && !generic {
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
		var err error
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
		if !generic {
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

	if !generic && !didSearch {
		if err := tryBlindKeyboardSearch(ctx, invoke, session, query, approved); err == nil {
			didSearch = true
			mediaSleep(500 * time.Millisecond)
			nodes, _ = snapshotMediaUI(ctx, invoke, session, approved)
			if track := pickTrackNode(nodes, query); track != nil {
				if res, ok := tryClick(track.Name, "clicked search result"); ok {
					return res, nil
				}
				if mustSearch {
					if res, ok := acceptSearchPlayback(track.Name, "keyboard search result"); ok {
						return res, nil
					}
				}
			}
			if mustSearch {
				if first := pickFirstPlayable(nodes); first != nil {
					if res, ok := acceptSearchPlayback(first.Name, "keyboard first result"); ok {
						return res, nil
					}
				}
			}
			_ = ccPress(ctx, invoke, session, "enter", approved)
			mediaSleep(400 * time.Millisecond)
			n2, t2 := snapshotMediaUI(ctx, invoke, session, approved)
			if !playbackLooksPaused(n2) && nowPlayingConfirmed(n2, t2, query) {
				return result(fmt.Sprintf("verified playing %q in %s (keyboard search)", query, label)), nil
			}
		}
	}

	if generic {
		if res, ok := trySparseTreePlay(ctx, invoke, session, label, approved); ok {
			return res, nil
		}
	}

	nodes, _ = snapshotMediaUI(ctx, invoke, session, approved)
	visible := summarizeNodeNames(nodes, 14)
	if playbackLooksPaused(nodes) {
		return Result{}, fmt.Errorf("未能开始播放「%s」（仍暂停）。可见控件：%s", query, visible)
	}
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
