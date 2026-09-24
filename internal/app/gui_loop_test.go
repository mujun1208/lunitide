package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type guiLoopScript struct {
	replies  []string
	replyIdx int
	execArgs []json.RawMessage
	execPix  []bool
	execOut  []toolruntime.Result
	observes int
	frame    string
	nodes    int
	hits     map[string]bool
}

func (s *guiLoopScript) runtime(goal string) guiLoopRuntime {
	img := []llmadapter.Image{{MIME: "image/png", Data: []byte{1}}}
	return guiLoopRuntime{
		guiFallbackRuntime: guiFallbackRuntime{
			Goal:    goal,
			FrameID: s.frame,
			Nodes:   s.nodes,
			VisW:    1000,
			VisH:    500,
			Images:  img,
			Observe: func() (string, int, int, int, []llmadapter.Image, error) {
				s.observes++
				return s.frame, s.nodes, 1000, 500, img, nil
			},
			Complete: func(exec guiExecutor, images []llmadapter.Image, prompt string) (string, error) {
				if s.replyIdx >= len(s.replies) {
					return "", errors.New("script exhausted")
				}
				r := s.replies[s.replyIdx]
				s.replyIdx++
				return r, nil
			},
			HasHit: func(id string) bool { return s.hits[id] },
		},
		Exec: func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error) {
			s.execArgs = append(s.execArgs, args)
			s.execPix = append(s.execPix, allowPixels)
			i := len(s.execArgs) - 1
			if i < len(s.execOut) {
				return s.execOut[i], nil
			}
			return toolruntime.Result{Output: "ok:true"}, nil
		},
	}
}

func guiLoopR2(gui, vision bool) guiFallbackIn {
	return guiFallbackIn{ccOn: true, route: RouteR2, guiCatalog: gui, visionCatalog: vision}
}

func TestGUILoopClickThenDone(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   4,
		hits:    map[string]bool{"B1": true},
		replies: []string{`{"action":"click","markId":"B1"}`, `{"action":"done","reason":"设置窗口已打开"}`},
	}
	res, args, used := runGUILoop(guiLoopR2(true, false), s.runtime("打开设置"))
	if !used {
		t.Fatal("loop must engage")
	}
	if !strings.HasPrefix(res.Output, "ok:true") || !strings.Contains(res.Output, "设置窗口已打开") {
		t.Fatalf("output=%q", res.Output)
	}
	if len(s.execArgs) != 1 || !strings.Contains(string(s.execArgs[0]), `"id":"B1"`) {
		t.Fatalf("exec args=%v", s.execArgs)
	}
	if s.execPix[0] {
		t.Fatal("marks present: pixel clicks must stay closed")
	}
	if s.observes != 1 {
		t.Fatalf("expected one verify observe after the action, got %d", s.observes)
	}
	if !strings.Contains(string(args), `"gui_loop"`) {
		t.Fatalf("display args=%s", args)
	}
	if len(res.VisionData) == 0 {
		t.Fatal("final frame must be returned so the main model sees the result")
	}
}

func TestGUILoopMissingMarkRefreshesTheFrame(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   4,
		hits:    map[string]bool{"B1": true},
		replies: []string{`{"action":"click","markId":"Z9"}`, `{"action":"done","reason":"已看清屏幕"}`},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("打开设置"))
	if !used {
		t.Fatal("loop must engage")
	}
	if s.observes < 1 {
		t.Fatal("a mark that is not on this frame must refresh the screen before the next step")
	}
	if len(s.execArgs) != 0 {
		t.Fatalf("missing mark was clicked: %s", s.execArgs)
	}
	if !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("output=%q", res.Output)
	}
}

func TestGUILoopEmptyTreeUsesPerMilleCoordinates(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f9",
		nodes:   0,
		replies: []string{`{"action":"click","x":500,"y":200,"frameId":"f9"}`, `{"action":"type","text":"hello"}`, `{"action":"done"}`},
	}
	res, _, used := runGUILoop(guiLoopR2(false, true), s.runtime("在输入框里输入 hello"))
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 2 {
		t.Fatalf("exec=%v", s.execArgs)
	}
	var click struct {
		Action  string `json:"action"`
		X, Y    int
		FrameID string `json:"frameId"`
	}
	if err := json.Unmarshal(s.execArgs[0], &click); err != nil {
		t.Fatal(err)
	}
	if click.Action != "click" || click.X != 500 || click.Y != 100 || click.FrameID != "f9" {
		t.Fatalf("click=%+v", click)
	}
	if !s.execPix[0] {
		t.Fatal("empty tree click must open the pixel gate for that one action")
	}
	if !strings.Contains(string(s.execArgs[1]), `"text":"hello"`) {
		t.Fatalf("type=%s", s.execArgs[1])
	}
}

func TestGUILoopRejectsStaleFrameAndUnknownMark(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f2",
		nodes:   0,
		replies: []string{`{"action":"click","x":10,"y":10,"frameId":"old"}`, `{"action":"click","x":10,"y":10,"frameId":"stale"}`},
	}
	res, _, used := runGUILoop(guiLoopR2(false, true), s.runtime("点一下"))
	if !used || !strings.HasPrefix(res.Output, "ok:false") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 0 {
		t.Fatalf("stale frames must never reach the host: %v", s.execArgs)
	}
	s2 := &guiLoopScript{
		frame:   "f3",
		nodes:   3,
		hits:    map[string]bool{"B1": true},
		replies: []string{`{"action":"click","markId":"B7"}`, `{"action":"click","markId":"B8"}`},
	}
	res, _, _ = runGUILoop(guiLoopR2(true, false), s2.runtime("点一下"))
	if !strings.HasPrefix(res.Output, "ok:false") || len(s2.execArgs) != 0 {
		t.Fatalf("unknown marks must not click: %q %v", res.Output, s2.execArgs)
	}
}

func TestGUILoopStopsOnModelFailAndBudget(t *testing.T) {
	s := &guiLoopScript{frame: "f4", nodes: 2, replies: []string{`{"action":"fail","reason":"需要先登录"}`}}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("发消息"))
	if !used || !strings.Contains(res.Output, "ok:false") || !strings.Contains(res.Output, "需要先登录") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	s = &guiLoopScript{
		frame: "f5", nodes: 2, hits: map[string]bool{"B1": true},
		replies: []string{`{"action":"click","markId":"B1"}`, `{"action":"click","markId":"B1"}`, `{"action":"click","markId":"B1"}`},
	}
	rt := s.runtime("一直点")
	rt.MaxSteps = 2
	res, _, _ = runGUILoop(guiLoopR2(true, false), rt)
	if !strings.Contains(res.Output, "已用完 2 步") || len(s.execArgs) != 2 {
		t.Fatalf("budget: %q exec=%d", res.Output, len(s.execArgs))
	}
}

func TestGUILoopStopsAfterConsecutiveHostFailures(t *testing.T) {
	s := &guiLoopScript{
		frame: "f6", nodes: 2, hits: map[string]bool{"B1": true, "B2": true},
		replies: []string{`{"action":"click","markId":"B1"}`, `{"action":"click","markId":"B2"}`, `{"action":"click","markId":"B1"}`},
		execOut: []toolruntime.Result{{Output: "ok:false\n屏幕未变化"}, {Output: "ok:false\n屏幕未变化"}},
	}
	res, _, _ := runGUILoop(guiLoopR2(true, false), s.runtime("点按钮"))
	if !strings.Contains(res.Output, "连续失败") || len(s.execArgs) != 2 {
		t.Fatalf("out=%q exec=%d", res.Output, len(s.execArgs))
	}
}

func TestGUILoopNeedsAnExecutorAndAnR2Route(t *testing.T) {
	s := &guiLoopScript{frame: "f7", nodes: 2, replies: []string{`{"action":"done"}`}}
	if _, _, used := runGUILoop(guiLoopR2(false, false), s.runtime("x")); used {
		t.Fatal("no gui/vision model: loop must not engage")
	}
	in := guiLoopR2(true, true)
	in.route = RouteR1
	if _, _, used := runGUILoop(in, s.runtime("x")); used {
		t.Fatal("R1 must not run the desktop loop")
	}
	in = guiLoopR2(true, true)
	in.route = RouteUnspecified
	in.usedScreenTools = true
	if _, _, used := runGUILoop(in, s.runtime("x")); !used {
		t.Fatal("unspecified route that already drove the screen must still get the visual loop")
	}
	in = guiLoopR2(true, true)
	in.desktopTypeL0Passed = true
	if _, _, used := runGUILoop(in, s.runtime("x")); used {
		t.Fatal("L0-passed typing must not be retried visually")
	}
}

func TestGUILoopDoneWithoutActionIsHonest(t *testing.T) {
	s := &guiLoopScript{frame: "f8", nodes: 2, replies: []string{`{"action":"done","reason":"已经在设置页"}`}}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("打开设置"))
	if !used || !strings.Contains(res.Output, "未执行新动作") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
}

func TestGUILoopStopsWhenTheGoalNamesTwoWindows(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1", nodes: 2, hits: map[string]bool{"B1": true},
		replies: []string{`{"action":"click","markId":"B1"}`},
	}
	rt := s.runtime("在记事本和微信里发一句")
	rt.Lock = func() (string, error) {
		return "", errors.New("找到多个窗口：记事本、微信。先指定一个再操作。")
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), rt)
	if !used || !strings.Contains(res.Output, "找到多个窗口") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 0 {
		t.Fatalf("ambiguous windows must not be clicked: %s", s.execArgs)
	}
}

func TestGUILoopDoesNotRepeatAFailedMark(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1", nodes: 4, hits: map[string]bool{"B1": true, "B2": true},
		replies: []string{
			`{"action":"click","markId":"B1"}`,
			`{"action":"click","markId":"B1"}`,
			`{"action":"click","markId":"B2"}`,
			`{"action":"done","reason":"已打开"}`,
		},
		execOut: []toolruntime.Result{{Output: "ok:false\n屏幕未变化"}},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("打开设置"))
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 2 || strings.Contains(string(s.execArgs[0]), "B2") || !strings.Contains(string(s.execArgs[1]), "B2") {
		t.Fatalf("failed mark must not be clicked again: %s", s.execArgs)
	}
}

func TestGUILoopRefusesTypeUntilFocusIsInAField(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1", nodes: 3, hits: map[string]bool{"E1": true},
		replies: []string{
			`{"action":"type","text":"你好"}`,
			`{"action":"click","markId":"E1"}`,
			`{"action":"type","text":"你好"}`,
			`{"action":"done","reason":"已输入"}`,
		},
	}
	focused := false
	rt := s.runtime("在输入框输入你好")
	rt.FocusEditable = func() (bool, bool) { return true, focused }
	rt.Exec = func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error) {
		s.execArgs = append(s.execArgs, args)
		if strings.Contains(string(args), `"id":"E1"`) {
			focused = true
		}
		return toolruntime.Result{Output: "ok:true"}, nil
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), rt)
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 2 {
		t.Fatalf("exec=%s", s.execArgs)
	}
	if strings.Contains(string(s.execArgs[0]), "你好") {
		t.Fatal("typed before the field had focus")
	}
	if !strings.Contains(string(s.execArgs[1]), "你好") {
		t.Fatalf("second type=%s", s.execArgs[1])
	}
}

func TestParseGUILoopActionGrammar(t *testing.T) {
	if _, err := parseGUILoopAction(`{"action":"key","keys":["ctrl","s"]}`, false, "f"); err != nil {
		t.Fatal(err)
	}
	a, err := parseGUILoopAction("```json\n{\"action\":\"scroll\"}\n```", false, "f")
	if err != nil || a.Scroll != -3 {
		t.Fatalf("scroll default: %+v %v", a, err)
	}
	if _, err := parseGUILoopAction(`{"action":"type","text":""}`, false, "f"); err == nil {
		t.Fatal("empty type must fail")
	}
	raw, err := buildGUILoopArgs(guiLoopAction{Action: "type", Text: "你好世界"}, false, 1000, 500)
	if err != nil || !strings.Contains(string(raw), `"action":"paste"`) {
		t.Fatalf("CJK type must emit paste: %s %v", raw, err)
	}
	if _, err := parseGUILoopAction(`{"action":"click","markId":"B1"}`, true, "f"); err == nil {
		t.Fatal("mark on an empty tree must fail")
	}
	if _, err := parseGUILoopAction(`{"action":"click","x":1200,"y":10,"frameId":"f"}`, true, "f"); err == nil {
		t.Fatal("per-mille out of range must fail")
	}
	a, err = parseGUILoopAction(`{"action":"wait","ms":99999}`, false, "f")
	if err != nil || a.Ms != 5000 {
		t.Fatalf("wait clamp: %+v %v", a, err)
	}
}

func TestObserveReturnedEmptyTree(t *testing.T) {
	if !observeReturnedEmptyTree(`{"count":0,"nodes":[],"refused":"","space":"image","frameId":"f"}`) {
		t.Fatal("count 0 must read as empty")
	}
	if !observeReturnedEmptyTree(`{"count": 0, "nodes": []}`) {
		t.Fatal("spaced json must read as empty")
	}
	if observeReturnedEmptyTree(`{"count":12,"nodes":[...]}`) {
		t.Fatal("non-empty tree must not trigger")
	}
}
