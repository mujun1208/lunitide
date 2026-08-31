package ccapp

import (
	"errors"
	"os"
	"strings"
	"testing"
)

type ladderStubHost struct {
	title   string
	process string
	nodes   []UINode
}

func (h ladderStubHost) Available() bool                    { return true }
func (h ladderStubHost) ScreenSize() (int, int)             { return 1280, 800 }
func (h ladderStubHost) ScreenOrigin() (int, int)           { return 0, 0 }
func (h ladderStubHost) CursorPosition() (int, int, error)  { return 0, 0, nil }
func (h ladderStubHost) MouseMove(int, int) error           { return nil }
func (h ladderStubHost) MouseClick(string, int) error       { return nil }
func (h ladderStubHost) MouseDrag(int, int, int, int) error { return nil }
func (h ladderStubHost) KeyboardType(string) error          { return nil }
func (h ladderStubHost) KeyboardShortcut([]string) error    { return nil }
func (h ladderStubHost) HoldKey(string, bool) error         { return nil }
func (h ladderStubHost) MouseScroll(int) error              { return nil }
func (h ladderStubHost) MouseScrollH(int) error             { return nil }
func (h ladderStubHost) EnsureForeground() error            { return nil }
func (h ladderStubHost) ScreenCapture() ([]byte, error)     { return nil, nil }
func (h ladderStubHost) WindowCapture(string) ([]byte, int, int, error) {
	return nil, 0, 0, nil
}
func (h ladderStubHost) ActiveWindow() (string, string, error) {
	return h.title, h.process, nil
}
func (h ladderStubHost) ListWindows() ([]WindowInfo, error) { return nil, nil }
func (h ladderStubHost) FocusWindow(string) (WindowInfo, error) {
	return WindowInfo{}, nil
}
func (h ladderStubHost) ObserveDialogs() ([]DialogSnapshot, error) { return nil, nil }
func (h ladderStubHost) ConfirmDialog(string) (DialogSnapshot, error) {
	return DialogSnapshot{}, nil
}
func (h ladderStubHost) ObserveUI(int) ([]UINode, error) {
	return append([]UINode(nil), h.nodes...), nil
}
func (h ladderStubHost) ClipboardGet() (string, error) { return "", nil }
func (h ladderStubHost) ClipboardSet(string) error     { return nil }
func (h ladderStubHost) WindowAction(string, string, int, int, int, int) (WindowInfo, error) {
	return WindowInfo{}, nil
}
func (h ladderStubHost) QuitApp(string) (int, WindowInfo, error) {
	return 0, WindowInfo{}, nil
}
func (h ladderStubHost) MenuClick(string) error        { return nil }
func (h ladderStubHost) SetValue(string, string) error { return nil }
func (h ladderStubHost) InvokeUI(string) error         { return errors.New("invoke refused") }

func TestResolveNamedTargetRefusesUAC(t *testing.T) {
	svc := New(nil)
	svc.SetHost(ladderStubHost{
		title:   "用户账户控制",
		process: "consent.exe",
		nodes:   []UINode{{Name: "是", X: 10, Y: 10, W: 40, H: 20}},
	})
	_, _, _, _, err := svc.resolveNamedTarget("是")
	if !errors.Is(err, ErrCcRiskBlocked) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "uac dialog") || !strings.Contains(err.Error(), UACUserPrompt) {
		t.Fatalf("uac must fail closed with user.ask text, got %v", err)
	}
}

func TestClickNameMatch(t *testing.T) {
	if !clickNameMatch("保存", "保存") || !clickNameMatch("OK", "ok button") {
		t.Fatal("expected match")
	}
	if clickNameMatch("保存", "取消") {
		t.Fatal("mismatch")
	}
	if !clickNameMatch("7", "七") || !clickNameMatch("七", "7") {
		t.Fatal("Chinese calculator digits must match ASCII names")
	}
}

func TestClickAccuracyFixturesSkipWithoutEnv(t *testing.T) {
	if os.Getenv("LUNITIDE_CC_ACCURACY") == "1" {
		t.Skip("live fixtures live in internal/ccapp/accuracy")
	}
	t.Log("set LUNITIDE_CC_ACCURACY=1 to run live Notepad/Calc click fixtures")
}

type nativeStubHost struct {
	ladderStubHost
	screenW, screenH int
	hit              string
	hitErr           error
	movedX, movedY   int
}

func (h *nativeStubHost) ScreenSize() (int, int) {
	if h.screenW > 0 && h.screenH > 0 {
		return h.screenW, h.screenH
	}
	return 2560, 1440
}
func (h *nativeStubHost) MouseMove(x, y int) error { h.movedX, h.movedY = x, y; return nil }
func (h *nativeStubHost) HitTest(int, int) (string, error) {
	return h.hit, h.hitErr
}

func TestRememberCaptureKeepsNativeDesktopAfterThumbnail(t *testing.T) {
	host := &nativeStubHost{screenW: 2560, screenH: 1440}
	svc := New(nil)
	svc.SetHost(host)
	svc.rememberCapture(tinyPNG(t, 256, 144), 0, 0, true)
	svc.capMu.Lock()
	svc.capDeskW, svc.capDeskH = 2560, 1440
	svc.capVisW, svc.capVisH = 256, 144
	svc.capMu.Unlock()
	svc.rememberCapture(tinyPNG(t, 64, 36), 0, 0, true)
	sx, sy := svc.toScreen(32, 18)
	if sx != 1280 || sy != 720 {
		t.Fatalf("layer-4 map through thumbnail = %d,%d want 1280,720", sx, sy)
	}
	if svc.capDeskW != 2560 || svc.capDeskH != 1440 || svc.capVisW != 64 || svc.capVisH != 36 {
		t.Fatalf("capture space desk=%dx%d vis=%dx%d", svc.capDeskW, svc.capDeskH, svc.capVisW, svc.capVisH)
	}
}

func TestClickNamedLadderPixelHitTest(t *testing.T) {
	host := &nativeStubHost{hit: "保存"}
	svc := New(nil)
	svc.SetHost(host)
	if err := svc.clickNamedLadder("保存", 100, 200, "保存"); err != nil {
		t.Fatal(err)
	}
	if host.movedX != 100 || host.movedY != 200 {
		t.Fatalf("native pixel click = %d,%d", host.movedX, host.movedY)
	}
	host.hit = "取消"
	if err := svc.clickNamedLadder("保存", 100, 200, "保存"); err == nil {
		t.Fatal("hit-test mismatch must fail")
	}
}

func TestVerifyPixelClickFailClosed(t *testing.T) {
	host := &nativeStubHost{hitErr: errors.New("no UI node")}
	svc := New(nil)
	svc.SetHost(host)
	if err := svc.verifyPixelClick(10, 10); err == nil {
		t.Fatal("pixel hit-test miss must fail")
	}
}
