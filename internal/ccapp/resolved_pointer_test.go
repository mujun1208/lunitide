package ccapp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type semanticClickHost struct {
	nativeStubHost
	button               string
	clicks               int
	invokes              int
	mods                 []string
	clickErr             error
	moveTargetAfterClick bool
	originX              int
}

func (h *semanticClickHost) ScreenOrigin() (int, int) { return h.originX, 0 }
func (h *semanticClickHost) InvokeUI(string) error {
	h.invokes++
	if h.moveTargetAfterClick {
		h.hit = "新页面"
	}
	return nil
}
func (h *semanticClickHost) MouseClick(button string, clicks int) error {
	h.button, h.clicks = button, clicks
	if h.moveTargetAfterClick {
		h.hit = "新页面"
	}
	return h.clickErr
}
func (h *semanticClickHost) HoldKey(key string, down bool) error {
	h.mods = append(h.mods, fmt.Sprintf("%s:%v", key, down))
	return nil
}

func TestObservedClickPreservesButtonCountModifiersAndNodeIdentity(t *testing.T) {
	for _, tc := range []struct {
		label, args, button string
		clicks              int
		mods                string
	}{
		{"right", `{"id":"B2","button":"right"}`, "right", 1, ""},
		{"double", `{"id":"B2","clicks":2}`, "left", 2, ""},
		{"ctrl", `{"id":"B2","modifiers":["ctrl"]}`, "left", 1, "ctrl:true,ctrl:false"},
		{"same-name-ID", `{"id":"B2"}`, "left", 1, ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			h := &semanticClickHost{nativeStubHost: nativeStubHost{ladderStubHost: ladderStubHost{title: "Test editor", process: "notepad.exe"}, hit: "打开"}}
			s := New(nil)
			s.SetHost(h)
			s.rememberHits([]UINode{{ID: "B1", Name: "打开", X: 10, Y: 10, W: 20, H: 20}, {ID: "B2", Name: "打开", X: 210, Y: 30, W: 20, H: 20}})
			_, _, err := s.runHost(ToolMouseClick, []byte(tc.args), nil)
			if err != nil {
				t.Fatal(err)
			}
			if h.invokes != 0 || h.button != tc.button || h.clicks != tc.clicks || h.movedX != 220 || h.movedY != 40 || strings.Join(h.mods, ",") != tc.mods {
				t.Fatalf("wrong gesture/target: %+v", h)
			}
		})
	}
}

func TestResolvedClickChecksBeforeMutationAndSupportsNegativeMonitorOrigin(t *testing.T) {
	h := &semanticClickHost{nativeStubHost: nativeStubHost{ladderStubHost: ladderStubHost{title: "Test editor", process: "notepad.exe"}, hit: "打开"}, originX: -1280, moveTargetAfterClick: true}
	s := New(nil)
	s.SetHost(h)
	if err := s.clickResolvedPointer(-300, 20, "打开", "right", 1, nil); err != nil {
		t.Fatal(err)
	}
	if h.movedX != -300 || h.button != "right" {
		t.Fatalf("negative monitor target lost: %+v", h)
	}
	// Successful activation changes the UI. It must not fail by checking for
	// the now-removed original node after the action has already happened.
	h.hit = "打开"
	if err := s.clickNamedLadder("打开", 100, 100, "打开"); err != nil {
		t.Fatalf("successful activation was falsely failed: %v", err)
	}
}

func TestResolvedClickRefusesWrongHitAndReleasesModifiersOnError(t *testing.T) {
	h := &semanticClickHost{nativeStubHost: nativeStubHost{ladderStubHost: ladderStubHost{title: "Test editor", process: "notepad.exe"}, hit: "取消"}}
	s := New(nil)
	s.SetHost(h)
	if err := s.clickResolvedPointer(20, 20, "保存", "left", 1, nil); err == nil || h.clicks != 0 {
		t.Fatal("wrong target was clicked")
	}
	h.hit = "保存"
	h.clickErr = errors.New("isolated click failed")
	if err := s.clickResolvedPointer(20, 20, "保存", "left", 2, []string{"ctrl", "shift"}); err == nil {
		t.Fatal("click error hidden")
	}
	if got := strings.Join(h.mods, ","); got != "ctrl:true,shift:true,shift:false,ctrl:false" {
		t.Fatalf("modifiers remained held: %s", got)
	}
}
