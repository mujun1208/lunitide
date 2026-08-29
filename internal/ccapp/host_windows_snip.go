//go:build windows

package ccapp

import (
	"errors"
	"image"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	snipClassName = "LunitideRegionSnip"

	wsPopup         = 0x80000000
	wsVisible       = 0x10000000
	wsExTopmost     = 0x00000008
	wsExToolwindow  = 0x00000080
	csHRedraw       = 0x0002
	csVRedraw       = 0x0001
	csDblClks       = 0x0008
	idcCross        = 32515
	hwndTopmost     = ^uintptr(0)
	swpShowWindow   = 0x0040
	dtCenter        = 0x00000001
	dtVCenter       = 0x00000004
	dtSingleLine    = 0x00000020
	transparentMode = 1
	defaultGUIFont  = 17
	snipMaxWait     = 170 * time.Second

	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmSetCursor     = 0x0020
	wmKeyDown       = 0x0100
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	vkEscape        = 0x1B
	vkReturn        = 0x0D
)

type snipMSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type snipPaint struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     winRect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type wndClassExW struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type snipOverlay struct {
	hwnd              uintptr
	w, h              int
	pixels            []byte
	lightBmp, darkBmp uintptr
	memDC             uintptr
	cross             uintptr
	dragging, haveSel bool
	x0, y0, x1, y1    int
	result            []byte
	err               error
	finished          bool
}

var (
	snipSessionMu sync.Mutex
	activeSnip    *snipOverlay

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procSetCapture       = user32.NewProc("SetCapture")
	procReleaseCapture   = user32.NewProc("ReleaseCapture")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procSetCursor        = user32.NewProc("SetCursor")
	procFillRect         = user32.NewProc("FillRect")
	procFrameRect        = user32.NewProc("FrameRect")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procSelectObjectGDI  = procSelectObject
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procUpdateWindow     = user32.NewProc("UpdateWindow")

	snipClassOnce sync.Once
	snipClassOK   bool
	snipWndProc   = sync.OnceValue(func() uintptr {
		return syscall.NewCallback(snipWindowProc)
	})
)

func (h *windowsHost) RegionCapture() ([]byte, error) {
	if !snipSessionMu.TryLock() {
		return nil, errors.New("a region capture is already open")
	}
	defer snipSessionMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return interactiveRegionCapture()
}

func interactiveRegionCapture() ([]byte, error) {
	enableDPIAwareness()
	originX, originY, w, ht := virtualScreenRect()
	if w <= 0 || ht <= 0 {
		return nil, errors.New("invalid screen size")
	}
	pixels, err := captureRectBGRA(originX, originY, w, ht)
	if err != nil {
		return nil, err
	}
	light, err := dibSectionFromBGRA(pixels, w, ht)
	if err != nil {
		return nil, err
	}
	dark, err := dibSectionFromBGRA(darkenBGRA(pixels), w, ht)
	if err != nil {
		procDeleteObject.Call(light)
		return nil, err
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		procDeleteObject.Call(light)
		procDeleteObject.Call(dark)
		return nil, errors.New("getdc failed")
	}
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	procReleaseDC.Call(0, screenDC)
	if memDC == 0 {
		procDeleteObject.Call(light)
		procDeleteObject.Call(dark)
		return nil, errors.New("createdc failed")
	}
	cross, _, _ := procLoadCursorW.Call(0, uintptr(idcCross))
	overlay := &snipOverlay{
		w:        w,
		h:        ht,
		pixels:   pixels,
		lightBmp: light,
		darkBmp:  dark,
		memDC:    memDC,
		cross:    cross,
	}
	activeSnip = overlay
	defer func() {
		activeSnip = nil
		if overlay.memDC != 0 {
			procDeleteDC.Call(overlay.memDC)
		}
		if overlay.lightBmp != 0 {
			procDeleteObject.Call(overlay.lightBmp)
		}
		if overlay.darkBmp != 0 {
			procDeleteObject.Call(overlay.darkBmp)
		}
	}()
	if err := registerSnipClass(); err != nil {
		return nil, err
	}
	instance, _, _ := procGetModuleHandleW.Call(0)
	title, _ := windows.UTF16PtrFromString("Lunitide screenshot")
	className, _ := windows.UTF16PtrFromString(snipClassName)
	ex := uintptr(wsExTopmost | wsExToolwindow)
	style := uintptr(wsPopup | wsVisible)
	hwnd, _, _ := procCreateWindowExW.Call(
		ex,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		toU32(originX), toU32(originY), uintptr(w), uintptr(ht),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		return nil, errors.New("unable to open screenshot overlay")
	}
	overlay.hwnd = hwnd
	procSetWindowPos.Call(hwnd, hwndTopmost, toU32(originX), toU32(originY), uintptr(w), uintptr(ht), swpShowWindow)
	procShowWindow.Call(hwnd, uintptr(swShow))
	procUpdateWindow.Call(hwnd)
	_, _, _ = procSetForeground.Call(hwnd)
	_, _, _ = procSetFocus.Call(hwnd)

	timer := time.AfterFunc(snipMaxWait, func() {
		if overlay.hwnd != 0 {
			procPostMessageW.Call(overlay.hwnd, wmClose, 0, 0)
		}
	})
	defer timer.Stop()

	for {
		var m snipMSG
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		code := int32(r)
		if code == 0 {
			break
		}
		if code == -1 {
			return nil, errors.New("screenshot overlay message loop failed")
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if overlay.err != nil {
		return nil, overlay.err
	}
	if len(overlay.result) == 0 {
		return nil, ErrCaptureCanceled
	}
	return overlay.result, nil
}

func registerSnipClass() error {
	var err error
	snipClassOnce.Do(func() {
		instance, _, _ := procGetModuleHandleW.Call(0)
		className, convErr := windows.UTF16PtrFromString(snipClassName)
		if convErr != nil {
			err = convErr
			return
		}
		cross, _, _ := procLoadCursorW.Call(0, uintptr(idcCross))
		wc := wndClassExW{
			CbSize:    uint32(unsafe.Sizeof(wndClassExW{})),
			Style:     csHRedraw | csVRedraw | csDblClks,
			WndProc:   snipWndProc(),
			Instance:  instance,
			Cursor:    cross,
			ClassName: className,
		}
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			if errno, ok := callErr.(syscall.Errno); ok && errno == 1410 {
				snipClassOK = true
				return
			}
			err = errors.New("register screenshot class failed")
			return
		}
		snipClassOK = true
	})
	if err != nil {
		return err
	}
	if !snipClassOK {
		return errors.New("register screenshot class failed")
	}
	return nil
}

func snipWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	s := activeSnip
	if s == nil {
		return snipDef(hwnd, msg, wParam, lParam)
	}
	if s.hwnd == 0 {
		s.hwnd = hwnd
	}
	switch msg {
	case wmEraseBkgnd:
		return 1
	case wmSetCursor:
		if s.cross != 0 {
			procSetCursor.Call(s.cross)
			return 1
		}
	case wmPaint:
		s.paint(hwnd)
		return 0
	case wmLButtonDown:
		s.onLeftDown(clientXY(lParam))
		return 0
	case wmMouseMove:
		s.onMove(clientXY(lParam))
		return 0
	case wmLButtonUp:
		s.onLeftUp(clientXY(lParam))
		return 0
	case wmLButtonDblClk:
		s.confirm()
		return 0
	case wmRButtonUp:
		s.finish(nil, ErrCaptureCanceled)
		return 0
	case wmKeyDown:
		if wParam == vkEscape {
			s.finish(nil, ErrCaptureCanceled)
			return 0
		}
		if wParam == vkReturn {
			s.confirm()
			return 0
		}
	case wmClose:
		s.finish(nil, ErrCaptureCanceled)
		return 0
	case wmDestroy:
		if !s.finished {
			s.finish(nil, ErrCaptureCanceled)
		}
		return 0
	}
	return snipDef(hwnd, msg, wParam, lParam)
}

func snipDef(hwnd, msg, wParam, lParam uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func clientXY(lParam uintptr) (int, int) {
	return int(int16(lParam)), int(int16(lParam >> 16))
}

func (s *snipOverlay) invalidate() {
	if s.hwnd != 0 {
		procInvalidateRect.Call(s.hwnd, 0, 0)
	}
}

func (s *snipOverlay) selRect() (x, y, w, h int, ok bool) {
	return normalizeSnipRect(s.x0, s.y0, s.x1, s.y1, s.w, s.h)
}

func (s *snipOverlay) onLeftDown(x, y int) {
	if s.haveSel && !s.dragging {
		sx, sy, sw, sh, ok := s.selRect()
		if ok {
			done, cancel := snipToolbarRects(sx, sy, sw, sh, s.w, s.h)
			if ptInRect(x, y, done.Min.X, done.Min.Y, done.Dx(), done.Dy()) {
				s.confirm()
				return
			}
			if ptInRect(x, y, cancel.Min.X, cancel.Min.Y, cancel.Dx(), cancel.Dy()) {
				s.finish(nil, ErrCaptureCanceled)
				return
			}
		}
	}
	s.dragging = true
	s.haveSel = false
	s.x0, s.y0, s.x1, s.y1 = x, y, x, y
	procSetCapture.Call(s.hwnd)
	s.invalidate()
}

func (s *snipOverlay) onMove(x, y int) {
	if !s.dragging {
		return
	}
	s.x1, s.y1 = x, y
	s.invalidate()
}

func (s *snipOverlay) onLeftUp(x, y int) {
	if !s.dragging {
		return
	}
	s.dragging = false
	s.x1, s.y1 = x, y
	procReleaseCapture.Call()
	if _, _, _, _, ok := s.selRect(); ok {
		s.haveSel = true
	} else {
		s.haveSel = false
	}
	s.invalidate()
}

func (s *snipOverlay) confirm() {
	x, y, w, h, ok := s.selRect()
	if !ok {
		return
	}
	cropped, err := cropBGRA(s.pixels, s.w, s.h, x, y, w, h)
	if err != nil {
		s.finish(nil, err)
		return
	}
	png, err := encodeBGRA(cropped, w, h)
	if err != nil {
		s.finish(nil, err)
		return
	}
	s.finish(png, nil)
}

func (s *snipOverlay) finish(png []byte, err error) {
	if s.finished {
		return
	}
	s.finished = true
	s.result = png
	s.err = err
	hwnd := s.hwnd
	s.hwnd = 0
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}
	procPostQuitMessage.Call(0)
}

func (s *snipOverlay) paint(hwnd uintptr) {
	var ps snipPaint
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if s.memDC == 0 || s.darkBmp == 0 {
		return
	}
	old, _, _ := procSelectObjectGDI.Call(s.memDC, s.darkBmp)
	procBitBlt.Call(hdc, 0, 0, uintptr(s.w), uintptr(s.h), s.memDC, 0, 0, srccopy)
	if sx, sy, sw, sh, ok := s.selRect(); ok && (s.dragging || s.haveSel) && s.lightBmp != 0 {
		procSelectObjectGDI.Call(s.memDC, s.lightBmp)
		procBitBlt.Call(hdc, uintptr(sx), uintptr(sy), uintptr(sw), uintptr(sh), s.memDC, uintptr(sx), uintptr(sy), srccopy)
		frame := winRect{Left: int32(sx), Top: int32(sy), Right: int32(sx + sw), Bottom: int32(sy + sh)}
		white, _, _ := procCreateSolidBrush.Call(0x00FFFFFF)
		green, _, _ := procCreateSolidBrush.Call(0x008CCC2E)
		if white != 0 {
			procFrameRect.Call(hdc, uintptr(unsafe.Pointer(&frame)), white)
			procDeleteObject.Call(white)
		}
		inner := winRect{Left: int32(sx + 1), Top: int32(sy + 1), Right: int32(sx + sw - 1), Bottom: int32(sy + sh - 1)}
		if green != 0 {
			procFrameRect.Call(hdc, uintptr(unsafe.Pointer(&inner)), green)
			procDeleteObject.Call(green)
		}
		if s.haveSel && !s.dragging {
			s.paintButtons(hdc, sx, sy, sw, sh)
		}
	}
	_, _, _ = procSelectObjectGDI.Call(s.memDC, old)
	s.paintHint(hdc)
}

func (s *snipOverlay) paintHint(hdc uintptr) {
	bar := winRect{Left: 0, Top: 0, Right: int32(s.w), Bottom: int32(snipHintHeight)}
	brush, _, _ := procCreateSolidBrush.Call(0x00E01010)
	if brush != 0 {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bar)), brush)
		procDeleteObject.Call(brush)
	}
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	old := uintptr(0)
	if font != 0 {
		old, _, _ = procSelectObjectGDI.Call(hdc, font)
	}
	procSetBkMode.Call(hdc, transparentMode)
	procSetTextColor.Call(hdc, 0x00FFFFFF)
	text, err := windows.UTF16PtrFromString("拖动选择区域 · 点完成或按 Enter 发送 · 右键 / Esc 取消")
	if err == nil {
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&bar)), dtCenter|dtVCenter|dtSingleLine)
	}
	if old != 0 {
		procSelectObjectGDI.Call(hdc, old)
	}
}

func (s *snipOverlay) paintButtons(hdc uintptr, sx, sy, sw, sh int) {
	done, cancel := snipToolbarRects(sx, sy, sw, sh, s.w, s.h)
	s.fillLabeledButton(hdc, done, 0x008CCC2E, "完成")
	s.fillLabeledButton(hdc, cancel, 0x00343434, "取消")
}

func (s *snipOverlay) fillLabeledButton(hdc uintptr, r image.Rectangle, color uint32, label string) {
	box := winRect{Left: int32(r.Min.X), Top: int32(r.Min.Y), Right: int32(r.Max.X), Bottom: int32(r.Max.Y)}
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	if brush != 0 {
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&box)), brush)
		procDeleteObject.Call(brush)
	}
	procSetBkMode.Call(hdc, transparentMode)
	procSetTextColor.Call(hdc, 0x00FFFFFF)
	text, err := windows.UTF16PtrFromString(label)
	if err == nil {
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&box)), dtCenter|dtVCenter|dtSingleLine)
	}
}

func dibSectionFromBGRA(pixels []byte, w, h int) (uintptr, error) {
	if w <= 0 || h <= 0 || len(pixels) < w*h*4 {
		return 0, errors.New("invalid bitmap")
	}
	var bits uintptr
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRgb,
		BiSizeImage:   uint32(w * h * 4),
	}
	bmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), dibRgbColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		if bmp != 0 {
			procDeleteObject.Call(bmp)
		}
		return 0, errors.New("createdibsection failed")
	}
	dest := unsafe.Slice((*byte)(ptrFromUintptr(bits)), w*h*4)
	copy(dest, pixels[:w*h*4])
	return bmp, nil
}
