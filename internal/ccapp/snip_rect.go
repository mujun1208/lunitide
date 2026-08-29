package ccapp

import "image"

const (
	snipMinSize    = 8
	snipBtnW       = 64
	snipBtnH       = 28
	snipBtnGap     = 8
	snipHintHeight = 36
)

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeSnipRect(x0, y0, x1, y1, maxW, maxH int) (x, y, w, h int, ok bool) {
	if maxW <= 0 || maxH <= 0 {
		return 0, 0, 0, 0, false
	}
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	x = clampInt(x0, 0, maxW)
	y = clampInt(y0, 0, maxH)
	x2 := clampInt(x1, 0, maxW)
	y2 := clampInt(y1, 0, maxH)
	w = x2 - x
	h = y2 - y
	ok = w >= snipMinSize && h >= snipMinSize
	return x, y, w, h, ok
}

func cropBGRA(pixels []byte, fullW, fullH, x, y, w, h int) ([]byte, error) {
	if fullW <= 0 || fullH <= 0 || w <= 0 || h <= 0 {
		return nil, ErrCcExecFailed
	}
	if x < 0 || y < 0 || x+w > fullW || y+h > fullH {
		return nil, ErrCcInputFiltered
	}
	need := fullW * fullH * 4
	if len(pixels) < need {
		return nil, ErrCcExecFailed
	}
	out := make([]byte, w*h*4)
	srcStride := fullW * 4
	dstStride := w * 4
	for row := 0; row < h; row++ {
		src := (y+row)*srcStride + x*4
		copy(out[row*dstStride:(row+1)*dstStride], pixels[src:src+dstStride])
	}
	return out, nil
}

func darkenBGRA(src []byte) []byte {
	out := make([]byte, len(src))
	for i := 0; i+3 < len(src); i += 4 {
		out[i] = src[i] * 2 / 5
		out[i+1] = src[i+1] * 2 / 5
		out[i+2] = src[i+2] * 2 / 5
		out[i+3] = src[i+3]
	}
	return out
}

func ptInRect(px, py, x, y, w, h int) bool {
	return px >= x && py >= y && px < x+w && py < y+h
}

func snipToolbarRects(selX, selY, selW, selH, screenW, screenH int) (done, cancel image.Rectangle) {
	totalW := snipBtnW*2 + snipBtnGap
	x := selX + selW - totalW
	y := selY + selH + snipBtnGap
	if x+totalW > screenW-8 {
		x = screenW - totalW - 8
	}
	if x < 8 {
		x = 8
	}
	if x+totalW > screenW {
		x = screenW - totalW
	}
	if x < 0 {
		x = 0
	}
	if y+snipBtnH > screenH-8 {
		y = selY - snipBtnH - snipBtnGap
	}
	if y < snipHintHeight {
		y = snipHintHeight
	}
	if y+snipBtnH > screenH {
		y = screenH - snipBtnH
	}
	if y < 0 {
		y = 0
	}
	done = image.Rect(x, y, x+snipBtnW, y+snipBtnH)
	cancel = image.Rect(x+snipBtnW+snipBtnGap, y, x+totalW, y+snipBtnH)
	return done, cancel
}
