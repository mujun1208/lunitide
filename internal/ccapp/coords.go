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
