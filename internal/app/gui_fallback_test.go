package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestGUISomPickPromptIsNotVisionDescribe(t *testing.T) {
	t.Parallel()
	if guiSomPickPrompt == visionDescribePrompt {
		t.Fatal("guiSomPickPrompt must not reuse visionDescribePrompt")
	}
	if strings.Contains(guiSomPickPrompt, "Describe the image") || strings.Contains(guiSomPickPrompt, "transcribe all visible text") {
		t.Fatal("SoM prompt must not ask for a vision description")
	}
}

func TestPickGUIFallbackTable(t *testing.T) {
	t.Parallel()
	base := guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2, guiCatalog: true, visionCatalog: true}
	cases := []struct {
		name string
		in   guiFallbackIn
		want guiExecutor
	}{
		{name: "D-D5 R1", in: guiFallbackIn{ccOn: true, route: RouteR1, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "D-D5 R0", in: guiFallbackIn{ccOn: true, route: RouteR0, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "D-D5 R4", in: guiFallbackIn{ccOn: true, route: RouteR4, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "unspecified", in: guiFallbackIn{ccOn: true, route: RouteUnspecified, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "D-D12 subagent", in: guiFallbackIn{ccOn: true, isSubagent: true, route: RouteR2, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "D-N1 cc off", in: guiFallbackIn{ccOn: false, route: RouteR2, nodeCount: 2, guiCatalog: true}, want: guiExecNone},
		{name: "D-D6 desktop.type L0", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2, guiCatalog: true, desktopTypeL0Passed: true}, want: guiExecNone},
		{name: "already used", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2, guiCatalog: true, alreadyUsed: true}, want: guiExecNone},
		{name: "R2 gui nodes", in: base, want: guiExecGUI},
		{name: "R3 gui nodes", in: guiFallbackIn{ccOn: true, route: RouteR3, nodeCount: 1, guiCatalog: true}, want: guiExecGUI},
		{name: "D-D7 vision only with tree", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 3, visionCatalog: true}, want: guiExecVision},
		{name: "gui preferred over vision", in: base, want: guiExecGUI},
		{name: "D-D8 no catalog with tree", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2}, want: guiExecNone},
		{name: "D-D8 empty tree no gui", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 0, visionCatalog: true}, want: guiExecNone},
		{name: "D-D9 empty tree + gui", in: guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 0, guiCatalog: true}, want: guiExecGUI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickGUIFallback(tc.in); got != tc.want {
				t.Fatalf("pickGUIFallback = %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseSOMPickMarkID(t *testing.T) {
	t.Parallel()
	got, err := parseSOMPick(`{"markId":"B1"}`, false, "frm_1")
	if err != nil || got.MarkID != "B1" {
		t.Fatalf("D-D7 string markId: %+v %v", got, err)
	}
	got, err = parseSOMPick("here you go\n{\"markId\":\"e12\"}\nthanks", false, "frm_1")
	if err != nil || got.MarkID != "E12" {
		t.Fatalf("extract one object: %+v %v", got, err)
	}
	if _, err := parseSOMPick(`{"markId":1}`, false, "frm_1"); err == nil {
		t.Fatal("D-D7 integer markId must fail")
	}
	if _, err := parseSOMPick(`click B1`, false, "frm_1"); err == nil {
		t.Fatal("prose must fail")
	}
	if _, err := parseSOMPick(`{"x":0.2,"y":0.3,"frameId":"frm_1"}`, false, "frm_1"); err == nil {
		t.Fatal("tree pick must require markId string")
	}
}

func TestParseSOMPickEmptyTreeCoords(t *testing.T) {
	t.Parallel()
	got, err := parseSOMPick(`{"x":0.25,"y":0.5,"frameId":"frm_empty"}`, true, "frm_empty")
	if err != nil || got.X == nil || got.Y == nil || *got.X != 0.25 || *got.Y != 0.5 || got.FrameID != "frm_empty" {
		t.Fatalf("D-D9 unit coords: %+v %v", got, err)
	}
	got, err = parseSOMPick(`{"x":250,"y":500,"frameId":"frm_empty"}`, true, "frm_empty")
	if err != nil || got.X == nil || *got.X != 250 {
		t.Fatalf("D-D9 0-1000: %+v %v", got, err)
	}
	if _, err := parseSOMPick(`{"x":0.25,"y":0.5,"frameId":"other"}`, true, "frm_empty"); err == nil {
		t.Fatal("D-D9 frameId must match this observe")
	}
	if _, err := parseSOMPick(`{"markId":"B1"}`, true, "frm_empty"); err == nil {
		t.Fatal("empty tree must not accept markId")
	}
	if _, err := parseSOMPick(`{"x":-1,"y":0.2,"frameId":"frm_empty"}`, true, "frm_empty"); err == nil {
		t.Fatal("negative xy rejected")
	}
	if _, err := parseSOMPick(`{"x":1001,"y":1,"frameId":"frm_empty"}`, true, "frm_empty"); err == nil {
		t.Fatal("xy > 1000 rejected")
	}
}

func TestMapGUICoord(t *testing.T) {
	t.Parallel()
	px, err := mapGUICoord(0.5, 200)
	if err != nil || px != 100 {
		t.Fatalf("0.5 of 200 = %d %v", px, err)
	}
	px, err = mapGUICoord(250, 200)
	if err != nil || px != 50 {
		t.Fatalf("250/1000 of 200 = %d %v", px, err)
	}
	px, err = mapGUICoord(1, 200)
	if err != nil || px != 199 {
		t.Fatalf("1.0 clamps to last pixel, got %d %v", px, err)
	}
}

func TestNoteDesktopGUIFailKeepsPriorDesktopFailure(t *testing.T) {
	t.Parallel()
	failed := noteDesktopGUIFail("computer.act", "ok:false\n无法执行", nil, false)
	if !failed {
		t.Fatal("failed computer.act must set")
	}
	if !noteDesktopGUIFail("web.search", "ok:true", nil, failed) {
		t.Fatal("later web.search must not clear a desktop fail")
	}
	if noteDesktopGUIFail("computer.act", `clicked {"l0":{"kind":"click","passed":true}}`, nil, failed) {
		t.Fatal("later successful computer.act must clear")
	}
	if !computerActIsObserve("computer.act", json.RawMessage(`{"action":"observe"}`)) {
		t.Fatal("observe action")
	}
	if computerActIsObserve("computer.act", json.RawMessage(`{"action":"click","id":"B1"}`)) {
		t.Fatal("click is not observe")
	}
}

func TestDesktopToolFailedForGUI(t *testing.T) {
	t.Parallel()
	if !desktopToolFailedForGUI("computer.act", "ok:false\n无法执行", nil) {
		t.Fatal("ok:false computer.act must trigger")
	}
	if !desktopToolFailedForGUI("desktop.type", "field missing", errSentinel("boom")) {
		t.Fatal("toolErr desktop.type must trigger")
	}
	if desktopToolFailedForGUI("web.search", "ok:false", nil) {
		t.Fatal("other tools do not trigger")
	}
	if desktopToolFailedForGUI("desktop.type", `typed ok {"l0":{"kind":"type","passed":true}}`, nil) {
		t.Fatal("D-D6 L0 passed must not trigger")
	}
	if !desktopTypePassedL0("desktop.type", `{"l0":{"kind":"type","passed":true}}`) {
		t.Fatal("desktop.type L0.passed")
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestRunGUIFallbackOnceAndGates(t *testing.T) {
	t.Parallel()
	var completes, observes, clicks int
	rt := guiFallbackRuntime{
		Goal:    "点保存",
		FrameID: "frm_1",
		Nodes:   2,
		VisW:    100,
		VisH:    100,
		Images:  []gateway.Image{{MIME: "image/png", Data: []byte("png")}},
		Observe: func() (string, int, int, int, []gateway.Image, error) {
			observes++
			return "frm_1", 2, 100, 100, []gateway.Image{{MIME: "image/png", Data: []byte("png")}}, nil
		},
		Complete: func(exec guiExecutor, _ []gateway.Image, prompt string) (string, error) {
			completes++
			if exec != guiExecGUI {
				t.Fatalf("want gui executor, got %s", exec)
			}
			if !strings.Contains(prompt, guiSomPickPrompt) || strings.Contains(prompt, visionDescribePrompt) {
				t.Fatalf("prompt: %s", prompt)
			}
			return `{"markId":"B1"}`, nil
		},
		HasHit: func(id string) bool { return id == "B1" },
		Click: func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error) {
			clicks++
			if allowPixels {
				t.Fatal("tree click must not open GUI pixels")
			}
			var m map[string]any
			if json.Unmarshal(args, &m) != nil || m["id"] != "B1" {
				t.Fatalf("click args %s", args)
			}
			return toolruntime.Result{Output: "clicked B1"}, nil
		},
	}
	in := guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2, guiCatalog: true}
	res, args, used := runGUIFallback(in, rt)
	if !used || res.Output != "clicked B1" || !strings.Contains(string(args), `"B1"`) {
		t.Fatalf("first: used=%v out=%q args=%s", used, res.Output, args)
	}
	if observes != 0 || completes != 1 || clicks != 1 {
		t.Fatalf("first counters observe=%d complete=%d click=%d", observes, completes, clicks)
	}

	in.alreadyUsed = true
	_, _, used = runGUIFallback(in, rt)
	if used || completes != 1 {
		t.Fatal("本轮只一次: alreadyUsed must not Complete again")
	}

	in = guiFallbackIn{ccOn: true, route: RouteR1, nodeCount: 2, guiCatalog: true}
	_, _, used = runGUIFallback(in, rt)
	if used || completes != 1 {
		t.Fatal("D-D5 R1 must not enter")
	}

	in = guiFallbackIn{ccOn: true, isSubagent: true, route: RouteR2, nodeCount: 2, guiCatalog: true}
	_, _, used = runGUIFallback(in, rt)
	if used || completes != 1 {
		t.Fatal("D-D12 subagent must not enter")
	}
}

func TestRunGUIFallbackEmptyTreePixels(t *testing.T) {
	t.Parallel()
	var allow bool
	rt := guiFallbackRuntime{
		Goal:   "点那个按钮",
		Images: nil,
		Observe: func() (string, int, int, int, []gateway.Image, error) {
			return "frm_empty", 0, 200, 100, []gateway.Image{{MIME: "image/png", Data: []byte("png")}}, nil
		},
		Complete: func(exec guiExecutor, imgs []gateway.Image, _ string) (string, error) {
			if exec != guiExecGUI || len(imgs) == 0 {
				t.Fatalf("empty-tree complete exec=%s imgs=%d", exec, len(imgs))
			}
			return `{"x":0.5,"y":0.5,"frameId":"frm_empty"}`, nil
		},
		Click: func(args json.RawMessage, allowPixels bool) (toolruntime.Result, error) {
			allow = allowPixels
			var m map[string]any
			if json.Unmarshal(args, &m) != nil {
				t.Fatal(string(args))
			}
			if m["x"] != float64(100) || m["y"] != float64(50) || m["frameId"] != "frm_empty" {
				t.Fatalf("pixel args %s", args)
			}
			return toolruntime.Result{Output: "clicked"}, nil
		},
	}
	in := guiFallbackIn{ccOn: true, route: RouteR2, guiCatalog: true}
	res, _, used := runGUIFallback(in, rt)
	if !used || res.Output != "clicked" || !allow {
		t.Fatalf("D-D9 used=%v out=%q allowPixels=%v", used, res.Output, allow)
	}
}

func TestRunGUIFallbackSomFailWritesOkFalse(t *testing.T) {
	t.Parallel()
	rt := guiFallbackRuntime{
		Goal:    "点保存",
		FrameID: "frm_1",
		Nodes:   2,
		Images:  []gateway.Image{{MIME: "image/png", Data: []byte("png")}},
		Complete: func(guiExecutor, []gateway.Image, string) (string, error) {
			return "click the save button", nil
		},
		HasHit: func(string) bool { return false },
	}
	res, _, used := runGUIFallback(guiFallbackIn{ccOn: true, route: RouteR2, nodeCount: 2, guiCatalog: true}, rt)
	if !used || !strings.Contains(res.Output, "ok:false") || !strings.Contains(res.Output, "无法执行") {
		t.Fatalf("SoM fail must write back ok:false, used=%v out=%q", used, res.Output)
	}
}

func TestRunGUIFallbackObserveFailIsNone(t *testing.T) {
	t.Parallel()
	var completes int
	rt := guiFallbackRuntime{
		Complete: func(guiExecutor, []gateway.Image, string) (string, error) {
			completes++
			return "", nil
		},
		Observe: func() (string, int, int, int, []gateway.Image, error) {
			return "", 0, 0, 0, nil, errSentinel("observe failed")
		},
	}
	_, _, used := runGUIFallback(guiFallbackIn{ccOn: true, route: RouteR2, guiCatalog: true}, rt)
	if used || completes != 0 {
		t.Fatal("observe failure must stay none")
	}
}
