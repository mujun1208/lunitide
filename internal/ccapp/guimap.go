package ccapp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// GuiNativeAction is one parsed gui-plus computer_use tool call. The native
// protocol declares a 1000x1000 screen, so Coordinate values normally land in
// 0-1000 per-mille; values above 1000 are treated as absolute pixels of the
// frame that was sent (the official gui-plus example emits [2530, 314] with
// high-resolution images).
type GuiNativeAction struct {
	Action     string
	Keys       []string
	Text       string
	Coordinate []float64
	Pixels     *float64
	Time       *float64
	Status     string
}

// GuiNativePoint is a pixel position inside the captured frame.
type GuiNativePoint struct{ X, Y int }

// GuiNativeSignal classifies orchestration-only actions. A non-empty return
// means the caller must handle the signal instead of executing: "done",
// "fail", or "ask" (a blocking window needs the user).
func GuiNativeSignal(a GuiNativeAction) string {
	switch a.Action {
	case "terminate":
		if strings.EqualFold(a.Status, "failure") {
			return "fail"
		}
		return "done"
	case "answer":
		return "done"
	case "interact":
		return "ask"
	}
	return ""
}

var guiNativeActionEnum = map[string]bool{
	"key": true, "type": true, "mouse_move": true, "left_click": true,
	"left_click_drag": true, "right_click": true, "middle_click": true,
	"double_click": true, "triple_click": true, "scroll": true,
	"hscroll": true, "wait": true, "terminate": true, "answer": true,
	"interact": true,
}

type guiNativeArgs struct {
	Action     string          `json:"action"`
	Keys       json.RawMessage `json:"keys"`
	Text       string          `json:"text"`
	Coordinate []float64       `json:"coordinate"`
	Pixels     *float64        `json:"pixels"`
	Time       *float64        `json:"time"`
	Status     string          `json:"status"`
}

// ParseGuiNativeToolCall extracts one gui-plus tool call from a model reply.
// Accepted shapes, in order: a <tool_call>{"name":"computer_use","arguments":
// {...}}</tool_call> wrapper (with or without surrounding prose), or a bare
// arguments object {"action":...}. Fail-closes on anything else so the loop
// counts the turn as an invalid step instead of guessing.
func ParseGuiNativeToolCall(raw string) (GuiNativeAction, error) {
	body := raw
	if i := strings.Index(raw, "<tool_call>"); i >= 0 {
		rest := raw[i+len("<tool_call>"):]
		j := strings.Index(rest, "</tool_call>")
		if j < 0 {
			return GuiNativeAction{}, fmt.Errorf("unterminated tool_call")
		}
		body = rest[:j]
	}
	js, err := firstCcJSONObject(body)
	if err != nil {
		return GuiNativeAction{}, err
	}
	var envelope struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	argsRaw := json.RawMessage(js)
	if json.Unmarshal([]byte(js), &envelope) == nil && len(strings.TrimSpace(string(envelope.Arguments))) > 0 {
		if envelope.Name != "" && envelope.Name != "computer_use" {
			return GuiNativeAction{}, fmt.Errorf("unexpected tool %q", envelope.Name)
		}
		argsRaw = envelope.Arguments
	}
	var a guiNativeArgs
	if json.Unmarshal(argsRaw, &a) != nil {
		return GuiNativeAction{}, fmt.Errorf("invalid tool_call arguments")
	}
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	if !guiNativeActionEnum[a.Action] {
		return GuiNativeAction{}, fmt.Errorf("unknown native action %q", a.Action)
	}
	keys, err := parseGuiNativeKeys(a.Keys)
	if err != nil {
		return GuiNativeAction{}, err
	}
	out := GuiNativeAction{
		Action:     a.Action,
		Keys:       keys,
		Text:       a.Text,
		Coordinate: a.Coordinate,
		Pixels:     a.Pixels,
		Time:       a.Time,
		Status:     strings.ToLower(strings.TrimSpace(a.Status)),
	}
	if err := validateGuiNativeAction(out); err != nil {
		return GuiNativeAction{}, err
	}
	return out, nil
}

// parseGuiNativeKeys accepts the schema's array form and the single-string
// form models drift into ("Return" instead of ["Return"]).
func parseGuiNativeKeys(raw json.RawMessage) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		out := make([]string, 0, len(arr))
		for _, k := range arr {
			if k = strings.TrimSpace(k); k != "" {
				out = append(out, k)
			}
		}
		return out, nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if one = strings.TrimSpace(one); one != "" {
			return []string{one}, nil
		}
	}
	return nil, fmt.Errorf("keys must be strings")
}

func validateGuiNativeAction(a GuiNativeAction) error {
	switch a.Action {
	case "key":
		if len(a.Keys) == 0 {
			return fmt.Errorf("key needs keys")
		}
	case "type", "answer", "interact":
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("%s needs text", a.Action)
		}
	case "mouse_move", "left_click", "left_click_drag", "right_click", "middle_click", "double_click", "triple_click":
		if err := validateGuiNativeCoordinate(a.Coordinate); err != nil {
			return err
		}
	case "terminate":
		if a.Status != "success" && a.Status != "failure" {
			return fmt.Errorf("terminate needs status success|failure")
		}
	}
	return nil
}

func validateGuiNativeCoordinate(c []float64) error {
	if len(c) != 2 {
		return fmt.Errorf("coordinate needs [x,y]")
	}
	if c[0] < 0 || c[1] < 0 {
		return fmt.Errorf("negative coordinate")
	}
	return nil
}

// firstCcJSONObject scans out the first balanced JSON object, skipping string
// contents, so prose around the tool call cannot confuse it.
func firstCcJSONObject(raw string) (string, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", fmt.Errorf("no json object")
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced json object")
}

// MapGuiNativeAction converts a native gui-plus action into a computer.act
// payload. frameW/frameH are the pixel dimensions of the screenshot the model
// saw; cursor is the last known mouse position, required only by
// left_click_drag. Signal actions (terminate/answer/interact) are rejected —
// the caller handles them via GuiNativeSignal.
// frameID is the current screenshot echo; pixel actions without it
// are rejected by the input filter (M10-CC-008 COMPUTER_STALE_FRAME), so the
// gui loop must always pass its live frameID. An empty frameID is accepted for
// callers that only render descriptions.
func MapGuiNativeAction(a GuiNativeAction, frameW, frameH int, cursor *GuiNativePoint, frameID string) (json.RawMessage, error) {
	if GuiNativeSignal(a) != "" {
		return nil, fmt.Errorf("native action %s is an orchestration signal", a.Action)
	}
	withFrame := func(m map[string]any) map[string]any {
		if id := strings.TrimSpace(frameID); id != "" {
			m["frameId"] = id
		}
		return m
	}
	switch a.Action {
	case "key":
		return json.Marshal(map[string]any{"action": "key", "keys": a.Keys})
	case "type":
		action := "type"
		if PreferPasteText(a.Text) {
			action = "paste"
		}
		return json.Marshal(map[string]any{"action": action, "text": a.Text})
	case "mouse_move", "left_click", "right_click", "middle_click", "double_click", "triple_click":
		x, y, err := guiNativeXY(a.Coordinate, frameW, frameH)
		if err != nil {
			return nil, err
		}
		action := a.Action
		switch action {
		case "mouse_move":
			action = "move"
		case "left_click":
			action = "click"
		case "triple_click":
			// The native schema itself says triple-click is "simulated as
			// double-click since it's the closest action".
			action = "double_click"
		}
		return json.Marshal(withFrame(map[string]any{"action": action, "x": x, "y": y}))
	case "left_click_drag":
		x2, y2, err := guiNativeXY(a.Coordinate, frameW, frameH)
		if err != nil {
			return nil, err
		}
		if cursor == nil {
			return nil, fmt.Errorf("drag needs a known cursor: move the mouse first")
		}
		return json.Marshal(withFrame(map[string]any{"action": "drag", "x1": cursor.X, "y1": cursor.Y, "x2": x2, "y2": y2}))
	case "scroll", "hscroll":
		m := map[string]any{"action": "scroll", "scroll": guiNativeScrollNotches(a.Pixels)}
		if a.Action == "hscroll" {
			m["scrollAxis"] = "horizontal"
		}
		return json.Marshal(m)
	case "wait":
		return json.Marshal(map[string]any{"action": "wait", "ms": GuiNativeWaitMs(a.Time)})
	}
	return nil, fmt.Errorf("unmapped native action %q", a.Action)
}

// guiNativeXY converts a native coordinate pair into frame pixels. The pair
// is interpreted as a whole: when either value exceeds 1000 (the official
// gui-plus example emits [2530, 314] with high-resolution images) both are
// absolute frame pixels; otherwise both are per-mille of the declared
// 1000x1000 screen. Both clamp inside the frame so a bad coordinate can
// never click off-screen.
func guiNativeXY(c []float64, frameW, frameH int) (int, int, error) {
	if err := validateGuiNativeCoordinate(c); err != nil {
		return 0, 0, err
	}
	if frameW <= 0 || frameH <= 0 {
		return 0, 0, fmt.Errorf("missing vision size")
	}
	var x, y int
	if c[0] > 1000 || c[1] > 1000 {
		x, y = int(math.Round(c[0])), int(math.Round(c[1]))
	} else {
		x = int(math.Round(c[0] / 1000 * float64(frameW)))
		y = int(math.Round(c[1] / 1000 * float64(frameH)))
	}
	if x > frameW-1 {
		x = frameW - 1
	}
	if y > frameH-1 {
		y = frameH - 1
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y, nil
}

// guiNativeScrollNotches converts native scroll pixels into wheel notches
// counts. gui-plus emits amounts like ±100..±1000; one notch ≈ 100 pixels of
// travel, clamped to the host's ±12 limit. A nonzero amount that rounds to
// zero still scrolls one notch so the intent is never lost.
func guiNativeScrollNotches(pixels *float64) int {
	if pixels == nil {
		return -3
	}
	notches := int(math.Round(*pixels / 100))
	if notches == 0 && *pixels != 0 {
		if *pixels < 0 {
			notches = -1
		} else {
			notches = 1
		}
	}
	if notches > 12 {
		notches = 12
	}
	if notches < -12 {
		notches = -12
	}
	return notches
}

// GuiNativeWaitMs converts wait seconds into the clamped millisecond budget
// the computer.act wait step accepts.
func GuiNativeWaitMs(seconds *float64) int {
	if seconds == nil || *seconds <= 0 {
		return 800
	}
	ms := int(math.Round(*seconds * 1000))
	if ms < 100 {
		ms = 100
	}
	if ms > 5000 {
		ms = 5000
	}
	return ms
}
