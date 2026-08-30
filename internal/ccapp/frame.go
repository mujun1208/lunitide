package ccapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// DisplayGeometry is the virtual-desktop topology at capture time.
// ScreenIndex is 0 for a full virtual-desktop shot and 1..N (left-to-right,
// then top-to-bottom) for a window shot on that monitor.
type DisplayGeometry struct {
	OriginX, OriginY int
	Width, Height    int
	Screens          int
	ScreenIndex      int
}

func (g DisplayGeometry) Signature() string {
	return g.topology() + fmt.Sprintf(" i%d", g.ScreenIndex)
}

func (g DisplayGeometry) topology() string {
	screens := g.Screens
	if screens < 1 {
		screens = 1
	}
	return fmt.Sprintf("%d,%d %dx%d n%d", g.OriginX, g.OriginY, g.Width, g.Height, screens)
}

func (g DisplayGeometry) TopologyEqual(o DisplayGeometry) bool {
	gs, os := g.Screens, o.Screens
	if gs < 1 {
		gs = 1
	}
	if os < 1 {
		os = 1
	}
	return g.OriginX == o.OriginX && g.OriginY == o.OriginY && g.Width == o.Width && g.Height == o.Height && gs == os
}

// FrameIDFromHash binds a screenshot to a stable id. Identical pixels keep
// the same frameId (OpenClaw: an unchanged verify frame is still current).
func FrameIDFromHash(sum [32]byte) string {
	return FrameIDFromCapture(sum, DisplayGeometry{})
}

// FrameIDFromCapture mixes pixel identity with display topology so a
// reconnect or DPI change mints a new id even when the pixels look similar.
// The sN suffix is the OpenClaw-shaped screenIndex (0 = virtual desktop).
func FrameIDFromCapture(sum [32]byte, geom DisplayGeometry) string {
	h := sha256.New()
	_, _ = h.Write(sum[:])
	_, _ = h.Write([]byte(geom.Signature()))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return "frm_" + hex.EncodeToString(out[:6]) + fmt.Sprintf("s%d", geom.ScreenIndex)
}

// CurrentFrameID is the latest capture/observe/verify frame. Tests and
// computer.act echo this on coordinate actions.
func (s *Service) CurrentFrameID() string {
	if s == nil {
		return ""
	}
	s.capMu.Lock()
	defer s.capMu.Unlock()
	return s.capFrameID
}

// requireFrameID fails closed when a capture exists and the caller did not
// echo that id (COMPUTER_STALE_FRAME). Live topology must still match the
// capture (reconnect, unplug, DPI). No capture yet: coordinates stay in
// virtual-desktop space, matching the pre-screenshot mouse_move contract.
func (s *Service) requireFrameID(got string) error {
	if s == nil {
		return nil
	}
	s.capMu.Lock()
	current := s.capFrameID
	stored := s.capGeom
	s.capMu.Unlock()
	if current == "" {
		return nil
	}
	if stored.Width > 0 && stored.Height > 0 && s.host != nil {
		live := snapshotTopology(s.host)
		if !stored.TopologyEqual(live) {
			return fmt.Errorf("%w: COMPUTER_STALE_FRAME: display topology changed %s → %s — screenshot again", ErrCcInputFiltered, stored.topology(), live.topology())
		}
	}
	got = strings.TrimSpace(got)
	if got == "" {
		return fmt.Errorf("%w: COMPUTER_STALE_FRAME: echo frameId %s from the latest screenshot", ErrCcInputFiltered, current)
	}
	if !strings.EqualFold(got, current) {
		return fmt.Errorf("%w: COMPUTER_STALE_FRAME: coordinates referenced %s, current is %s — screenshot again", ErrCcInputFiltered, got, current)
	}
	return nil
}

func appendFrameID(summary, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return summary
	}
	if strings.Contains(summary, "frameId=") {
		return summary
	}
	return summary + "; frameId=" + id
}

type screenCounter interface {
	ScreenCount() int
}

type screenIndexer interface {
	ScreenIndexAt(x, y int) int
}

func hostScreenCount(h Host) int {
	if c, ok := h.(screenCounter); ok {
		if n := c.ScreenCount(); n > 0 {
			return n
		}
	}
	return 1
}

func snapshotTopology(h Host) DisplayGeometry {
	if h == nil {
		return DisplayGeometry{Screens: 1}
	}
	w, ht := h.ScreenSize()
	ox, oy := h.ScreenOrigin()
	return DisplayGeometry{OriginX: ox, OriginY: oy, Width: w, Height: ht, Screens: hostScreenCount(h)}
}

func geometryForCapture(h Host, originX, originY, imgW, imgH int, wide bool) DisplayGeometry {
	g := snapshotTopology(h)
	if wide {
		g.ScreenIndex = 0
		return g
	}
	cx, cy := originX, originY
	if imgW > 0 {
		cx += imgW / 2
	}
	if imgH > 0 {
		cy += imgH / 2
	}
	if idx, ok := h.(screenIndexer); ok {
		g.ScreenIndex = idx.ScreenIndexAt(cx, cy)
		return g
	}
	if g.Screens <= 1 {
		g.ScreenIndex = 1
	}
	return g
}
