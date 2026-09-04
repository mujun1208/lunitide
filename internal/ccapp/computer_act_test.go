package ccapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMapComputerActScreenshotAndClick(t *testing.T) {
	tool, raw, err := MapComputerAct([]byte(`{"action":"screenshot","target":"desktop"}`))
	if err != nil || tool != ToolScreenCapture {
		t.Fatalf("screenshot: %s %v", tool, err)
	}
	var cap struct {
		Target string `json:"target"`
	}
	if json.Unmarshal(raw, &cap) != nil || cap.Target != "desktop" {
		t.Fatalf("capture args %s", raw)
	}

	tool, raw, err = MapComputerAct([]byte(`{"action":"screenshot"}`))
	if err != nil || tool != ToolScreenCapture {
		t.Fatalf("default screenshot: %s %v", tool, err)
	}
	if json.Unmarshal(raw, &cap) != nil || cap.Target != "foreground" {
		t.Fatalf("empty target must default to foreground, got %s", raw)
	}

	tool, raw, err = MapComputerAct([]byte(`{"action":"click","x":12,"y":34,"frameId":"frm_abc"}`))
	if err != nil || tool != ToolMouseClick {
		t.Fatalf("click: %s %v", tool, err)
	}
	var click struct {
		X         int      `json:"x"`
		Y         int      `json:"y"`
		FrameID   string   `json:"frameId"`
		Button    string   `json:"button"`
		Clicks    int      `json:"clicks"`
		Modifiers []string `json:"modifiers"`
	}
	if json.Unmarshal(raw, &click) != nil || click.X != 12 || click.Y != 34 || click.FrameID != "frm_abc" || click.Button != "left" || click.Clicks != 1 {
		t.Fatalf("click args %s", raw)
	}

	tool, raw, err = MapComputerAct([]byte(`{"action":"click","x":1,"y":2,"frameId":"frm_abc","modifiers":["ctrl","shift"]}`))
	if err != nil || tool != ToolMouseClick {
		t.Fatalf("mod click: %s %v", tool, err)
	}
	if json.Unmarshal(raw, &click) != nil || len(click.Modifiers) != 2 || click.Modifiers[0] != "ctrl" || click.Modifiers[1] != "shift" {
		t.Fatalf("mod click args %s", raw)
	}
}

func TestMapComputerActDoubleClickAndKey(t *testing.T) {
	tool, raw, err := MapComputerAct([]byte(`{"action":"double_click","id":"B1","frameId":"frm_1"}`))
	if err != nil || tool != ToolMouseClick {
		t.Fatalf("dbl: %s %v", tool, err)
	}
	var click struct {
		ID     string `json:"id"`
		Clicks int    `json:"clicks"`
	}
	if json.Unmarshal(raw, &click) != nil || click.ID != "B1" || click.Clicks != 2 {
		t.Fatalf("dbl args %s", raw)
	}

	tool, _, err = MapComputerAct([]byte(`{"action":"key","keys":["ctrl","s"]}`))
	if err != nil || tool != ToolKeyboardShortcut {
		t.Fatalf("hotkey: %s %v", tool, err)
	}
	tool, _, err = MapComputerAct([]byte(`{"action":"key","key":"enter"}`))
	if err != nil || tool != ToolPress {
		t.Fatalf("press: %s %v", tool, err)
	}

	tool, raw, err = MapComputerAct([]byte(`{"action":"hold_key","key":"shift"}`))
	if err != nil || tool != ToolPress {
		t.Fatalf("hold_key: %s %v", tool, err)
	}
	var hold struct {
		Key  string `json:"key"`
		Hold string `json:"hold"`
	}
	if json.Unmarshal(raw, &hold) != nil || hold.Key != "shift" || hold.Hold != "down" {
		t.Fatalf("hold args %s", raw)
	}
	tool, raw, err = MapComputerAct([]byte(`{"action":"key_up","key":"shift"}`))
	if err != nil || tool != ToolPress {
		t.Fatalf("key_up: %s %v", tool, err)
	}
	if json.Unmarshal(raw, &hold) != nil || hold.Hold != "up" {
		t.Fatalf("key_up args %s", raw)
	}
	_ = raw
}

func TestMapComputerActListIsWindowList(t *testing.T) {
	tool, _, err := MapComputerAct([]byte(`{"action":"list"}`))
	if err != nil || tool != ToolWindowList {
		t.Fatalf("D-B4 list must map to window list, got %s %v", tool, err)
	}
}

func TestMapComputerActUnknownAction(t *testing.T) {
	if _, _, err := MapComputerAct([]byte(`{"action":"explode"}`)); !errors.Is(err, ErrCcInputFiltered) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := MapComputerAct([]byte(`{}`)); !errors.Is(err, ErrCcInputFiltered) {
		t.Fatalf("empty action: %v", err)
	}
}

func TestFrameIDFromHashStable(t *testing.T) {
	var a, b [32]byte
	a[0], a[1] = 0xab, 0xcd
	b[0], b[1] = 0xab, 0xcd
	if FrameIDFromHash(a) != FrameIDFromHash(b) || FrameIDFromHash(a) == "" {
		t.Fatalf("stable id %q %q", FrameIDFromHash(a), FrameIDFromHash(b))
	}
	if !strings.HasSuffix(FrameIDFromHash(a), "s0") {
		t.Fatalf("pixel-only id must carry screenIndex 0, got %q", FrameIDFromHash(a))
	}
	b[2] = 1
	if FrameIDFromHash(a) == FrameIDFromHash(b) {
		t.Fatal("different pixels must mint a new frameId")
	}
	samePix := a
	g1 := DisplayGeometry{Width: 1920, Height: 1080, Screens: 1, ScreenIndex: 1}
	g2 := DisplayGeometry{Width: 1920, Height: 1080, Screens: 2, OriginX: -1920, ScreenIndex: 2}
	if FrameIDFromCapture(samePix, g1) == FrameIDFromCapture(samePix, g2) {
		t.Fatal("topology must bind into frameId")
	}
	if !strings.HasSuffix(FrameIDFromCapture(samePix, g2), "s2") {
		t.Fatalf("window shot must suffix screenIndex, got %q", FrameIDFromCapture(samePix, g2))
	}
}
