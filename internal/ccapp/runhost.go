package ccapp

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func observeUIPayload(mapped []UINode, maxNodes int, frameID string) map[string]any {
	return map[string]any{
		"count":     len(mapped),
		"space":     "image",
		"nodes":     mapped,
		"frameId":   frameID,
		"truncated": maxNodes > 0 && len(mapped) >= maxNodes,
		"maxNodes":  maxNodes,
		"returned":  len(mapped),
	}
}

// runHost dispatches one validated call onto the OS host.
func (s *Service) runHost(tool string, args json.RawMessage, shortcut []string) (string, []byte, error) {
	switch tool {
	case ToolMouseMove:
		var a struct {
			X int `json:"x"`
			Y int `json:"y"`
		}
		_ = json.Unmarshal(args, &a)
		sx, sy := s.toScreen(a.X, a.Y)
		if err := s.host.MouseMove(sx, sy); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("moved cursor to screen (%d,%d)", sx, sy), nil, nil
	case ToolMouseClick:
		var a struct {
			Button     string   `json:"button"`
			Clicks     int      `json:"clicks"`
			X          *int     `json:"x"`
			Y          *int     `json:"y"`
			Scroll     int      `json:"scroll"`
			ScrollAxis string   `json:"scrollAxis"`
			Name       string   `json:"name"`
			ID         string   `json:"id"`
			Modifiers  []string `json:"modifiers"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		mods, _ := normalizeModifiers(a.Modifiers)
		click := func() error { return s.host.MouseClick(a.Button, a.Clicks) }
		if name := strings.TrimSpace(a.Name); name != "" || strings.TrimSpace(a.ID) != "" {
			query := strings.TrimSpace(a.ID)
			if query == "" {
				query = name
			}
			invokeName, sx, sy, hit, err := s.resolveNamedTarget(query)
			if err != nil {
				return "", nil, err
			}
			if err := s.clickNamedLadder(invokeName, sx, sy, hit); err != nil {
				return "", nil, err
			}
			return s.verifyAfter(fmt.Sprintf("invoked %q via accessibility", hit))
		}
		if err := s.refuseSelfWindowPixels(); err != nil {
			return "", nil, err
		}
		if a.X != nil && a.Y != nil {
			if err := s.requireObserveBeforeXY(); err != nil {
				return "", nil, err
			}
			sx, sy := s.toScreen(*a.X, *a.Y)
			if err := s.host.MouseMove(sx, sy); err != nil {
				return "", nil, err
			}
			time.Sleep(25 * time.Millisecond)
		}
		if a.Scroll != 0 {
			var err error
			if a.ScrollAxis == "horizontal" {
				err = s.host.MouseScrollH(a.Scroll)
			} else {
				err = s.host.MouseScroll(a.Scroll)
			}
			if err != nil {
				return "", nil, err
			}
			axis := a.ScrollAxis
			if axis == "" {
				axis = "vertical"
			}
			return s.verifyAfter(fmt.Sprintf("scrolled %s %d notch(es)", axis, a.Scroll))
		}
		if err := s.withModifiers(mods, click); err != nil {
			return "", nil, err
		}
		if a.X != nil && a.Y != nil {
			if err := s.verifyPixelClick(s.toScreen(*a.X, *a.Y)); err != nil {
				return "", nil, err
			}
		}
		return s.verifyAfter(fmt.Sprintf("clicked %s mouse %d time(s)", a.Button, a.Clicks))
	case ToolMouseDrag:
		var a struct {
			X1 int `json:"x1"`
			Y1 int `json:"y1"`
			X2 int `json:"x2"`
			Y2 int `json:"y2"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.refuseSelfWindowPixels(); err != nil {
			return "", nil, err
		}
		if err := s.requireObserveBeforeXY(); err != nil {
			return "", nil, err
		}
		sx1, sy1 := s.toScreen(a.X1, a.Y1)
		sx2, sy2 := s.toScreen(a.X2, a.Y2)
		if err := s.host.MouseDrag(sx1, sy1, sx2, sy2); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("dragged from (%d,%d) to (%d,%d)", sx1, sy1, sx2, sy2))
	case ToolKeyboardType:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		time.Sleep(40 * time.Millisecond)
		if PreferPasteText(a.Text) {
			raw, _ := json.Marshal(map[string]any{"text": a.Text, "window": a.Window})
			return s.runHost(ToolPaste, raw, nil)
		}
		if err := s.host.KeyboardType(a.Text); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("typed %d character(s)", len([]rune(a.Text))))
	case ToolKeyboardShortcut:
		var a struct {
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		time.Sleep(40 * time.Millisecond)
		if err := s.host.KeyboardShortcut(shortcut); err != nil {
			return "", nil, err
		}
		return s.verifyAfter("pressed " + strings.Join(shortcut, "+"))
	case ToolScreenCapture:
		var a struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal(args, &a)
		target := strings.TrimSpace(a.Target)
		if target == "" {
			target = "desktop"
		}
		if target == "desktop" && strings.TrimSpace(a.Title) == "" {
			png, err := s.captureDesktop()
			if err != nil {
				return "", nil, err
			}
			return s.captureSummary(png, "desktop"), png, nil
		}
		query := strings.TrimSpace(a.Title)
		if target == "foreground" {
			query = "foreground"
			if title, process, err := s.host.ActiveWindow(); err == nil && (isCompanionProcess(process) || companionWindowTitle(title)) {
				if s.restoreNonCompanionForeground() == nil {
					png, ox, oy, capErr := s.host.WindowCapture("foreground")
					if capErr == nil {
						s.rememberCapture(png, ox, oy, false)
						return s.captureSummary(png, "foreground window"), png, nil
					}
				}
				png, deskErr := s.captureDesktop()
				if deskErr != nil {
					return "", nil, deskErr
				}
				return s.captureSummary(png, "desktop"), png, nil
			}
		}
		png, ox, oy, err := s.host.WindowCapture(query)
		if err != nil {
			return "", nil, err
		}
		s.rememberCapture(png, ox, oy, false)
		kind := "window"
		if target == "foreground" {
			kind = "foreground window"
		}
		return s.captureSummary(png, kind), png, nil
	case ToolGetActiveWindow:
		title, process, err := s.host.ActiveWindow()
		if err != nil {
			return "", nil, err
		}
		s.noteForeground(title, process)
		cx, cy, _ := s.host.CursorPosition()
		s.capMu.Lock()
		ox, oy, vw, vh, dw, dh := s.capOriginX, s.capOriginY, s.capVisW, s.capVisH, s.capDeskW, s.capDeskH
		hasCap := dw > 0 || vw > 0
		s.capMu.Unlock()
		if hasCap {
			ix, iy := MapScreenToVision(cx, cy, ox, oy, vw, vh, dw, dh)
			return appendFrameID(fmt.Sprintf("active window: %s (process: %s); cursor screen (%d,%d) image (%d,%d)", title, process, cx, cy, ix, iy), s.CurrentFrameID()), nil, nil
		}
		return fmt.Sprintf("active window: %s (process: %s); cursor screen (%d,%d)", title, process, cx, cy), nil, nil
	case ToolWindowList:
		wins, err := s.host.ListWindows()
		if err != nil {
			return "", nil, err
		}
		if wins == nil {
			wins = []WindowInfo{}
		}
		mapped, space := s.mapWindows(wins)
		payload := map[string]any{"count": len(mapped), "space": space, "windows": mapped}
		if id := s.CurrentFrameID(); id != "" {
			payload["frameId"] = id
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolWindowFocus:
		var a struct {
			Title   string `json:"title"`
			Process string `json:"process"`
		}
		_ = json.Unmarshal(args, &a)
		info, err := s.host.FocusWindow(windowFocusQuery(a.Title, a.Process))
		if err != nil {
			return "", nil, err
		}
		s.noteForeground(info.Title, info.Process)
		return s.verifyAfter(fmt.Sprintf("focused window %q (process: %s, id: %s)", info.Title, info.Process, info.ID))
	case ToolObserveDialog:
		var a struct {
			WaitMs int `json:"waitMs"`
		}
		_ = json.Unmarshal(args, &a)
		if a.WaitMs > 0 {
			time.Sleep(time.Duration(a.WaitMs) * time.Millisecond)
		}
		snaps, err := s.host.ObserveDialogs()
		if err != nil {
			return "", nil, err
		}
		if snaps == nil {
			snaps = []DialogSnapshot{}
		}
		mapped := s.mapDialogs(snaps)
		_, _, _, _, _, _, space := s.captureSpace()
		payload := map[string]any{"count": len(mapped), "space": space, "dialogs": mapped}
		if id := s.CurrentFrameID(); id != "" {
			payload["frameId"] = id
		}
		for _, d := range mapped {
			if msg := FilePickerHandoff(d.Title, d.Process, d.Class, d.Buttons); msg != "" {
				payload["needs_user"] = msg
				payload["handoff"] = "file_dialog"
				break
			}
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolConfirmDialog:
		var a struct {
			Button string `json:"button"`
		}
		_ = json.Unmarshal(args, &a)
		snap, err := s.host.ConfirmDialog(strings.TrimSpace(a.Button))
		if err != nil {
			if errors.Is(err, ErrCcRiskBlocked) && strings.Contains(err.Error(), "file open/save dialog") {
				return "", nil, fmt.Errorf("%w: %s", err, FilePickerUserPrompt)
			}
			if errors.Is(err, ErrCcRiskBlocked) && (strings.Contains(err.Error(), "uac dialog") || strings.Contains(err.Error(), "elevation dialog")) {
				return "", nil, fmt.Errorf("%w: %s", err, UACUserPrompt)
			}
			return "", nil, err
		}
		caption := ConfirmButtonName(snap.Buttons, a.Button)
		if caption == "" {
			caption = "confirm"
		}
		return s.verifyAfter(formatDialogSummary("clicked "+caption+" on", snap))
	case ToolObserveUI:
		var a struct {
			MaxNodes int `json:"maxNodes"`
		}
		_ = json.Unmarshal(args, &a)
		if a.MaxNodes <= 0 {
			a.MaxNodes = CcDefaultObserveUINodes
		}
		png, err := s.captureDesktop()
		if err != nil {
			return "", nil, err
		}
		if reason := s.observeHideReason(nil); reason != "" {
			raw, err := json.Marshal(map[string]any{"count": 0, "nodes": []UINode{}, "refused": reason, "space": "image", "frameId": s.CurrentFrameID()})
			if err != nil {
				return "", nil, err
			}
			return string(raw), png, nil
		}
		nodes, err := s.host.ObserveUI(a.MaxNodes)
		if err != nil {
			return "", nil, err
		}
		if len(nodes) > a.MaxNodes {
			nodes = nodes[:a.MaxNodes]
		}
		if reason := s.observeHideReason(nodeNames(nodes)); reason != "" {
			raw, err := json.Marshal(map[string]any{"count": 0, "nodes": []UINode{}, "refused": reason, "space": "image", "frameId": s.CurrentFrameID()})
			if err != nil {
				return "", nil, err
			}
			return string(raw), png, nil
		}
		nodes = assignNodeIDs(nodes)
		s.rememberHits(nodes)
		mapped := s.mapUINodes(nodes)
		s.capMu.Lock()
		vw, vh := s.capVisW, s.capVisH
		s.capMu.Unlock()
		if annotated, aerr := AnnotateCapture(png, mapped, vw, vh); aerr == nil && len(annotated) > 0 {
			png = annotated
			ox, oy := s.host.ScreenOrigin()
			s.rememberCapture(png, ox, oy, true)
		}
		payload := observeUIPayload(mapped, a.MaxNodes, s.CurrentFrameID())
		if handoff := s.filePickerHandoff(nodeNames(mapped)); handoff != "" {
			payload["needs_user"] = handoff
			payload["handoff"] = "file_dialog"
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", nil, err
		}
		return string(raw), png, nil
	case ToolWait:
		var a struct {
			Ms    *int   `json:"ms"`
			Until string `json:"until"`
		}
		_ = json.Unmarshal(args, &a)
		ms := 400
		if a.Ms != nil {
			ms = *a.Ms
		}
		if a.Until == "change" {
			before, err := s.host.ScreenCapture()
			if err != nil {
				return "", nil, err
			}
			beforeHash := sha256.Sum256(before)
			deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
			for {
				remain := time.Until(deadline)
				if remain <= 0 {
					ox, oy := s.host.ScreenOrigin()
					s.rememberCapture(before, ox, oy, true)
					return appendFrameID(fmt.Sprintf("waited %dms; screen unchanged", ms), s.CurrentFrameID()), before, nil
				}
				slice := 200 * time.Millisecond
				if remain < slice {
					slice = remain
				}
				time.Sleep(slice)
				cur, err := s.host.ScreenCapture()
				if err != nil {
					continue
				}
				if sha256.Sum256(cur) != beforeHash {
					ox, oy := s.host.ScreenOrigin()
					s.rememberCapture(cur, ox, oy, true)
					return s.captureSummary(cur, "desktop after wait"), cur, nil
				}
			}
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return fmt.Sprintf("waited %dms", ms), nil, nil
	case ToolClipboard:
		var a struct {
			Op   string `json:"op"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(args, &a)
		if a.Op == "get" {
			text, err := s.host.ClipboardGet()
			if err != nil {
				return "", nil, err
			}
			text = clampClipboard(text)
			raw, err := json.Marshal(map[string]any{"text": text, "runes": utf8.RuneCountInString(text)})
			if err != nil {
				return "", nil, err
			}
			return string(raw), nil, nil
		}
		if err := s.host.ClipboardSet(a.Text); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("clipboard set (%d character(s))", utf8.RuneCountInString(a.Text)), nil, nil
	case ToolWindowAction:
		var a struct {
			Title string `json:"title"`
			Op    string `json:"op"`
			X     int    `json:"x"`
			Y     int    `json:"y"`
			W     int    `json:"w"`
			H     int    `json:"h"`
		}
		_ = json.Unmarshal(args, &a)
		query := strings.TrimSpace(a.Title)
		if query == "" {
			query = "foreground"
		}
		info, err := s.host.WindowAction(query, strings.ToLower(strings.TrimSpace(a.Op)), a.X, a.Y, a.W, a.H)
		if err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("window %s %q (process: %s, id: %s)", a.Op, info.Title, info.Process, info.ID))
	case ToolAppList:
		wins, err := s.host.ListWindows()
		if err != nil {
			return "", nil, err
		}
		type appRow struct {
			Name       string `json:"name"`
			Windows    int    `json:"windows"`
			Foreground bool   `json:"foreground,omitempty"`
		}
		seen := map[string]int{}
		order := make([]string, 0)
		fg := map[string]bool{}
		for _, w := range wins {
			name := strings.TrimSpace(w.Process)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; !ok {
				order = append(order, name)
			}
			seen[key]++
			if w.Foreground {
				fg[key] = true
			}
		}
		apps := make([]appRow, 0, len(order))
		for _, name := range order {
			key := strings.ToLower(name)
			apps = append(apps, appRow{Name: name, Windows: seen[key], Foreground: fg[key]})
		}
		raw, err := json.Marshal(map[string]any{"count": len(apps), "apps": apps})
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	case ToolAppQuit:
		var a struct {
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		_ = json.Unmarshal(args, &a)
		query := strings.TrimSpace(a.Title)
		if query == "" {
			query = strings.TrimSpace(a.Name)
		}
		closed, info, err := s.host.QuitApp(query)
		if err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("quit %d window(s) matching %q (sample: %s / %s)", closed, query, info.Title, info.Process))
	case ToolPaste:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(a.Text) != "" {
			if err := s.host.ClipboardSet(a.Text); err != nil {
				return "", nil, err
			}
		}
		if err := s.host.KeyboardShortcut([]string{"ctrl", "v"}); err != nil {
			return "", nil, err
		}
		n := utf8.RuneCountInString(a.Text)
		if n == 0 {
			return s.verifyAfter("pasted clipboard")
		}
		return s.verifyAfter(fmt.Sprintf("pasted %d character(s)", n))
	case ToolPress:
		var a struct {
			Key    string `json:"key"`
			Count  int    `json:"count"`
			Window string `json:"window"`
			Hold   string `json:"hold"`
		}
		_ = json.Unmarshal(args, &a)
		key := strings.ToLower(strings.TrimSpace(a.Key))
		if key == "del" {
			key = "delete"
		}
		if a.Count < 1 {
			a.Count = 1
		}
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		hold := strings.ToLower(strings.TrimSpace(a.Hold))
		if hold == "down" || hold == "up" {
			if err := s.host.HoldKey(key, hold == "down"); err != nil {
				return "", nil, err
			}
			if hold == "down" {
				s.noteHeld(key)
				return s.verifyAfter(fmt.Sprintf("holding %s", key))
			}
			s.clearHeld(key)
			return s.verifyAfter(fmt.Sprintf("released %s", key))
		}
		for i := 0; i < a.Count; i++ {
			if err := s.host.KeyboardShortcut([]string{key}); err != nil {
				return "", nil, err
			}
			if i+1 < a.Count {
				time.Sleep(30 * time.Millisecond)
			}
		}
		return s.verifyAfter(fmt.Sprintf("pressed %s x%d", key, a.Count))
	case ToolMenuClick:
		var a struct {
			Path   string `json:"path"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		if err := s.host.MenuClick(strings.TrimSpace(a.Path)); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("clicked menu %q", strings.TrimSpace(a.Path)))
	case ToolSetValue:
		var a struct {
			Target string `json:"target"`
			Value  string `json:"value"`
			Window string `json:"window"`
		}
		_ = json.Unmarshal(args, &a)
		if err := s.focusIfNamed(a.Window); err != nil {
			return "", nil, err
		}
		target := strings.TrimSpace(a.Target)
		if hit, ok := s.lookupHit(target); ok && hit.Name != "" {
			target = hit.Name
		}
		if err := s.host.SetValue(target, a.Value); err != nil {
			return "", nil, err
		}
		return s.verifyAfter(fmt.Sprintf("set value on %q (%d character(s))", target, utf8.RuneCountInString(a.Value)))
	}
	return "", nil, fmt.Errorf("unknown tool %q", tool)
}
