package ccapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// filterInput validates the per-tool argument shapes (layer 2).
func (s *Service) filterInput(tool string, args json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	switch tool {
	case ToolMouseMove:
		var a struct {
			X       int    `json:"x"`
			Y       int    `json:"y"`
			FrameID string `json:"frameId"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.X < 0 || a.Y < 0 || a.X > 65535 || a.Y > 65535 {
			return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
		}
		if err := s.rejectOutOfBounds(a.X, a.Y); err != nil {
			return nil, err
		}
		if err := s.requireFrameID(a.FrameID); err != nil {
			return nil, err
		}
		return nil, nil
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
			FrameID    string   `json:"frameId"`
			Modifiers  []string `json:"modifiers"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Scroll < -12 || a.Scroll > 12 {
			return nil, fmt.Errorf("%w: scroll", ErrCcInputFiltered)
		}
		if a.ScrollAxis != "" && a.ScrollAxis != "vertical" && a.ScrollAxis != "horizontal" {
			return nil, fmt.Errorf("%w: scrollAxis", ErrCcInputFiltered)
		}
		if a.Button == "" {
			a.Button = "left"
		}
		if a.Scroll == 0 && a.Button != "left" && a.Button != "right" && a.Button != "middle" {
			return nil, fmt.Errorf("%w: button %q", ErrCcInputFiltered, a.Button)
		}
		if a.Clicks < 1 {
			a.Clicks = 1
		}
		if a.Clicks > 3 {
			return nil, fmt.Errorf("%w: clicks", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Name) > 80 {
			return nil, fmt.Errorf("%w: name", ErrCcInputFiltered)
		}
		if a.ID != "" && (len(a.ID) > 8 || !validNodeID(a.ID)) {
			return nil, fmt.Errorf("%w: id", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.ID) > 8 {
			return nil, fmt.Errorf("%w: id", ErrCcInputFiltered)
		}
		if (a.X == nil) != (a.Y == nil) {
			return nil, fmt.Errorf("%w: x and y must be paired", ErrCcInputFiltered)
		}
		if a.X != nil {
			if *a.X < 0 || *a.Y < 0 || *a.X > 65535 || *a.Y > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
			if err := s.rejectOutOfBounds(*a.X, *a.Y); err != nil {
				return nil, err
			}
		}
		needsFrame := a.X != nil || strings.TrimSpace(a.ID) != "" || strings.TrimSpace(a.Name) != ""
		if !needsFrame && s.CurrentFrameID() != "" {
			needsFrame = true
		}
		if needsFrame {
			if err := s.requireFrameID(a.FrameID); err != nil {
				return nil, err
			}
		}
		if _, err := normalizeModifiers(a.Modifiers); err != nil {
			return nil, err
		}
		return nil, nil
	case ToolMouseDrag:
		var a struct {
			X1      int    `json:"x1"`
			Y1      int    `json:"y1"`
			X2      int    `json:"x2"`
			Y2      int    `json:"y2"`
			FrameID string `json:"frameId"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		for _, n := range []int{a.X1, a.Y1, a.X2, a.Y2} {
			if n < 0 || n > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
		}
		if err := s.rejectOutOfBounds(a.X1, a.Y1); err != nil {
			return nil, err
		}
		if err := s.rejectOutOfBounds(a.X2, a.Y2); err != nil {
			return nil, err
		}
		if err := s.requireFrameID(a.FrameID); err != nil {
			return nil, err
		}
		return nil, nil
	case ToolKeyboardType:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if !utf8.ValidString(a.Text) {
			return nil, fmt.Errorf("%w: text encoding", ErrCcInputFiltered)
		}
		runes := []rune(a.Text)
		if len(runes) < 1 || len(runes) > CcMaxTextRunes {
			return nil, fmt.Errorf("%w: text length", ErrCcInputFiltered)
		}
		for _, r := range runes {
			if r == '\t' || r == '\n' || r == '\r' {
				continue
			}
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("%w: control character", ErrCcInputFiltered)
			}
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolKeyboardShortcut:
		var a struct {
			Keys   []string `json:"keys"`
			Window string   `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return normalizeShortcut(a.Keys)
	case ToolScreenCapture:
		var a struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Target != "" && a.Target != "desktop" && a.Target != "foreground" && a.Target != "window" {
			return nil, fmt.Errorf("%w: target", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Title) > 200 {
			return nil, fmt.Errorf("%w: title", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolGetActiveWindow, ToolWindowList:
		var a struct{}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return nil, nil
	case ToolWindowFocus:
		var a struct {
			Title   string `json:"title"`
			Process string `json:"process"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		q := windowFocusQuery(a.Title, a.Process)
		if q == "" || utf8.RuneCountInString(q) > 200 {
			return nil, fmt.Errorf("%w: title or process", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolObserveDialog:
		var a struct {
			WaitMs int `json:"waitMs"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.WaitMs < 0 || a.WaitMs > 5000 {
			return nil, fmt.Errorf("%w: waitMs", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolConfirmDialog:
		var a struct {
			Button string `json:"button"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if len([]rune(a.Button)) > 32 {
			return nil, fmt.Errorf("%w: button", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolObserveUI:
		var a struct {
			MaxNodes int `json:"maxNodes"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.MaxNodes < 0 || a.MaxNodes > CcMaxObserveUINodes {
			return nil, fmt.Errorf("%w: maxNodes", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolWait:
		var a struct {
			Ms    *int   `json:"ms"`
			Until string `json:"until"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Ms != nil && (*a.Ms < 0 || *a.Ms > 8000) {
			return nil, fmt.Errorf("%w: ms", ErrCcInputFiltered)
		}
		if a.Until != "" && a.Until != "timeout" && a.Until != "change" {
			return nil, fmt.Errorf("%w: until", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolClipboard:
		var a struct {
			Op   string `json:"op"`
			Text string `json:"text"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Op != "get" && a.Op != "set" {
			return nil, fmt.Errorf("%w: op", ErrCcInputFiltered)
		}
		if a.Op == "set" {
			if !utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) < 1 || utf8.RuneCountInString(a.Text) > CcMaxClipboardRunes {
				return nil, fmt.Errorf("%w: text", ErrCcInputFiltered)
			}
		}
		return nil, nil
	case ToolWindowAction:
		var a struct {
			Title string `json:"title"`
			Op    string `json:"op"`
			X     int    `json:"x"`
			Y     int    `json:"y"`
			W     int    `json:"w"`
			H     int    `json:"h"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if utf8.RuneCountInString(a.Title) > 200 {
			return nil, fmt.Errorf("%w: title", ErrCcInputFiltered)
		}
		op := strings.ToLower(strings.TrimSpace(a.Op))
		switch op {
		case "close", "minimize", "maximize", "restore", "hide":
		case "move":
			if a.X < -32768 || a.Y < -32768 || a.X > 65535 || a.Y > 65535 {
				return nil, fmt.Errorf("%w: coordinates out of range", ErrCcInputFiltered)
			}
		case "resize":
			if a.W < 1 || a.H < 1 || a.W > 65535 || a.H > 65535 {
				return nil, fmt.Errorf("%w: size", ErrCcInputFiltered)
			}
		default:
			return nil, fmt.Errorf("%w: op", ErrCcInputFiltered)
		}
		return []string{op}, nil
	case ToolAppList:
		var a struct{}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		return nil, nil
	case ToolAppQuit:
		var a struct {
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if strings.TrimSpace(a.Title) == "" && strings.TrimSpace(a.Name) == "" {
			return nil, fmt.Errorf("%w: title or name required", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Title) > 200 || utf8.RuneCountInString(a.Name) > 200 {
			return nil, fmt.Errorf("%w: query", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolPaste:
		var a struct {
			Text   string `json:"text"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if a.Text != "" && (!utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) > 8192) {
			return nil, fmt.Errorf("%w: text", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolPress:
		var a struct {
			Key    string `json:"key"`
			Count  int    `json:"count"`
			Window string `json:"window"`
			Hold   string `json:"hold"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		key := strings.ToLower(strings.TrimSpace(a.Key))
		if key == "del" {
			key = "delete"
		}
		hold := strings.ToLower(strings.TrimSpace(a.Hold))
		if hold != "" && hold != "down" && hold != "up" {
			return nil, fmt.Errorf("%w: hold", ErrCcInputFiltered)
		}
		if hold != "" {
			if !keyVocabulary[key] {
				return nil, fmt.Errorf("%w: key", ErrCcInputFiltered)
			}
		} else if !keyVocabulary[key] || isHoldModifier(key) {
			return nil, fmt.Errorf("%w: key", ErrCcInputFiltered)
		}
		if a.Count < 0 || a.Count > 8 {
			return nil, fmt.Errorf("%w: count", ErrCcInputFiltered)
		}
		if hold != "" && a.Count > 1 {
			return nil, fmt.Errorf("%w: count", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolMenuClick:
		var a struct {
			Path   string `json:"path"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		segs := SplitMenuPath(a.Path)
		if len(segs) < 1 || len(segs) > 6 {
			return nil, fmt.Errorf("%w: path", ErrCcInputFiltered)
		}
		for _, seg := range segs {
			if utf8.RuneCountInString(seg) > 80 {
				return nil, fmt.Errorf("%w: path segment", ErrCcInputFiltered)
			}
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	case ToolSetValue:
		var a struct {
			Target string `json:"target"`
			Value  string `json:"value"`
			Window string `json:"window"`
		}
		if dec.Decode(&a) != nil {
			return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
		}
		if strings.TrimSpace(a.Target) == "" || utf8.RuneCountInString(a.Target) > 80 {
			return nil, fmt.Errorf("%w: target", ErrCcInputFiltered)
		}
		if !utf8.ValidString(a.Value) || utf8.RuneCountInString(a.Value) > 4096 {
			return nil, fmt.Errorf("%w: value", ErrCcInputFiltered)
		}
		if utf8.RuneCountInString(a.Window) > 200 {
			return nil, fmt.Errorf("%w: window", ErrCcInputFiltered)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("%w: tool", ErrCcSchema)
}
