package ccapp

import (
	"bytes"
	"image/png"
)

// MapCapturePoint converts a point in the last vision screenshot into
// virtual-desktop pixels. Identity when no capture has been recorded or the
// vision image is 1:1 with the desktop.
func MapCapturePoint(x, y, visW, visH, deskW, deskH int) (int, int) {
	if visW <= 0 || visH <= 0 || deskW <= 0 || deskH <= 0 {
		return x, y
	}
	if visW == deskW && visH == deskH {
		return x, y
	}
	return (x*deskW + visW/2) / visW, (y*deskH + visH/2) / visH
}

// MapScreenToVision converts a SetCursorPos screen point into the last
// vision-image pixel space (inverse of MapCapturePoint plus capture origin).
func MapScreenToVision(sx, sy, originX, originY, visW, visH, deskW, deskH int) (int, int) {
	lx, ly := sx-originX, sy-originY
	if visW <= 0 || visH <= 0 || deskW <= 0 || deskH <= 0 || (visW == deskW && visH == deskH) {
		return lx, ly
	}
	return (lx*visW + deskW/2) / deskW, (ly*visH + deskH/2) / deskH
}

// ProjectRect maps a screen-space rectangle into vision-image pixels when a
// capture exists, otherwise origin-relative desktop pixels (the same space
// cc.mouse_move uses before the first capture).
func ProjectRect(x, y, w, h, originX, originY, visW, visH, deskW, deskH int) (int, int, int, int) {
	x2, y2 := x+w, y+h
	x, y = MapScreenToVision(x, y, originX, originY, visW, visH, deskW, deskH)
	x2, y2 = MapScreenToVision(x2, y2, originX, originY, visW, visH, deskW, deskH)
	w, h = x2-x, y2-y
	if w < 0 {
		w = -w
	}
	if h < 0 {
		h = -h
	}
	return x, y, w, h
}

func visionDimensions(pngBytes []byte) (deskW, deskH, visW, visH int) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, 0, 0
	}
	deskW, deskH = cfg.Width, cfg.Height
	visW, visH = deskW, deskH
	if len(pngBytes) <= visionMaxBytes {
		return
	}
	visW, visH = scaledSize(deskW, deskH, visionMaxEdge)
	return
}

func scaledSize(w, h, maxEdge int) (int, int) {
	if w <= 0 || h <= 0 || (w <= maxEdge && h <= maxEdge) {
		return w, h
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return nw, nh
}
