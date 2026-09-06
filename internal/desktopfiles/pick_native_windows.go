//go:build windows

package desktopfiles

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modComdlg32              = syscall.NewLazyDLL("comdlg32.dll")
	modShell32               = syscall.NewLazyDLL("shell32.dll")
	modOle32                 = syscall.NewLazyDLL("ole32.dll")
	procGetOpenFileNameW     = modComdlg32.NewProc("GetOpenFileNameW")
	procSHBrowseForFolderW   = modShell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = modShell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = modOle32.NewProc("CoTaskMemFree")
	procCoInitializeEx       = modOle32.NewProc("CoInitializeEx")
)

type openFileName struct {
	lStructSize       uint32
	hwndOwner         syscall.Handle
	hInstance         syscall.Handle
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
}

type browseInfo struct {
	hwndOwner      syscall.Handle
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

const (
	ofnExplorer             = 0x00080000
	ofnFileMustExist        = 0x00001000
	ofnPathMustExist        = 0x00000800
	ofnHideReadOnly         = 0x00000004
	ofnAllowMultiSelect     = 0x00000200
	bifReturnOnlyFSDirs     = 0x00000001
	bifNewDialogStyle       = 0x00000040
	coInitApartmentThreaded = 0x2
)

func pickOSNative(folder, multiple bool) ([]Item, []string, error) {
	_, _, _ = procCoInitializeEx.Call(0, uintptr(coInitApartmentThreaded))
	if folder {
		dir, err := browseFolder()
		if err != nil {
			return nil, nil, err
		}
		return listFolder(dir)
	}
	paths, err := openFileDialog(multiple)
	if err != nil {
		return nil, nil, err
	}
	var items []Item
	for _, path := range paths {
		item, itemErr := itemFromPath(path)
		if itemErr != nil {
			continue
		}
		items = append(items, item)
		if len(items) >= maxItems {
			break
		}
	}
	if len(items) == 0 {
		return nil, nil, ErrCanceled
	}
	return items, nil, nil
}

func openFileDialog(multiple bool) ([]string, error) {
	buf := make([]uint16, 32768)
	title, _ := syscall.UTF16PtrFromString("选择要附加的文件")
	filter, _ := syscall.UTF16PtrFromString("支持的文件\x00*.txt;*.md;*.json;*.csv;*.html;*.xml;*.js;*.ts;*.py;*.go;*.java;*.c;*.cpp;*.rs;*.yaml;*.yml;*.sh;*.sql;*.png;*.jpg;*.jpeg;*.webp\x00所有文件\x00*.*\x00")
	flags := uint32(ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnHideReadOnly)
	if multiple {
		flags |= ofnAllowMultiSelect
	}
	ofn := openFileName{
		hwndOwner:    0,
		lpstrFilter:  filter,
		nFilterIndex: 1,
		lpstrFile:    &buf[0],
		nMaxFile:     uint32(len(buf)),
		lpstrTitle:   title,
		flags:        flags,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return nil, ErrCanceled
	}
	return joinPickedNames(splitUTF16Z(buf)), nil
}

func browseFolder() (string, error) {
	display := make([]uint16, 260)
	title, _ := syscall.UTF16PtrFromString("选择要导入的文件夹")
	bi := browseInfo{
		pszDisplayName: &display[0],
		lpszTitle:      title,
		ulFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", ErrCanceled
	}
	defer procCoTaskMemFree.Call(pidl)
	path := make([]uint16, 32768)
	r, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if r == 0 {
		return "", ErrCanceled
	}
	return syscall.UTF16ToString(path), nil
}

func splitUTF16Z(buf []uint16) []string {
	var parts []string
	start := 0
	for i, w := range buf {
		if w != 0 {
			continue
		}
		if i == start {
			break
		}
		parts = append(parts, syscall.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	return parts
}

func joinPickedNames(parts []string) []string {
	if len(parts) <= 1 {
		return parts
	}
	dir := parts[0]
	out := make([]string, 0, len(parts)-1)
	for _, name := range parts[1:] {
		if name == "" {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}
