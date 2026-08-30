//go:build windows

package ccapp

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"sort"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procGetDesktopWindow    = user32.NewProc("GetDesktopWindow")
	procGetWindowDC         = user32.NewProc("GetWindowDC")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

func toU32(n int) uintptr { return uintptr(uint32(int32(n))) }

func (h *windowsHost) ScreenCapture() ([]byte, error) {
	enableDPIAwareness()
	originX, originY, w, ht := virtualScreenRect()
	if w <= 0 || ht <= 0 {
		return nil, errors.New("invalid screen size")
	}
	if png, err := captureMonitorsStitched(originX, originY, w, ht); err == nil && len(png) > 0 {
		return png, nil
	}
	return captureRect(originX, originY, w, ht)
}

func captureRect(originX, originY, w, h int) ([]byte, error) {
	pixels, err := captureRectBGRA(originX, originY, w, h)
	if err != nil {
		return nil, err
	}
	return encodeBGRA(pixels, w, h)
}

func captureRectBGRA(originX, originY, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("invalid capture size")
	}
	var last error
	attempts := []func() ([]byte, error){
		func() ([]byte, error) { return captureDIBSection(originX, originY, w, h, true) },
		func() ([]byte, error) { return captureDIBSection(originX, originY, w, h, false) },
		func() ([]byte, error) { return captureGetDIBits(originX, originY, w, h) },
		func() ([]byte, error) { return captureFromWindowDC(originX, originY, w, h) },
	}
	for _, fn := range attempts {
		pixels, err := fn()
		if err != nil {
			last = err
			continue
		}
		if !bgraHasVisiblePixels(pixels) {
			last = errors.New("capture is blank")
			continue
		}
		return pixels, nil
	}
	if last == nil {
		last = errors.New("capture failed")
	}
	return nil, last
}

func captureDIBSection(originX, originY, w, h int, topDown bool) ([]byte, error) {
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("getdc failed")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("createdc failed")
	}
	defer procDeleteDC.Call(memDC)

	height := int32(h)
	if topDown {
		height = -int32(h)
	}
	var bits uintptr
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      height,
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRgb,
		BiSizeImage:   uint32(w * h * 4),
	}
	bmp := uintptr(0)
	for _, hdc := range []uintptr{0, memDC, screenDC} {
		bits = 0
		bmp, _, _ = procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)),
			dibRgbColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
		if bmp != 0 && bits != 0 {
			break
		}
		if bmp != 0 {
			procDeleteObject.Call(bmp)
			bmp = 0
		}
	}
	if bmp == 0 || bits == 0 {
		return nil, errors.New("createdibsection failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)

	ok := false
	for _, raster := range []uintptr{srccopy, srccopy | captureBlt} {
		if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h),
			screenDC, toU32(originX), toU32(originY), raster); r != 0 {
			ok = true
			break
		}
	}
	pixels := unsafe.Slice((*byte)(ptrFromUintptr(bits)), w*h*4)
	copied := make([]byte, len(pixels))
	copy(copied, pixels)
	_, _, _ = procSelectObject.Call(memDC, old)
	if !ok {
		return nil, errors.New("bitblt failed")
	}
	if !topDown {
		copied = flipBGRARows(copied, w, h)
	}
	return copied, nil
}

func captureGetDIBits(originX, originY, w, h int) ([]byte, error) {
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("getdc failed")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("createdc failed")
	}
	defer procDeleteDC.Call(memDC)
	bmp, _, _ := procCreateCompatibleBmp.Call(screenDC, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, errors.New("createbitmap failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h),
		screenDC, toU32(originX), toU32(originY), srccopy)
	if ok == 0 {
		ok, _, _ = procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h),
			screenDC, toU32(originX), toU32(originY), srccopy|captureBlt)
	}
	_, _, _ = procSelectObject.Call(memDC, old)
	if ok == 0 {
		return nil, errors.New("bitblt failed")
	}
	pixels, err := readDIBBits(screenDC, bmp, w, h)
	if err != nil {
		pixels, err = readDIBBits(memDC, bmp, w, h)
	}
	return pixels, err
}

func captureFromWindowDC(originX, originY, w, h int) ([]byte, error) {
	desktop, _, _ := procGetDesktopWindow.Call()
	if desktop == 0 {
		return nil, errors.New("getdesktopwindow failed")
	}
	winDC, _, _ := procGetWindowDC.Call(desktop)
	if winDC == 0 {
		return nil, errors.New("getwindowdc failed")
	}
	defer procReleaseDC.Call(desktop, winDC)
	memDC, _, _ := procCreateCompatibleDC.Call(winDC)
	if memDC == 0 {
		return nil, errors.New("createdc failed")
	}
	defer procDeleteDC.Call(memDC)
	var bits uintptr
	bmi := bitmapInfoHeader{
		BiSize: uint32(unsafe.Sizeof(bitmapInfoHeader{})), BiWidth: int32(w), BiHeight: -int32(h),
		BiPlanes: 1, BiBitCount: 32, BiCompression: biRgb, BiSizeImage: uint32(w * h * 4),
	}
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)),
		dibRgbColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		return nil, errors.New("createdibsection failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h),
		winDC, toU32(originX), toU32(originY), srccopy)
	pixels := unsafe.Slice((*byte)(ptrFromUintptr(bits)), w*h*4)
	copied := make([]byte, len(pixels))
	copy(copied, pixels)
	_, _, _ = procSelectObject.Call(memDC, old)
	if ok == 0 {
		return nil, errors.New("windowdc bitblt failed")
	}
	return copied, nil
}

func readDIBBits(hdc, bmp uintptr, w, h int) ([]byte, error) {
	row := w * 4
	pixels := make([]byte, row*h)
	try := func(height int32) bool {
		var bmi bitmapInfoHeader
		bmi.BiSize = uint32(unsafe.Sizeof(bmi))
		bmi.BiWidth = int32(w)
		bmi.BiHeight = height
		bmi.BiPlanes = 1
		bmi.BiBitCount = 32
		bmi.BiCompression = biRgb
		bmi.BiSizeImage = uint32(row * h)
		ok, _, _ := procGetDIBits.Call(hdc, bmp, 0, uintptr(h),
			uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bmi)),
			dibRgbColors)
		return ok != 0
	}
	if try(-int32(h)) {
		return pixels, nil
	}
	if try(int32(h)) {
		return flipBGRARows(pixels, w, h), nil
	}
	return nil, errors.New("getdibits failed")
}

func flipBGRARows(pixels []byte, w, h int) []byte {
	row := w * 4
	flipped := make([]byte, len(pixels))
	for y := 0; y < h; y++ {
		copy(flipped[y*row:(y+1)*row], pixels[(h-1-y)*row:(h-y)*row])
	}
	return flipped
}

func encodeBGRA(pixels []byte, w, h int) ([]byte, error) {
	img := bgraToRGBA(pixels, w, h)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func bgraToRGBA(pixels []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	row := w * 4
	for y := 0; y < h; y++ {
		src := pixels[y*row:]
		dst := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			dst[x*4+0] = src[x*4+2]
			dst[x*4+1] = src[x*4+1]
			dst[x*4+2] = src[x*4+0]
			dst[x*4+3] = 0xFF
		}
	}
	return img
}

func captureWindowHWND(hwnd uintptr) ([]byte, int, int, error) {
	x, y, w, h := windowRect(hwnd)
	if w <= 0 || h <= 0 {
		return nil, 0, 0, errors.New("invalid window size")
	}
	if png, err := captureRect(x, y, w, h); err == nil {
		return png, x, y, nil
	}
	png, err := capturePrintWindow(hwnd, w, h)
	if err != nil {
		return nil, 0, 0, err
	}
	return png, x, y, nil
}

func capturePrintWindow(hwnd uintptr, w, h int) ([]byte, error) {
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("getdc failed")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("createdc failed")
	}
	defer procDeleteDC.Call(memDC)
	var bits uintptr
	bmi := bitmapInfoHeader{
		BiSize: uint32(unsafe.Sizeof(bitmapInfoHeader{})), BiWidth: int32(w), BiHeight: -int32(h),
		BiPlanes: 1, BiBitCount: 32, BiCompression: biRgb, BiSizeImage: uint32(w * h * 4),
	}
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)),
		dibRgbColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		return nil, errors.New("createdibsection failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	ok, _, _ := procPrintWindow.Call(hwnd, memDC, uintptr(pwRenderFullContent))
	if ok == 0 {
		_, _, _ = procPrintWindow.Call(hwnd, memDC, 0)
	}
	pixels := unsafe.Slice((*byte)(ptrFromUintptr(bits)), w*h*4)
	copied := make([]byte, len(pixels))
	copy(copied, pixels)
	_, _, _ = procSelectObject.Call(memDC, old)
	if !bgraHasVisiblePixels(copied) {
		return nil, errors.New("printwindow blank")
	}
	return encodeBGRA(copied, w, h)
}

type monitorEnumState struct {
	rects []winRect
}

var (
	monitorEnumMu  sync.Mutex
	monitorEnumSeq uintptr
	monitorEnums   = map[uintptr]*monitorEnumState{}
)

var enumMonitorCallback = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumMonitorProc)
})

func enumMonitorProc(hMonitor, hdcMonitor, lprcMonitor, dwData uintptr) uintptr {
	monitorEnumMu.Lock()
	st := monitorEnums[dwData]
	monitorEnumMu.Unlock()
	if st == nil {
		return 1
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi))); ok != 0 {
		st.rects = append(st.rects, mi.RcMonitor)
		return 1
	}
	if lprcMonitor != 0 {
		st.rects = append(st.rects, *(*winRect)(ptrFromUintptr(lprcMonitor)))
	}
	return 1
}

func listMonitorRects() []winRect {
	monitorEnumMu.Lock()
	monitorEnumSeq++
	token := monitorEnumSeq
	st := &monitorEnumState{}
	monitorEnums[token] = st
	monitorEnumMu.Unlock()
	defer func() {
		monitorEnumMu.Lock()
		delete(monitorEnums, token)
		monitorEnumMu.Unlock()
	}()
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, enumMonitorCallback(), token)
	return st.rects
}

func sortedMonitorRects() []winRect {
	rects := listMonitorRects()
	sort.Slice(rects, func(i, j int) bool {
		if rects[i].Left != rects[j].Left {
			return rects[i].Left < rects[j].Left
		}
		return rects[i].Top < rects[j].Top
	})
	return rects
}

// ScreenIndexAt returns the 1-based monitor index containing (x,y) in
// virtual-desktop pixels, or 0 if the point is off every monitor.
func (h *windowsHost) ScreenIndexAt(x, y int) int {
	for i, r := range sortedMonitorRects() {
		if x >= int(r.Left) && x < int(r.Right) && y >= int(r.Top) && y < int(r.Bottom) {
			return i + 1
		}
	}
	return 0
}

func captureMonitorsStitched(originX, originY, vw, vh int) ([]byte, error) {
	rects := listMonitorRects()
	if len(rects) == 0 {
		return nil, errors.New("no monitors")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, vw, vh))
	got := 0
	for _, r := range rects {
		x, y := int(r.Left), int(r.Top)
		w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
		if w <= 0 || h <= 0 {
			continue
		}
		pixels, err := captureRectBGRA(x, y, w, h)
		if err != nil {
			continue
		}
		img := bgraToRGBA(pixels, w, h)
		dx, dy := x-originX, y-originY
		draw.Draw(canvas, image.Rect(dx, dy, dx+w, dy+h), img, image.Point{}, draw.Src)
		got++
	}
	if got == 0 {
		return nil, errors.New("all monitor captures failed")
	}
	if !rgbaHasVisiblePixels(canvas) {
		return nil, errors.New("stitched capture is blank")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func rgbaHasVisiblePixels(img *image.RGBA) bool {
	if img == nil {
		return false
	}
	n := len(img.Pix)
	if n < 4 {
		return false
	}
	step := 64
	if n < 1024 {
		step = 4
	}
	for i := 0; i+2 < n; i += step {
		if img.Pix[i] != 0 || img.Pix[i+1] != 0 || img.Pix[i+2] != 0 {
			return true
		}
	}
	return false
}
