package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

const nativeClickReply = "Action: 点击搜索框\n<tool_call>\n{\"name\": \"computer_use\", \"arguments\": {\"action\": \"left_click\", \"coordinate\": [500, 200]}}\n</tool_call>"

const nativeTerminateOK = `<tool_call>{"name":"computer_use","arguments":{"action":"terminate","status":"success"}}</tool_call>`

// The fake runtime frame is 1000x500, so native per-mille (500,200) maps to
// pixels (500,100).
func TestGUILoopNativePromptClickTerminate(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   4,
		replies: []string{nativeClickReply, nativeTerminateOK},
	}
	res, args, used := runGUILoop(guiLoopR2(true, false), s.runtime("打开搜索"))
	if !used {
		t.Fatal("loop must engage")
	}
	if !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("output=%q", res.Output)
	}
	if len(s.prompts) == 0 {
		t.Fatal("no prompt captured")
	}
	// G1: the gui executor must get the native Alibaba Computer System Prompt.
	if !strings.Contains(s.prompts[0], "computer_use") || !strings.Contains(s.prompts[0], "1000x1000") {
		t.Fatalf("native system prompt missing: %q", clipGUIResult(s.prompts[0], 200))
	}
	if strings.Contains(s.prompts[0], "markId") {
		t.Fatal("native prompt must not teach the internal mark grammar")
	}
	// The prompt split in completeGUIStep needs "Goal:" after the system part.
	if i := strings.Index(s.prompts[0], "\n\nGoal:\n"); i < 0 {
		t.Fatalf("native prompt must carry a Goal section: %q", clipGUIResult(s.prompts[0], 200))
	}
	// The click must carry converted frame pixels, not native coordinates.
	if len(s.execArgs) != 1 {
		t.Fatalf("exec=%s", s.execArgs)
	}
	var click struct {
		Action string `json:"action"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
	}
	if err := json.Unmarshal(s.execArgs[0], &click); err != nil {
		t.Fatal(err)
	}
	if click.Action != "click" || click.X != 500 || click.Y != 100 {
		t.Fatalf("click=%+v", click)
	}
	if !strings.Contains(string(args), `"gui_loop"`) {
		t.Fatalf("display args=%s", args)
	}
}

func TestGUILoopNativeSignals(t *testing.T) {
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   2,
		replies: []string{nativeClickReply, `<tool_call>{"name":"computer_use","arguments":{"action":"terminate","status":"failure"}}</tool_call>`},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("打不开"))
	if !used || !strings.HasPrefix(res.Output, "ok:false") {
		t.Fatalf("terminate failure must fail: used=%v out=%q", used, res.Output)
	}

	s = &guiLoopScript{
		frame:   "f1",
		nodes:   2,
		replies: []string{nativeClickReply, `<tool_call>{"name":"computer_use","arguments":{"action":"answer","text":"共 3 条结果"}}</tool_call>`},
	}
	res, _, _ = runGUILoop(guiLoopR2(true, false), s.runtime("查一下"))
	if !strings.HasPrefix(res.Output, "ok:true") || !strings.Contains(res.Output, "共 3 条结果") {
		t.Fatalf("answer must finish ok with text: %q", res.Output)
	}

	s = &guiLoopScript{
		frame:   "f1",
		nodes:   2,
		replies: []string{`<tool_call>{"name":"computer_use","arguments":{"action":"interact","text":"请先登录"}}</tool_call>`},
	}
	res, _, _ = runGUILoop(guiLoopR2(true, false), s.runtime("继续"))
	if !strings.HasPrefix(res.Output, "ok:false") || !strings.Contains(res.Output, "请先登录") {
		t.Fatalf("interact must stop for the user: %q", res.Output)
	}
}

func TestGUILoopNativeDragUsesTrackedCursor(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1",
		nodes: 0,
		replies: []string{
			`<tool_call>{"name":"computer_use","arguments":{"action":"mouse_move","coordinate":[100,100]}}</tool_call>`,
			`<tool_call>{"name":"computer_use","arguments":{"action":"left_click_drag","coordinate":[800,400]}}</tool_call>`,
			nativeTerminateOK,
		},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("拖动滑块"))
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if len(s.execArgs) != 2 {
		t.Fatalf("exec=%s", s.execArgs)
	}
	var drag struct {
		Action        string `json:"action"`
		X1, Y1, X2, Y2 int
	}
	if err := json.Unmarshal(s.execArgs[1], &drag); err != nil {
		t.Fatal(err)
	}
	// frame 1000x500: move (100,100) -> px (100,50); drag to (800,400) -> px (800,200)
	if drag.Action != "drag" || drag.X1 != 100 || drag.Y1 != 50 || drag.X2 != 800 || drag.Y2 != 200 {
		t.Fatalf("drag=%+v", drag)
	}
}

func TestGUILoopNativeDragWithoutCursorFailsGracefully(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1",
		nodes: 0,
		replies: []string{
			`<tool_call>{"name":"computer_use","arguments":{"action":"left_click_drag","coordinate":[800,400]}}</tool_call>`,
			nativeTerminateOK,
		},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("拖动"))
	if !used {
		t.Fatal("loop must engage")
	}
	if len(s.execArgs) != 0 {
		t.Fatalf("drag without cursor must not execute: %s", s.execArgs)
	}
	if !strings.Contains(res.Output, "ok:true") {
		t.Fatalf("model should recover after the refused drag: %q", res.Output)
	}
}

func TestGUILoopNativeRepeatBreaker(t *testing.T) {
	s := &guiLoopScript{
		frame: "f1",
		nodes: 0,
		replies: []string{
			`<tool_call>{"name":"computer_use","arguments":{"action":"left_click","coordinate":[500,200]}}</tool_call>`,
			`<tool_call>{"name":"computer_use","arguments":{"action":"left_click","coordinate":[500,200]}}</tool_call>`,
			`<tool_call>{"name":"computer_use","arguments":{"action":"left_click","coordinate":[500,200]}}</tool_call>`,
			nativeTerminateOK,
		},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("点一下"))
	if !used {
		t.Fatal("loop must engage")
	}
	if !strings.Contains(res.Output, "重复") {
		t.Fatalf("repeat breaker must fire: %q", res.Output)
	}
	if !strings.HasPrefix(res.Output, "ok:false") {
		t.Fatalf("repeat breaker must fail closed: %q", res.Output)
	}
}

func TestGUILoopNativeFallsBackToInternalGrammar(t *testing.T) {
	// A bound gui model that answers the internal grammar (a vision fallback
	// model reached through the gui catalog) must still parse.
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   0,
		replies: []string{`{"action":"click","x":500,"y":200,"frameId":"f1"}`, `{"action":"done","reason":"完成"}`},
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), s.runtime("点一下"))
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
}

func TestGUILoopVisionExecutorParsesNativeToolCall(t *testing.T) {
	// Symmetric fallback: a vision executor whose model happens to emit the
	// gui-plus tool_call format must still drive the loop.
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   0,
		replies: []string{nativeClickReply, nativeTerminateOK},
	}
	res, _, used := runGUILoop(guiLoopR2(false, true), s.runtime("点一下"))
	if !used || !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("used=%v out=%q", used, res.Output)
	}
	if !strings.Contains(s.prompts[0], "markId") {
		t.Fatal("vision executor keeps the internal prompt")
	}
}

func TestGUILoopDoneIsVerifiedIndependently(t *testing.T) {
	// The gui model claims done; the independent verifier disagrees twice.
	// The loop must keep working and then stop honestly instead of
	// self-certifying.
	verifyCalls := 0
	s := &guiLoopScript{
		frame:   "f1",
		nodes:   0,
		replies: []string{nativeClickReply, nativeTerminateOK, nativeTerminateOK},
	}
	rt := s.runtime("打开设置")
	rt.Verify = func(goal string, images []llmadapter.Image) (bool, string, error) {
		verifyCalls++
		return false, "设置窗口没有打开", nil
	}
	res, _, used := runGUILoop(guiLoopR2(true, false), rt)
	if !used {
		t.Fatal("loop must engage")
	}
	if verifyCalls < 2 {
		t.Fatalf("verifier must run on every done claim: %d", verifyCalls)
	}
	if !strings.Contains(res.Output, "独立核验") || !strings.HasPrefix(res.Output, "ok:false") {
		t.Fatalf("out=%q", res.Output)
	}

	// Verifier agrees: the finish must mention the independent check.
	s = &guiLoopScript{
		frame:   "f1",
		nodes:   0,
		replies: []string{nativeClickReply, nativeTerminateOK},
	}
	rt = s.runtime("打开设置")
	rt.Verify = func(goal string, images []llmadapter.Image) (bool, string, error) {
		return true, "设置窗口已在屏幕上", nil
	}
	res, _, _ = runGUILoop(guiLoopR2(true, false), rt)
	if !strings.HasPrefix(res.Output, "ok:true") || !strings.Contains(res.Output, "已独立核验") {
		t.Fatalf("verified finish: %q", res.Output)
	}

	// A broken verifier must not strand the loop: the done stands as before.
	s = &guiLoopScript{
		frame:   "f1",
		nodes:   0,
		replies: []string{nativeClickReply, nativeTerminateOK},
	}
	rt = s.runtime("打开设置")
	rt.Verify = func(goal string, images []llmadapter.Image) (bool, string, error) {
		return false, "", errors.New("no vision model")
	}
	res, _, _ = runGUILoop(guiLoopR2(true, false), rt)
	if !strings.HasPrefix(res.Output, "ok:true") {
		t.Fatalf("verifier outage must not block done: %q", res.Output)
	}
}

func TestParseGuiNativeStepMapping(t *testing.T) {
	a, err := parseGuiNativeStep(nativeClickReply)
	if err != nil {
		t.Fatal(err)
	}
	if a.Action != "left_click" || a.Native == nil || len(a.Native.Coordinate) != 2 {
		t.Fatalf("a=%+v", a)
	}
	if a.X == nil || *a.X != 500 || *a.Y != 200 {
		t.Fatalf("coords for description/repeat tracking: %+v", a)
	}
	a, err = parseGuiNativeStep(nativeTerminateOK)
	if err != nil || a.Action != "done" {
		t.Fatalf("terminate success must map to done: %+v %v", a, err)
	}
	a, err = parseGuiNativeStep(`<tool_call>{"name":"computer_use","arguments":{"action":"answer","text":"找到了"}}</tool_call>`)
	if err != nil || a.Action != "done" || a.Reason != "找到了" {
		t.Fatalf("answer must map to done+reason: %+v %v", a, err)
	}
	a, err = parseGuiNativeStep(`<tool_call>{"name":"computer_use","arguments":{"action":"interact","text":"请扫码"}}</tool_call>`)
	if err != nil || a.Action != "fail" || !strings.Contains(a.Reason, "请扫码") {
		t.Fatalf("interact must map to fail: %+v %v", a, err)
	}
	if _, err := parseGuiNativeStep("我觉得应该长按屏幕"); err == nil {
		t.Fatal("prose must not parse")
	}
}

func TestGUILoopNativeBudgetIsTenSteps(t *testing.T) {
	if maxGUILoopSteps != 10 {
		t.Fatalf("plan hard cap is 10 steps, got %d", maxGUILoopSteps)
	}
}

func TestGuiLoopActionSignature(t *testing.T) {
	x, y := 500.0, 200.0
	a := guiLoopAction{Action: "left_click", X: &x, Y: &y}
	b := guiLoopAction{Action: "left_click", X: &x, Y: &y}
	c := guiLoopAction{Action: "left_click", MarkID: "B1"}
	if guiLoopStepSignature(a) != guiLoopStepSignature(b) {
		t.Fatal("same action must share a signature")
	}
	if guiLoopStepSignature(a) == guiLoopStepSignature(c) {
		t.Fatal("different targets must differ")
	}
	d := guiLoopAction{Action: "type", Text: "hello"}
	e := guiLoopAction{Action: "type", Text: "world"}
	if guiLoopStepSignature(d) == guiLoopStepSignature(e) {
		t.Fatal("different text must differ")
	}
}
