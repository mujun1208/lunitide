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
	app := strings.TrimSpace(appHint)
	if app == "" {
		app = q
	}
	if err := winexec.ActivateWindowMatching(app); err != nil {
		return Result{}, err
	}
	time.Sleep(700 * time.Millisecond)
	if err := ccShortcut(ctx, invoke, session, approved, "ctrl", "f"); err != nil {
		return Result{}, fmt.Errorf("open in-app search: %w", err)
	}
	time.Sleep(350 * time.Millisecond)
	if err := ccType(ctx, invoke, session, q, approved); err != nil {
		return Result{}, fmt.Errorf("type search query: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	if err := ccShortcut(ctx, invoke, session, approved, "enter"); err != nil {
		return Result{}, fmt.Errorf("confirm search: %w", err)
	}
	time.Sleep(900 * time.Millisecond)
	if err := ccShortcut(ctx, invoke, session, approved, "enter"); err != nil {
		return Result{}, fmt.Errorf("select first result: %w", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := winexec.SendMediaKey("play"); err != nil {
		return Result{}, err
	}
	label := app
	if label == "" {
		label = "foreground app"
	}
	return result("searched " + q + " in " + label + " and sent play"), nil
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
