package ccapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ToolComputerAct is the OpenClaw-shaped unified desktop action. It expands
// onto the existing cc.* host path so audit, rate limits, and risk gates stay
// on one pipeline. The model tool list exposes computer.act only.
const ToolComputerAct = "computer.act"

type computerActArgs struct {
	Action     string   `json:"action"`
	FrameID    string   `json:"frameId"`
	X          *int     `json:"x"`
	Y          *int     `json:"y"`
	X1         *int     `json:"x1"`
	Y1         *int     `json:"y1"`
	X2         *int     `json:"x2"`
	Y2         *int     `json:"y2"`
	Button     string   `json:"button"`
	Clicks     int      `json:"clicks"`
	Modifiers  []string `json:"modifiers"`
	Scroll     int      `json:"scroll"`
	ScrollAxis string   `json:"scrollAxis"`
	Name       string   `json:"name"`
	ID         string   `json:"id"`
	ID2        string   `json:"id2"`
	Text       string   `json:"text"`
	Keys       []string `json:"keys"`
	Key        string   `json:"key"`
	Count      int      `json:"count"`
	Window     string   `json:"window"`
	Title      string   `json:"title"`
	Process    string   `json:"process"`
	Target     string   `json:"target"`
	Ms         *int     `json:"ms"`
	Until      string   `json:"until"`
	MaxNodes   int      `json:"maxNodes"`
	Path       string   `json:"path"`
	Op         string   `json:"op"`
	Value      string   `json:"value"`
	W          int      `json:"w"`
	H          int      `json:"h"`
}

func compactJSON(v map[string]any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: arguments", ErrCcSchema)
	}
	return raw, nil
}

func putFrame(m map[string]any, frameID string) {
	if strings.TrimSpace(frameID) != "" {
		m["frameId"] = strings.TrimSpace(frameID)
	}
}

// MapComputerAct expands one computer.act payload onto a governed cc.* tool.
func MapComputerAct(args json.RawMessage) (string, json.RawMessage, error) {
	var a computerActArgs
	if json.Unmarshal(args, &a) != nil {
		return "", nil, fmt.Errorf("%w: arguments", ErrCcSchema)
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	if action == "" {
		return "", nil, fmt.Errorf("%w: action required", ErrCcInputFiltered)
	}
	if utf8.RuneCountInString(a.Action) > 40 {
		return "", nil, fmt.Errorf("%w: action", ErrCcInputFiltered)
	}

	switch action {
	case "screenshot", "screen_capture", "capture":
		target := strings.TrimSpace(a.Target)
		if target == "" {
			target = "foreground"
		}
		m := map[string]any{"target": target}
		if a.Title != "" {
			m["title"] = a.Title
		}
		raw, err := compactJSON(m)
		return ToolScreenCapture, raw, err

	case "click", "left_click", "leftclick":
		return mapClick(a, "left", 1)
	case "double_click", "dblclick", "doubleclick":
		return mapClick(a, "left", 2)
	case "right_click", "rightclick":
		return mapClick(a, "right", 1)
	case "middle_click", "middleclick":
		return mapClick(a, "middle", 1)

	case "move", "hover", "mouse_move":
		if a.X == nil || a.Y == nil {
			return "", nil, fmt.Errorf("%w: x and y required", ErrCcInputFiltered)
		}
		m := map[string]any{"x": *a.X, "y": *a.Y}
		putFrame(m, a.FrameID)
		raw, err := compactJSON(m)
		return ToolMouseMove, raw, err

	case "drag", "mouse_drag":
		if strings.TrimSpace(a.ID) == "" && (a.X1 == nil || a.Y1 == nil) {
			return "", nil, fmt.Errorf("%w: drag needs id or x1,y1", ErrCcInputFiltered)
		}
		if strings.TrimSpace(a.ID2) == "" && (a.X2 == nil || a.Y2 == nil) {
			return "", nil, fmt.Errorf("%w: drag needs id2 or x2,y2", ErrCcInputFiltered)
		}
		m := map[string]any{}
		if a.X1 != nil {
			m["x1"] = *a.X1
		}
		if a.Y1 != nil {
			m["y1"] = *a.Y1
		}
		if a.X2 != nil {
			m["x2"] = *a.X2
		}
		if a.Y2 != nil {
			m["y2"] = *a.Y2
		}
		if strings.TrimSpace(a.ID) != "" {
			m["id"] = strings.TrimSpace(a.ID)
		}
		if strings.TrimSpace(a.ID2) != "" {
			m["id2"] = strings.TrimSpace(a.ID2)
		}
		putFrame(m, a.FrameID)
		raw, err := compactJSON(m)
		return ToolMouseDrag, raw, err

	case "scroll":
		m := map[string]any{"scroll": a.Scroll}
		if a.ScrollAxis != "" {
			m["scrollAxis"] = a.ScrollAxis
		}
		if a.X != nil {
			m["x"] = *a.X
		}
		if a.Y != nil {
			m["y"] = *a.Y
		}
		putFrame(m, a.FrameID)
		raw, err := compactJSON(m)
		return ToolMouseClick, raw, err

	case "type", "keyboard_type":
		m := map[string]any{"text": a.Text}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolKeyboardType, raw, err

	case "key", "hotkey", "shortcut", "keyboard_shortcut":
		if len(a.Keys) > 0 {
			m := map[string]any{"keys": a.Keys}
			if a.Window != "" {
				m["window"] = a.Window
			}
			raw, err := compactJSON(m)
			return ToolKeyboardShortcut, raw, err
		}
		key := strings.TrimSpace(a.Key)
		if key == "" {
			return "", nil, fmt.Errorf("%w: keys or key required", ErrCcInputFiltered)
		}
		m := map[string]any{"key": key}
		if a.Count > 0 {
			m["count"] = a.Count
		}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolPress, raw, err

	case "press":
		m := map[string]any{"key": a.Key}
		if a.Count > 0 {
			m["count"] = a.Count
		}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolPress, raw, err

	case "hold_key", "key_down":
		return mapHold(a, "down")
	case "key_up":
		return mapHold(a, "up")

	case "wait":
		m := map[string]any{}
		if a.Ms != nil {
			m["ms"] = *a.Ms
		}
		if a.Until != "" {
			m["until"] = a.Until
		}
		raw, err := compactJSON(m)
		return ToolWait, raw, err

	case "observe", "observe_ui":
		m := map[string]any{}
		if a.MaxNodes > 0 {
			m["maxNodes"] = a.MaxNodes
		}
		raw, err := compactJSON(m)
		return ToolObserveUI, raw, err

	case "observe_dialog":
		m := map[string]any{}
		if a.Ms != nil {
			m["waitMs"] = *a.Ms
		}
		raw, err := compactJSON(m)
		return ToolObserveDialog, raw, err

	case "confirm", "confirm_dialog":
		m := map[string]any{}
		if a.Button != "" {
			m["button"] = a.Button
		}
		raw, err := compactJSON(m)
		return ToolConfirmDialog, raw, err

	case "focus", "window_focus":
		m := map[string]any{}
		if a.Title != "" {
			m["title"] = a.Title
		} else if a.Window != "" {
			m["title"] = a.Window
		}
		if a.Process != "" {
			m["process"] = a.Process
		}
		raw, err := compactJSON(m)
		return ToolWindowFocus, raw, err

	case "list", "window_list":
		raw, err := compactJSON(map[string]any{})
		return ToolWindowList, raw, err

	case "active", "get_active_window":
		raw, err := compactJSON(map[string]any{})
		return ToolGetActiveWindow, raw, err

	case "paste":
		m := map[string]any{}
		if a.Text != "" {
			m["text"] = a.Text
		}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolPaste, raw, err

	case "menu", "menu_click":
		m := map[string]any{"path": a.Path}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolMenuClick, raw, err

	case "set_value", "setvalue":
		m := map[string]any{"target": a.Target, "value": a.Value}
		if a.Window != "" {
			m["window"] = a.Window
		}
		raw, err := compactJSON(m)
		return ToolSetValue, raw, err

	case "clipboard":
		m := map[string]any{"op": a.Op}
		if a.Text != "" {
			m["text"] = a.Text
		}
		raw, err := compactJSON(m)
		return ToolClipboard, raw, err

	case "window_action":
		m := map[string]any{"op": a.Op}
		if a.Title != "" {
			m["title"] = a.Title
		} else if a.Window != "" {
			m["title"] = a.Window
		}
		if a.X != nil {
			m["x"] = *a.X
		}
		if a.Y != nil {
			m["y"] = *a.Y
		}
		if a.W > 0 {
			m["w"] = a.W
		}
		if a.H > 0 {
			m["h"] = a.H
		}
		raw, err := compactJSON(m)
		return ToolWindowAction, raw, err

	default:
		return "", nil, fmt.Errorf("%w: unknown action %q", ErrCcInputFiltered, a.Action)
	}
}

func mapClick(a computerActArgs, button string, clicks int) (string, json.RawMessage, error) {
	if a.Button != "" {
		button = a.Button
	}
	if a.Clicks > 0 {
		clicks = a.Clicks
	}
	m := map[string]any{"button": button, "clicks": clicks}
	if a.X != nil {
		m["x"] = *a.X
	}
	if a.Y != nil {
		m["y"] = *a.Y
	}
	if a.Name != "" {
		m["name"] = a.Name
	}
	if a.ID != "" {
		m["id"] = a.ID
	}
	if len(a.Modifiers) > 0 {
		m["modifiers"] = a.Modifiers
	}
	putFrame(m, a.FrameID)
	raw, err := compactJSON(m)
	return ToolMouseClick, raw, err
}

func mapHold(a computerActArgs, hold string) (string, json.RawMessage, error) {
	key := strings.TrimSpace(a.Key)
	if key == "" {
		return "", nil, fmt.Errorf("%w: key required", ErrCcInputFiltered)
	}
	m := map[string]any{"key": key, "hold": hold}
	if a.Window != "" {
		m["window"] = a.Window
	}
	raw, err := compactJSON(m)
	return ToolPress, raw, err
}
