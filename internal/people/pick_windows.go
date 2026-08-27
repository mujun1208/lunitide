//go:build windows

package people

import (
	"context"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lunitide/lunitide/internal/winexec"
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
	ofnExplorer              = 0x00080000
	ofnFileMustExist         = 0x00001000
	ofnPathMustExist         = 0x00000800
	ofnHideReadOnly          = 0x00000004
	bifReturnOnlyFSDirs      = 0x00000001
	bifNewDialogStyle        = 0x00000040
	coInitApartmentThreaded  = 0x2
)

func pickLocalPath(folder bool) (string, error) {
	if path, err := pickWindowsForms(folder); err == nil && path != "" {
		return path, nil
	} else if err != nil && !os.IsTimeout(err) && err != ErrCanceled {
		// Forms unavailable — fall through to the older common dialogs.
	} else if err == ErrCanceled {
		return "", ErrCanceled
	}
	_, _, _ = procCoInitializeEx.Call(0, uintptr(coInitApartmentThreaded))
	if folder {
		return browseFolder()
	}
	return openFileDialog()
}

func pickWindowsForms(folder bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	script := openFileFormsScript
	if folder {
		script = folderFormsScript
	}
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrCanceled
	}
	return path, nil
}

const folderFormsScript = `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择要打包发送的文件夹'
$d.ShowNewFolderButton = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.SelectedPath
}
`

const openFileFormsScript = `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = '选择要发送的文件'
$d.Filter = '所有文件|*.*'
$d.CheckFileExists = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.FileName
}
`

func openFileDialog() (string, error) {
	buf := make([]uint16, 32768)
	title, _ := syscall.UTF16PtrFromString("选择要发送的文件")
	filter, _ := syscall.UTF16PtrFromString("所有文件\x00*.*\x00")
	ofn := openFileName{
		hwndOwner:    0,
		lpstrFilter:  filter,
		nFilterIndex: 1,
		lpstrFile:    &buf[0],
		nMaxFile:     uint32(len(buf)),
		lpstrTitle:   title,
		flags:        ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnHideReadOnly,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", ErrCanceled
	}
	return syscall.UTF16ToString(buf), nil
}

func browseFolder() (string, error) {
	display := make([]uint16, 260)
	title, _ := syscall.UTF16PtrFromString("选择要打包发送的文件夹")
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
