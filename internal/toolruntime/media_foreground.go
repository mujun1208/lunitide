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
	if target != "" {
		if _, err := ccCall(ctx, invoke, session, ccapp.ToolSetValue, map[string]any{
			"target": target, "value": query,
		}, approved); err == nil {
			return nil
		}
		if err := ccClickName(ctx, invoke, session, target, 1, approved); err != nil {
			return err
		}
	}
	mediaSleep(180 * time.Millisecond)
	_ = ccShortcut(ctx, invoke, session, approved, "ctrl", "a")
	mediaSleep(80 * time.Millisecond)
	return ccType(ctx, invoke, session, query, approved)
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
	if err := winexec.ActivateWindowMatching(app); err != nil {
		return Result{}, err
	}
	_, _ = ccCall(ctx, invoke, session, ccapp.ToolWindowFocus, map[string]any{"title": app}, approved)
	mediaSleep(450 * time.Millisecond)
	return playNamedTrackInForeground(ctx, invoke, session, q, app, approved)
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

	if !isGenericMediaQuery(query) {
		if res, ok := tryClick(query, "clicked named track"); ok {
			return res, nil
		}
	}

	nodes, _, err := ccObserveNodes(ctx, invoke, session, approved)
	if err != nil {
		return Result{}, fmt.Errorf("observe music UI: %w", err)
	}
	if track := pickTrackNode(nodes, query); track != nil {
		if res, ok := tryClick(track.Name, "clicked list item"); ok {
			return res, nil
		}
	}

	if search := pickSearchNode(nodes); search != nil {
		if err := fillSearchField(ctx, invoke, session, *search, query, approved); err != nil {
			return Result{}, fmt.Errorf("type search query: %w", err)
		}
		mediaSleep(900 * time.Millisecond)
		nodes, _, err = ccObserveNodes(ctx, invoke, session, approved)
		if err != nil {
			return Result{}, fmt.Errorf("observe search results: %w", err)
		}
		if track := pickTrackNode(nodes, query); track != nil {
			if res, ok := tryClick(track.Name, "clicked search result"); ok {
				return res, nil
			}
		}
		if !isGenericMediaQuery(query) {
			if res, ok := tryClick(query, "clicked named result after search"); ok {
				return res, nil
			}
		}
	}

	visible := summarizeNodeNames(nodes, 14)
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
