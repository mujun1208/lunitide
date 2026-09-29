package ccapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPreferPasteText(t *testing.T) {
	t.Parallel()
	if PreferPasteText("hello") || PreferPasteText("short latin") {
		t.Fatal("short latin must type")
	}
	if !PreferPasteText("你好世界") {
		t.Fatal("D-O1 Han must paste")
	}
	if !PreferPasteText(strings.Repeat("a", 17)) {
		t.Fatal("rune>16 must paste")
	}
}

func TestUnnamedUIName(t *testing.T) {
	t.Parallel()
	if !unnamedUIName("button (unnamed)") || unnamedUIName("保存") {
		t.Fatal("unnamed detector")
	}
}

func TestRequireObserveBeforeXY(t *testing.T) {
	t.Parallel()
	s := New(nil)
	if err := s.requireObserveBeforeXY(); !errors.Is(err, ErrCcInputFiltered) {
		t.Fatalf("D-I1 no observe: %v", err)
	}
	s.rememberHits(assignNodeIDs([]UINode{
		{Role: "button", Name: "保存", X: 10, Y: 10, W: 40, H: 20},
	}))
	s.observedFrameID = "frm_nodes"
	if err := s.requireObserveBeforeXY(); err == nil {
		t.Fatal("nodes>0 must forbid raw xy")
	}
	s.observedCount = 0
	s.observedFrameID = "frm_empty"
	s.allowGUIPixels = true
	if err := s.requireObserveBeforeXY(); err != nil {
		t.Fatalf("empty tree + GUI: %v", err)
	}
}

func TestSetAllowGUIPixelsProduction(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.observedFrameID = "frm_empty"
	s.observedCount = 0
	if err := s.requireObserveBeforeXY(); err == nil {
		t.Fatal("false: empty-tree xy must still fail")
	}
	s.SetAllowGUIPixels(true)
	if err := s.requireObserveBeforeXY(); err != nil {
		t.Fatalf("true: empty-tree xy must pass: %v", err)
	}
	s.SetAllowGUIPixels(false)
	if err := s.requireObserveBeforeXY(); err == nil {
		t.Fatal("after close: empty-tree xy must fail again")
	}
	frame, n := s.ObservedSnapshot()
	if frame != "frm_empty" || n != 0 {
		t.Fatalf("ObservedSnapshot = %q %d", frame, n)
	}
}

func TestObserveTruncatedFlags(t *testing.T) {
	t.Parallel()
	nodes := []UINode{
		{Role: "button", Name: "A", X: 1, Y: 1, W: 10, H: 10},
		{Role: "button", Name: "B", X: 20, Y: 1, W: 10, H: 10},
	}
	full, _ := json.Marshal(observeUIPayload(nodes, 2, "frm_x"))
	var payload struct {
		Truncated bool `json:"truncated"`
		MaxNodes  int  `json:"maxNodes"`
		Returned  int  `json:"returned"`
	}
	if json.Unmarshal(full, &payload) != nil {
		t.Fatal(string(full))
	}
	if !payload.Truncated || payload.MaxNodes != 2 || payload.Returned != 2 {
		t.Fatalf("D-K1 %+v", payload)
	}
	under, _ := json.Marshal(observeUIPayload(nodes, 8, "frm_x"))
	if json.Unmarshal(under, &payload) != nil {
		t.Fatal(string(under))
	}
	if payload.Truncated || payload.Returned != 2 {
		t.Fatalf("D-K2 %+v", payload)
	}
}

func TestResolveNamedAmbiguousUnnamed(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetHost(ladderStubHost{
		nodes: []UINode{
			{Role: "button", Name: "button (unnamed)", X: 10, Y: 10, W: 20, H: 20},
			{Role: "button", Name: "button (unnamed)", X: 80, Y: 10, W: 20, H: 20},
		},
	})
	_, _, _, _, err := s.resolveNamedTarget("button (unnamed)")
	if err == nil || !strings.Contains(err.Error(), "B1") || !strings.Contains(err.Error(), "B2") {
		t.Fatalf("D-J1 want candidate ids, got %v", err)
	}
}

func TestLookupHitAfterObserve(t *testing.T) {
	t.Parallel()
	s := New(nil)
	nodes := assignNodeIDs([]UINode{
		{Role: "button", Name: "button (unnamed)", X: 10, Y: 10, W: 20, H: 20},
		{Role: "button", Name: "button (unnamed)", X: 80, Y: 10, W: 20, H: 20},
	})
	s.rememberHits(nodes)
	if _, ok := s.lookupHit("B2"); !ok {
		t.Fatal("D-I2 rememberHits must keep B2")
	}
	_, _, _, _, err := s.resolveNamedTarget("B9")
	if err == nil || !strings.Contains(err.Error(), "先 observe") {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestClickIDB2AfterAmbiguousUnnamed(t *testing.T) {
	nodes := []UINode{
		{Role: "button", Name: "button (unnamed)", X: 10, Y: 10, W: 20, H: 20},
		{Role: "button", Name: "button (unnamed)", X: 80, Y: 10, W: 20, H: 20},
	}
	host := &nativeStubHost{
		ladderStubHost: ladderStubHost{title: "Notepad", process: "notepad.exe", nodes: nodes},
		hit:            "button (unnamed)",
	}
	svc := New(nil)
	svc.SetHost(host)
	svc.rememberHits(assignNodeIDs(nodes))
	_, _, _, _, err := svc.resolveNamedTarget("button (unnamed)")
	if err == nil || !strings.Contains(err.Error(), "B1") || !strings.Contains(err.Error(), "B2") {
		t.Fatalf("D-J1 name=: %v", err)
	}
	invokeName, sx, sy, hit, err := svc.resolveNamedTarget("B2")
	if err != nil {
		t.Fatalf("D-J1 id=B2 resolve: %v", err)
	}
	if err := svc.clickNamedLadder(invokeName, sx, sy, hit); err != nil {
		t.Fatalf("D-J1 id=B2 click: %v", err)
	}
	if host.movedX != 90 || host.movedY != 20 {
		t.Fatalf("D-J1 id=B2 must land on B2, got %d,%d", host.movedX, host.movedY)
	}
}

type dragStubHost struct {
	ladderStubHost
	x1, y1, x2, y2 int
}

func (h *dragStubHost) MouseDrag(x1, y1, x2, y2 int) error {
	h.x1, h.y1, h.x2, h.y2 = x1, y1, x2, y2
	return nil
}

func TestDragByIDRequiresObserve(t *testing.T) {
	t.Parallel()
	host := &dragStubHost{ladderStubHost: ladderStubHost{title: "Notepad", process: "notepad.exe"}}
	s := New(nil)
	s.SetHost(host)
	_, _, err := s.runHost(ToolMouseDrag, []byte(`{"id":"B1","id2":"B2"}`), nil)
	if err == nil || !errors.Is(err, ErrCcInputFiltered) || !strings.Contains(err.Error(), "先 observe") {
		t.Fatalf("D-Q1 want observe miss, got %v", err)
	}
}

type scrollObserveHost struct {
	nativeStubHost
	png []byte
}

func (h *scrollObserveHost) ScreenCapture() ([]byte, error) { return h.png, nil }

func (h *scrollObserveHost) MouseScroll(n int) error {
	h.nodes = []UINode{{Role: "button", Name: "提交", X: 200, Y: 80, W: 40, H: 20}}
	return nil
}

func (h *scrollObserveHost) ObserveUI(int) ([]UINode, error) {
	return append([]UINode(nil), h.nodes...), nil
}

func TestScrollThenObserveRefreshesHits(t *testing.T) {
	t.Parallel()
	host := &scrollObserveHost{
		nativeStubHost: nativeStubHost{
			ladderStubHost: ladderStubHost{
				title: "Notepad", process: "notepad.exe",
				nodes: []UINode{{Role: "button", Name: "取消", X: 10, Y: 10, W: 20, H: 20}},
			},
			hit: "提交",
		},
		png: tinyPNG(t, 64, 36),
	}
	s := New(nil)
	s.SetHost(host)
	tool, raw, err := MapComputerAct([]byte(`{"action":"observe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.runHost(tool, raw, nil); err != nil {
		t.Fatalf("first observe: %v", err)
	}
	first, ok := s.lookupHit("B1")
	if !ok {
		t.Fatal("first observe must remember B1")
	}
	tool, raw, err = MapComputerAct([]byte(`{"action":"scroll","scroll":-3}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.runHost(tool, raw, nil); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	tool, raw, err = MapComputerAct([]byte(`{"action":"observe"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.runHost(tool, raw, nil); err != nil {
		t.Fatalf("second observe: %v", err)
	}
	second, ok := s.lookupHit("B1")
	if !ok {
		t.Fatal("second observe must remember B1")
	}
	if second.SX == first.SX && second.SY == first.SY {
		t.Fatal("scroll then observe must refresh SoM hits")
	}
	if second.SX != 220 || second.SY != 90 {
		t.Fatalf("post-scroll B1 = %+v", second)
	}
}

func TestDragByIDUsesRememberedHits(t *testing.T) {
	t.Parallel()
	nodes := assignNodeIDs([]UINode{
		{Role: "button", Name: "A", X: 10, Y: 10, W: 20, H: 20},
		{Role: "button", Name: "B", X: 80, Y: 10, W: 20, H: 20},
	})
	host := &dragStubHost{ladderStubHost: ladderStubHost{title: "Notepad", process: "notepad.exe", nodes: nodes}}
	s := New(nil)
	s.SetHost(host)
	s.rememberHits(nodes)
	s.observedFrameID = "frm_1"
	_, _, err := s.runHost(ToolMouseDrag, []byte(`{"id":"B1","id2":"B2"}`), nil)
	if err != nil {
		t.Fatalf("D-Q2 drag: %v", err)
	}
	if host.x1 != 20 || host.y1 != 20 || host.x2 != 90 || host.y2 != 20 {
		t.Fatalf("D-Q2 drag coords %d,%d -> %d,%d", host.x1, host.y1, host.x2, host.y2)
	}
}

func TestClickNamedLadderSkipsInvokeForUnnamed(t *testing.T) {
	host := &invokeWatchHost{
		nativeStubHost: nativeStubHost{
			ladderStubHost: ladderStubHost{title: "Notepad", process: "notepad.exe"},
			hit:            "button (unnamed)",
		},
	}
	svc := New(nil)
	svc.SetHost(host)
	if err := svc.clickNamedLadder("button (unnamed)", 80, 20, "button (unnamed)"); err != nil {
		t.Fatal(err)
	}
	if len(host.invokes) != 0 {
		t.Fatalf("D-J3 InvokeUI must not run for unnamed, got %v", host.invokes)
	}
}

type invokeWatchHost struct {
	nativeStubHost
	invokes []string
}

func (h *invokeWatchHost) InvokeUI(name string) error {
	h.invokes = append(h.invokes, name)
	return nil
}

func TestPixelClickRefusesLunitideForeground(t *testing.T) {
	host := &nativeStubHost{
		ladderStubHost: ladderStubHost{title: "月伴对话 - Lunitide", process: "lunitide.exe"},
		hit:            "保存",
	}
	svc := New(nil)
	svc.SetHost(host)
	if err := svc.clickNamedLadder("保存", 100, 200, "保存"); err == nil || !strings.Contains(err.Error(), "Lunitide") {
		t.Fatalf("D-B5: %v", err)
	}
}
