//go:build windows

package officerender

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LOGFONTW follows wingdi.h: five LONGs, eight BYTEs and WCHAR[32]. No
// document text or file path is passed to GDI and no visible window is opened.
type fontLogW struct {
	Height, Width, Escapement, Orientation, Weight       int32
	Italic, Underline, StrikeOut, CharSet                byte
	OutPrecision, ClipPrecision, Quality, PitchAndFamily byte
	FaceName                                             [32]uint16
}

var fontGDI = windows.NewLazySystemDLL("gdi32.dll")
var fontCreateDC = fontGDI.NewProc("CreateCompatibleDC")
var fontDeleteDC = fontGDI.NewProc("DeleteDC")
var fontEnum = fontGDI.NewProc("EnumFontFamiliesExW")
var fontEnumerationMu sync.Mutex
var activeFontEnumeration struct {
	ctx      context.Context
	families map[string]string
	complete bool
}

// Allocate the Windows callback once, not once per user request; syscall
// callbacks are permanent and repeated allocation would leak callback slots.
var fontEnumCallback = syscall.NewCallback(func(logfont, metric, fontType, param uintptr) uintptr {
	state := &activeFontEnumeration
	if state.ctx == nil || state.ctx.Err() != nil {
		state.complete = false
		return 0
	}
	if logfont == 0 {
		return 1
	}
	// uintptr→Pointer via address-of, same vet-safe pattern as host_windows.go.
	lf := (*fontLogW)(*(*unsafe.Pointer)(unsafe.Pointer(&logfont)))
	end := 0
	for end < len(lf.FaceName) && lf.FaceName[end] != 0 {
		end++
	}
	name := fontFamilyName(string(utf16.Decode(lf.FaceName[:end])))
	if name != "" {
		state.families[fontFamilyKey(name)] = name
	}
	if len(state.families) >= 4096 {
		state.complete = false
		return 0
	}
	return 1
})

func systemFontInventory(ctx context.Context) (FontInventory, error) {
	out := FontInventory{Basis: "windows-gdi-registered-families", Families: []string{}, Notice: "来自Windows当前设备上下文的字体家族枚举；字形覆盖和本地化别名另行验证。"}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	fontEnumerationMu.Lock()
	defer fontEnumerationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	hdc, _, err := fontCreateDC.Call(0)
	if hdc == 0 {
		return out, err
	}
	defer fontDeleteDC.Call(hdc)
	activeFontEnumeration.ctx = ctx
	activeFontEnumeration.families = map[string]string{}
	activeFontEnumeration.complete = true
	defer func() { activeFontEnumeration.ctx = nil; activeFontEnumeration.families = nil }()
	lf := fontLogW{CharSet: 1} // DEFAULT_CHARSET, empty face, zero pitch/family
	fontEnum.Call(hdc, uintptr(unsafe.Pointer(&lf)), fontEnumCallback, 0, 0)
	runtime.KeepAlive(lf)
	if err := ctx.Err(); err != nil {
		return out, err
	}
	for _, name := range activeFontEnumeration.families {
		out.Families = append(out.Families, name)
	}
	sort.Strings(out.Families)
	out.Complete = activeFontEnumeration.complete
	if len(out.Families) == 0 {
		out.Complete = false
		return out, errors.New("Windows字体枚举没有返回任何家族")
	}
	return out, nil
}
