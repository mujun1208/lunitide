package ccapp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseGuiNativeToolCallWrappers(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want GuiNativeAction
	}{
		{
			name: "full tool_call wrapper",
			raw:  "Action: 点击开始按钮\n<tool_call>\n{\"name\": \"computer_use\", \"arguments\": {\"action\": \"left_click\", \"coordinate\": [2530, 314]}}\n</tool_call>",
			want: GuiNativeAction{Action: "left_click", Coordinate: []float64{2530, 314}},
		},
		{
			name: "bare arguments json",
			raw:  `{"action":"type","text":"你好世界"}`,
			want: GuiNativeAction{Action: "type", Text: "你好世界"},
		},
		{
			name: "fenced tool_call",
			raw:  "```json\n<tool_call>{\"name\":\"computer_use\",\"arguments\":{\"action\":\"key\",\"keys\":[\"ctrl\",\"s\"]}}</tool_call>\n```",
			want: GuiNativeAction{Action: "key", Keys: []string{"ctrl", "s"}},
		},
		{
			name: "keys as single string",
			raw:  `<tool_call>{"name":"computer_use","arguments":{"action":"key","keys":"Return"}}</tool_call>`,
			want: GuiNativeAction{Action: "key", Keys: []string{"Return"}},
		},
		{
			name: "scroll with pixels",
			raw:  `<tool_call>{"name":"computer_use","arguments":{"action":"scroll","pixels":-300}}</tool_call>`,
			want: GuiNativeAction{Action: "scroll", Pixels: float64Ptr(-300)},
		},
		{
			name: "wait seconds",
			raw:  `<tool_call>{"name":"computer_use","arguments":{"action":"wait","time":2}}</tool_call>`,
			want: GuiNativeAction{Action: "wait", Time: float64Ptr(2)},
		},
		{
			name: "terminate success",
			raw:  `<tool_call>{"name":"computer_use","arguments":{"action":"terminate","status":"success"}}</tool_call>`,
			want: GuiNativeAction{Action: "terminate", Status: "success"},
		},
		{
			name: "answer text",
			raw:  `<tool_call>{"name":"computer_use","arguments":{"action":"answer","text":"共找到 3 条记录"}}</tool_call>`,
			want: GuiNativeAction{Action: "answer", Text: "共找到 3 条记录"},
		},
	}
	for _, tc := range cases {
		got, err := ParseGuiNativeToolCall(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Action != tc.want.Action {
			t.Fatalf("%s: action=%q want %q", tc.name, got.Action, tc.want.Action)
		}
		if got.Text != tc.want.Text || len(got.Keys) != len(tc.want.Keys) {
			t.Fatalf("%s: %+v want %+v", tc.name, got, tc.want)
		}
		if len(got.Keys) > 0 && strings.Join(got.Keys, ",") != strings.Join(tc.want.Keys, ",") {
			t.Fatalf("%s: keys=%v want %v", tc.name, got.Keys, tc.want.Keys)
		}
		if len(got.Coordinate) != len(tc.want.Coordinate) {
			t.Fatalf("%s: coordinate=%v want %v", tc.name, got.Coordinate, tc.want.Coordinate)
		}
		if (got.Pixels == nil) != (tc.want.Pixels == nil) || (got.Pixels != nil && *got.Pixels != *tc.want.Pixels) {
			t.Fatalf("%s: pixels=%v want %v", tc.name, got.Pixels, tc.want.Pixels)
		}
		if (got.Time == nil) != (tc.want.Time == nil) || (got.Time != nil && *got.Time != *tc.want.Time) {
			t.Fatalf("%s: time=%v want %v", tc.name, got.Time, tc.want.Time)
		}
		if got.Status != tc.want.Status {
			t.Fatalf("%s: status=%q want %q", tc.name, got.Status, tc.want.Status)
		}
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestParseGuiNativeToolCallErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"no json at all", "我觉得应该长按屏幕。"},
		{"unknown action", `<tool_call>{"name":"computer_use","arguments":{"action":"poke"}}</tool_call>`},
		{"click without coordinate", `<tool_call>{"name":"computer_use","arguments":{"action":"left_click"}}</tool_call>`},
		{"click with one coordinate", `<tool_call>{"name":"computer_use","arguments":{"action":"left_click","coordinate":[100]}}</tool_call>`},
		{"negative coordinate", `<tool_call>{"name":"computer_use","arguments":{"action":"mouse_move","coordinate":[-5,100]}}</tool_call>`},
		{"type without text", `<tool_call>{"name":"computer_use","arguments":{"action":"type","text":""}}</tool_call>`},
		{"key without keys", `<tool_call>{"name":"computer_use","arguments":{"action":"key"}}</tool_call>`},
		{"terminate without status", `<tool_call>{"name":"computer_use","arguments":{"action":"terminate"}}</tool_call>`},
		{"terminate bad status", `<tool_call>{"name":"computer_use","arguments":{"action":"terminate","status":"maybe"}}</tool_call>`},
		{"drag without coordinate", `<tool_call>{"name":"computer_use","arguments":{"action":"left_click_drag"}}</tool_call>`},
	}
	for _, tc := range cases {
		if _, err := ParseGuiNativeToolCall(tc.raw); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestGuiNativeSignal(t *testing.T) {
	if s := GuiNativeSignal(GuiNativeAction{Action: "terminate", Status: "success"}); s != "done" {
		t.Fatalf("terminate success = %q", s)
	}
	if s := GuiNativeSignal(GuiNativeAction{Action: "terminate", Status: "failure"}); s != "fail" {
		t.Fatalf("terminate failure = %q", s)
	}
	if s := GuiNativeSignal(GuiNativeAction{Action: "answer", Text: "ok"}); s != "done" {
		t.Fatalf("answer = %q", s)
	}
	if s := GuiNativeSignal(GuiNativeAction{Action: "interact", Text: "请登录"}); s != "ask" {
		t.Fatalf("interact = %q", s)
	}
	if s := GuiNativeSignal(GuiNativeAction{Action: "left_click"}); s != "" {
		t.Fatalf("click must not be a signal: %q", s)
	}
}

func TestMapGuiNativeActionTable(t *testing.T) {
	cursor := &GuiNativePoint{X: 100, Y: 200}
	cases := []struct {
		name     string
		action   GuiNativeAction
		want     string
		wantErr  bool
		noCursor bool
	}{
		{
			name:   "left_click per-mille",
			action: GuiNativeAction{Action: "left_click", Coordinate: []float64{500, 250}},
			want:   `{"action":"click","x":960,"y":270}`,
		},
		{
			name:   "coordinate above 1000 is absolute frame pixels",
			action: GuiNativeAction{Action: "left_click", Coordinate: []float64{1500, 100}},
			want:   `{"action":"click","x":1500,"y":100}`,
		},
		{
			name:   "right_click",
			action: GuiNativeAction{Action: "right_click", Coordinate: []float64{10, 10}},
			want:   `{"action":"right_click","x":19,"y":11}`,
		},
		{
			name:   "middle_click",
			action: GuiNativeAction{Action: "middle_click", Coordinate: []float64{10, 10}},
			want:   `{"action":"middle_click","x":19,"y":11}`,
		},
		{
			name:   "double_click",
			action: GuiNativeAction{Action: "double_click", Coordinate: []float64{100, 100}},
			want:   `{"action":"double_click","x":192,"y":108}`,
		},
		{
			name:   "triple_click maps to double_click",
			action: GuiNativeAction{Action: "triple_click", Coordinate: []float64{100, 100}},
			want:   `{"action":"double_click","x":192,"y":108}`,
		},
		{
			name:   "mouse_move",
			action: GuiNativeAction{Action: "mouse_move", Coordinate: []float64{400, 600}},
			want:   `{"action":"move","x":768,"y":648}`,
		},
		{
			name:   "drag uses the tracked cursor as start",
			action: GuiNativeAction{Action: "left_click_drag", Coordinate: []float64{800, 400}},
			want:   `{"action":"drag","x1":100,"y1":200,"x2":1536,"y2":432}`,
		},
		{
			name:     "drag without a known cursor fails",
			action:   GuiNativeAction{Action: "left_click_drag", Coordinate: []float64{800, 400}},
			wantErr:  true,
			noCursor: true,
		},
		{
			name:   "scroll pixels to notches",
			action: GuiNativeAction{Action: "scroll", Pixels: float64Ptr(-300)},
			want:   `{"action":"scroll","scroll":-3}`,
		},
		{
			name:   "scroll clamps at 12 notches",
			action: GuiNativeAction{Action: "scroll", Pixels: float64Ptr(5000)},
			want:   `{"action":"scroll","scroll":12}`,
		},
		{
			name:   "scroll small value keeps one notch",
			action: GuiNativeAction{Action: "scroll", Pixels: float64Ptr(-40)},
			want:   `{"action":"scroll","scroll":-1}`,
		},
		{
			name:   "scroll without pixels defaults down",
			action: GuiNativeAction{Action: "scroll"},
			want:   `{"action":"scroll","scroll":-3}`,
		},
		{
			name:   "hscroll adds axis",
			action: GuiNativeAction{Action: "hscroll", Pixels: float64Ptr(200)},
			want:   `{"action":"scroll","scroll":2,"scrollAxis":"horizontal"}`,
		},
		{
			name:   "wait seconds to ms",
			action: GuiNativeAction{Action: "wait", Time: float64Ptr(2)},
			want:   `{"action":"wait","ms":2000}`,
		},
		{
			name:   "wait clamps high",
			action: GuiNativeAction{Action: "wait", Time: float64Ptr(30)},
			want:   `{"action":"wait","ms":5000}`,
		},
		{
			name:   "wait without time defaults",
			action: GuiNativeAction{Action: "wait"},
			want:   `{"action":"wait","ms":800}`,
		},
		{
			name:   "key array",
			action: GuiNativeAction{Action: "key", Keys: []string{"ctrl", "s"}},
			want:   `{"action":"key","keys":["ctrl","s"]}`,
		},
		{
			name:   "type ascii",
			action: GuiNativeAction{Action: "type", Text: "hello"},
			want:   `{"action":"type","text":"hello"}`,
		},
		{
			name:   "type cjk becomes paste",
			action: GuiNativeAction{Action: "type", Text: "你好世界"},
			want:   `{"action":"paste","text":"你好世界"}`,
		},
		{
			name:    "terminate is not executable",
			action:  GuiNativeAction{Action: "terminate", Status: "success"},
			wantErr: true,
		},
		{
			name:    "answer is not executable",
			action:  GuiNativeAction{Action: "answer", Text: "ok"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		useCursor := cursor
		if tc.noCursor {
			useCursor = nil
		}
		raw, err := MapGuiNativeAction(tc.action, 1920, 1080, useCursor)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got %s", tc.name, raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got, want map[string]any
		if json.Unmarshal(raw, &got) != nil || json.Unmarshal([]byte(tc.want), &want) != nil {
			t.Fatalf("%s: bad json %s", tc.name, raw)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: got %s want %s", tc.name, raw, tc.want)
		}
		for k, v := range want {
			gv, ok := got[k]
			if !ok {
				t.Fatalf("%s: missing %q in %s", tc.name, k, raw)
			}
			gn, gIsNum := gv.(float64)
			wn, wIsNum := v.(float64)
			if gIsNum && wIsNum {
				if gn != wn {
					t.Fatalf("%s: %s got %v want %v", tc.name, k, gv, v)
				}
				continue
			}
			if fmtAny(gv) != fmtAny(v) {
				t.Fatalf("%s: %s got %v want %v", tc.name, k, gv, v)
			}
		}
	}
}

func fmtAny(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestMapGuiNativeActionCoordEdges(t *testing.T) {
	// Zero frame size must fail closed instead of clicking (0,0).
	if _, err := MapGuiNativeAction(GuiNativeAction{Action: "left_click", Coordinate: []float64{500, 500}}, 0, 0, nil); err == nil {
		t.Fatal("zero frame must fail")
	}
	// Absolute pixel past the frame clamps inside.
	raw, err := MapGuiNativeAction(GuiNativeAction{Action: "left_click", Coordinate: []float64{4000, 2000}}, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"x":1919`) || !strings.Contains(string(raw), `"y":1079`) {
		t.Fatalf("clamp: %s", raw)
	}
	// Per-mille at the far edge clamps inside too.
	raw, err = MapGuiNativeAction(GuiNativeAction{Action: "left_click", Coordinate: []float64{1000, 1000}}, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"x":1919`) || !strings.Contains(string(raw), `"y":1079`) {
		t.Fatalf("edge clamp: %s", raw)
	}
}
