package ccapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

// chainHost records one OpenClaw-shaped computer.act session.
type chainHost struct {
	nativeStubHost
	png        []byte
	captures   int
	typed      []string
	shortcuts  [][]string
	clicks     []string
	scrolls    []int
	pasted     []string
	menus      []string
	values     [][2]string
	invokes    []string
	invokeFail error
	clip       string
	hitName    string
}

func (h *chainHost) ScreenCapture() ([]byte, error) {
	h.captures++
	return tintPNG(h.png, h.captures), nil
}

func (h *chainHost) WindowCapture(string) ([]byte, int, int, error) {
	raw, err := h.ScreenCapture()
	return raw, 0, 0, err
}

func (h *chainHost) MouseClick(button string, clicks int) error {
	h.clicks = append(h.clicks, button)
	_ = clicks
	return nil
}

func (h *chainHost) KeyboardType(text string) error {
	h.typed = append(h.typed, text)
	return nil
}

func (h *chainHost) KeyboardShortcut(keys []string) error {
	h.shortcuts = append(h.shortcuts, append([]string(nil), keys...))
	return nil
}

func (h *chainHost) MouseScroll(n int) error {
	h.scrolls = append(h.scrolls, n)
	return nil
}

func (h *chainHost) InvokeUI(name string) error {
	h.invokes = append(h.invokes, name)
	if h.invokeFail != nil {
		return h.invokeFail
	}
	return nil
}

func (h *chainHost) ClipboardGet() (string, error) { return h.clip, nil }
func (h *chainHost) ClipboardSet(text string) error {
	h.clip = text
	h.pasted = append(h.pasted, text)
	return nil
}
func (h *chainHost) MenuClick(path string) error {
	h.menus = append(h.menus, path)
	return nil
}
func (h *chainHost) SetValue(target, value string) error {
	h.values = append(h.values, [2]string{target, value})
	return nil
}
func (h *chainHost) HitTest(int, int) (string, error) {
	if h.hitName != "" {
		return h.hitName, nil
	}
	return h.hit, nil
}
func (h *chainHost) ListWindows() ([]WindowInfo, error) {
	return []WindowInfo{{
		ID: "0x1", Title: h.title, Process: h.process, Foreground: true,
		W: 1280, H: 800,
	}}, nil
}
func (h *chainHost) FocusWindow(query string) (WindowInfo, error) {
	return WindowInfo{Title: h.title, Process: h.process, Foreground: true}, nil
}

func tintPNG(src []byte, n int) []byte {
	if len(src) == 0 {
		return src
	}
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return src
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			dst.Set(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)})
		}
	}
	dst.SetRGBA(b.Min.X, b.Min.Y, color.RGBA{R: uint8(n), G: 9, B: 16, A: 255})
	var buf bytes.Buffer
	if png.Encode(&buf, dst) != nil {
		return src
	}
	return buf.Bytes()
}

func newChainService(t *testing.T, nodes []UINode) (*Service, *chainHost) {
	t.Helper()
	host := &chainHost{
		nativeStubHost: nativeStubHost{
			ladderStubHost: ladderStubHost{
				title: "Untitled - Notepad", process: "notepad.exe", nodes: nodes,
			},
			hit: "保存",
		},
		png: tinyPNG(t, 64, 36),
	}
	svc := New(nil)
	svc.SetHost(host)
	svc.SetMutateSettleForTest(time.Millisecond)
	return svc, host
}

func computerAct(s *Service, raw string) (string, []byte, error) {
	tool, args, err := MapComputerAct([]byte(raw))
	if err != nil {
		return "", nil, err
	}
	shortcut, err := s.filterInput(tool, args)
	if err != nil {
		return "", nil, err
	}
	return s.runHost(tool, args, shortcut)
}

func TestComputerActChainCapabilityCatalog(t *testing.T) {
	t.Parallel()
	nodes := []UINode{
		{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24},
		{Role: "edit", Name: "内容", X: 10, Y: 40, W: 200, H: 30},
	}
	svc, host := newChainService(t, nodes)
	host.clip = "clip"

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"screenshot", `{"action":"screenshot"}`, "captured"},
		{"observe", `{"action":"observe"}`, `"role"`},
		{"list", `{"action":"list"}`, "notepad"},
		{"active", `{"action":"active"}`, "notepad"},
		{"focus", `{"action":"focus","title":"Notepad"}`, "Notepad"},
		{"click-name", `{"action":"click","name":"保存"}`, "保存"},
		{"type", `{"action":"type","text":"hi"}`, "typed"},
		{"key-enter", `{"action":"key","key":"enter"}`, "enter"},
		{"hotkey", `{"action":"key","keys":["ctrl","s"]}`, "ctrl"},
		{"scroll", `{"action":"scroll","scroll":-3}`, "scrolled"},
		{"clipboard", `{"action":"clipboard","op":"get"}`, "clip"},
		{"paste", `{"action":"paste","text":"ok"}`, "pasted"},
		{"menu", `{"action":"menu","path":"File > Save"}`, "File"},
		{"set-value", `{"action":"set_value","target":"内容","value":"Ada"}`, "set value"},
		{"wait", `{"action":"wait","ms":1}`, "waited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary, _, err := computerAct(svc, tc.raw)
			if err != nil {
				t.Fatalf("%s: %v (%s)", tc.name, err, summary)
			}
			if !strings.Contains(strings.ToLower(summary), strings.ToLower(tc.want)) && !strings.Contains(summary, tc.want) {
				t.Fatalf("%s summary %q want %q", tc.name, summary, tc.want)
			}
		})
	}
	if len(host.typed) == 0 || host.typed[0] != "hi" {
		t.Fatalf("type must hit keyboard, typed=%v", host.typed)
	}
	if len(host.invokes) == 0 || host.invokes[0] != "保存" {
		t.Fatalf("name click must invoke, invokes=%v", host.invokes)
	}
}

func TestComputerActChainScreenshotXYWithNodes(t *testing.T) {
	t.Parallel()
	svc, host := newChainService(t, []UINode{
		{Role: "button", Name: "播放", X: 200, Y: 400, W: 40, H: 24},
		{Role: "button", Name: "随机播放", X: 10, Y: 10, W: 80, H: 24},
	})
	start := time.Now()
	cap, png, err := computerAct(svc, `{"action":"screenshot"}`)
	if err != nil || len(png) == 0 || !strings.Contains(cap, "frameId=") {
		t.Fatalf("screenshot: %v %s png=%d", err, cap, len(png))
	}
	id := svc.CurrentFrameID()
	if id == "" {
		t.Fatal("screenshot must mint frameId")
	}
	raw, _ := json.Marshal(map[string]any{"action": "click", "x": 20, "y": 10, "frameId": id})
	summary, _, err := computerAct(svc, string(raw))
	if err != nil {
		t.Fatalf("xy after screenshot with UIA nodes must work: %v", err)
	}
	if !strings.Contains(summary, "clicked") {
		t.Fatalf("xy click summary %q", summary)
	}
	if host.movedX == 0 && host.movedY == 0 {
		t.Fatal("xy click must move the pointer")
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("screenshot+xy took %s, OpenClaw path must stay snappy", time.Since(start))
	}
}

func TestComputerActChainNameClickSkipsFrameAndPicksExact(t *testing.T) {
	t.Parallel()
	svc, host := newChainService(t, []UINode{
		{Role: "button", Name: "随机播放", X: 10, Y: 10, W: 80, H: 24},
		{Role: "button", Name: "播放", X: 200, Y: 400, W: 40, H: 24},
		{Role: "button", Name: "历史播放", X: 10, Y: 80, W: 80, H: 24},
	})
	start := time.Now()
	summary, _, err := computerAct(svc, `{"action":"click","name":"播放"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "播放") || strings.Contains(summary, "随机播放") {
		t.Fatalf("exact 播放 must win: %s", summary)
	}
	if len(host.invokes) != 1 || host.invokes[0] != "播放" {
		t.Fatalf("invokes=%v", host.invokes)
	}
	if time.Since(start) > 800*time.Millisecond {
		t.Fatalf("live name click took %s", time.Since(start))
	}
	summary, _, err = computerAct(svc, `{"action":"click","name":"Play"}`)
	if err != nil || !strings.Contains(summary, "播放") {
		t.Fatalf("Play alias: %v %s", err, summary)
	}
}

func TestComputerActChainInvokeFailClicksPixels(t *testing.T) {
	t.Parallel()
	svc, host := newChainService(t, []UINode{
		{Role: "button", Name: "确定", X: 40, Y: 80, W: 60, H: 24},
	})
	host.invokeFail = errors.New("electron invoke refused")
	summary, _, err := computerAct(svc, `{"action":"click","name":"OK"}`)
	if err != nil {
		t.Fatalf("alias+pixel fallback: %v", err)
	}
	if !strings.Contains(summary, "确定") && !strings.Contains(summary, "clicked") {
		t.Fatalf("summary %q", summary)
	}
	if len(host.invokes) != 1 || host.invokes[0] != "确定" {
		t.Fatalf("must try invoke first, invokes=%v", host.invokes)
	}
	if host.movedX != 70 || host.movedY != 92 || len(host.clicks) == 0 {
		t.Fatalf("pixel fallback = move(%d,%d) clicks=%v", host.movedX, host.movedY, host.clicks)
	}
}

func TestComputerActChainXYRequiresCurrentFrame(t *testing.T) {
	t.Parallel()
	svc, _ := newChainService(t, []UINode{{Role: "button", Name: "保存", X: 10, Y: 10, W: 20, H: 20}})
	if _, _, err := computerAct(svc, `{"action":"click","x":8,"y":8}`); err == nil || !strings.Contains(err.Error(), "screenshot") {
		t.Fatalf("xy without frame must fail closed: %v", err)
	}
	if _, _, err := computerAct(svc, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := computerAct(svc, `{"action":"click","x":8,"y":8}`); err == nil || !strings.Contains(err.Error(), "COMPUTER_STALE_FRAME") {
		t.Fatalf("xy after screenshot still needs echoed frameId: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"action": "click", "x": 8, "y": 8, "frameId": "frm_stale"})
	if _, _, err := computerAct(svc, string(raw)); err == nil || !strings.Contains(err.Error(), "COMPUTER_STALE_FRAME") {
		t.Fatalf("wrong frameId must fail: %v", err)
	}
}

func TestComputerActChainSafetyUACAndSelfWindow(t *testing.T) {
	t.Parallel()
	svc, host := newChainService(t, []UINode{{Name: "是", X: 10, Y: 10, W: 40, H: 20}})
	host.title, host.process = "用户账户控制", "consent.exe"
	if _, _, err := computerAct(svc, `{"action":"click","name":"是"}`); !errors.Is(err, ErrCcRiskBlocked) {
		t.Fatalf("UAC: %v", err)
	}
	host.title, host.process = "月伴对话 - Lunitide", "lunitide.exe"
	host.nodes = []UINode{{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24}}
	host.invokeFail = errors.New("skip invoke")
	if _, _, err := computerAct(svc, `{"action":"click","name":"保存"}`); err == nil || !strings.Contains(err.Error(), "Lunitide") {
		t.Fatalf("self window: %v", err)
	}
}

func TestComputerActChainDoubleClickAndDragAndTypeCN(t *testing.T) {
	t.Parallel()
	svc, host := newChainService(t, []UINode{
		{Role: "button", Name: "保存", X: 10, Y: 10, W: 40, H: 20},
		{Role: "button", Name: "打开", X: 80, Y: 10, W: 40, H: 20},
	})
	if _, _, err := computerAct(svc, `{"action":"observe"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := computerAct(svc, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"action": "double_click", "id": "B1", "frameId": svc.CurrentFrameID()})
	host.hitName = "保存"
	summary, _, err := computerAct(svc, string(raw))
	if err != nil || !strings.Contains(summary, "clicked") {
		t.Fatalf("double_click id: %v %s", err, summary)
	}
	raw, _ = json.Marshal(map[string]any{
		"action": "drag", "id": "B1", "id2": "B2", "frameId": svc.CurrentFrameID(),
	})
	if _, _, err := computerAct(svc, string(raw)); err != nil {
		t.Fatalf("drag: %v", err)
	}
	summary, _, err = computerAct(svc, `{"action":"type","text":"你好世界"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "pasted") {
		t.Fatalf("Han must paste, got %s", summary)
	}
}

func TestComputerActMapsStayOnOnePipeline(t *testing.T) {
	t.Parallel()
	for _, action := range []string{
		"screenshot", "click", "double_click", "right_click", "move", "drag",
		"type", "key", "press", "hold_key", "key_up", "scroll", "wait",
		"observe", "observe_dialog", "confirm", "focus", "list", "paste",
		"menu", "set_value", "clipboard", "window_action",
	} {
		raw := map[string]any{"action": action}
		switch action {
		case "click", "double_click", "right_click", "move":
			raw["x"], raw["y"], raw["frameId"] = 1, 1, "frm_x"
		case "drag":
			raw["x1"], raw["y1"], raw["x2"], raw["y2"], raw["frameId"] = 1, 1, 2, 2, "frm_x"
		case "type", "paste":
			raw["text"] = "hi"
		case "key":
			raw["key"] = "enter"
		case "press", "hold_key", "key_up":
			raw["key"] = "shift"
		case "scroll":
			raw["scroll"] = -1
		case "menu":
			raw["path"] = "File > Save"
		case "set_value":
			raw["target"], raw["value"] = "Name", "Ada"
		case "clipboard":
			raw["op"] = "get"
		case "window_action":
			raw["op"] = "restore"
		case "focus":
			raw["title"] = "Notepad"
		}
		body, _ := json.Marshal(raw)
		tool, _, err := MapComputerAct(body)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if tool == ToolComputerAct || !strings.HasPrefix(tool, "cc.") {
			t.Fatalf("%s mapped to %q, want governed cc.*", action, tool)
		}
	}
}

func TestExecuteComputerActNamedStepsOneCall(t *testing.T) {
	t.Parallel()
	svc, tx, _ := executionTestService(t)
	nodes := []UINode{{Role: "button", Name: "保存", X: 40, Y: 80, W: 60, H: 24}}
	host := &chainHost{
		nativeStubHost: nativeStubHost{
			ladderStubHost: ladderStubHost{title: "Untitled - Notepad", process: "notepad.exe", nodes: nodes},
			hit:            "保存",
		},
		png: tinyPNG(t, 64, 36),
	}
	svc.SetHost(host)
	svc.SetMutateSettleForTest(time.Millisecond)
	out, err := svc.ExecuteTool(context.Background(), "s1", ToolComputerAct, []byte(`{"action":"run","steps":[{"action":"focus","title":"Notepad"},{"action":"click","name":"保存"},{"action":"type","text":"hi"}]}`), true)
	if err != nil {
		t.Fatalf("named steps: %v", err)
	}
	if !strings.Contains(out.Summary, "保存") || !strings.Contains(strings.ToLower(out.Summary), "typed") {
		t.Fatalf("batch summary %q", out.Summary)
	}
	if len(host.invokes) == 0 || host.invokes[0] != "保存" {
		t.Fatalf("batch must click by name, invokes=%v", host.invokes)
	}
	if len(host.typed) == 0 || host.typed[0] != "hi" {
		t.Fatalf("batch must type after the named click, typed=%v", host.typed)
	}
	if len(tx.entries) < 3 {
		t.Fatalf("each step must keep its own audit, got %d", len(tx.entries))
	}
}
