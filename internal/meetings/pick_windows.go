//go:build windows

package meetings

import (
	"context"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lunitide/lunitide/internal/winexec"
)

var (
	modComdlg32          = syscall.NewLazyDLL("comdlg32.dll")
	procGetSaveFileNameW = modComdlg32.NewProc("GetSaveFileNameW")
)

const (
	ofnExplorer        = 0x00080000
	ofnOverwritePrompt = 0x00000002
	ofnPathMustExist   = 0x00000800
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

func pickSavePath(title, defaultName string) (string, error) {
	if path, err := pickSaveForms(title, defaultName); err == nil && path != "" {
		return path, nil
	} else if err == ErrCanceled {
		return "", ErrCanceled
	}
	return saveFileDialog(title, defaultName)
}

func pickSaveForms(title, defaultName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	escapedName := strings.ReplaceAll(defaultName, "'", "''")
	escapedTitle := strings.ReplaceAll(title, "'", "''")
	script := `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.SaveFileDialog
$d.Title = '` + escapedTitle + `'
$d.FileName = '` + escapedName + `'
$d.Filter = 'Markdown|*.md|HTML|*.html|文本|*.txt|所有文件|*.*'
$d.OverwritePrompt = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.FileName
}
`
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

func saveFileDialog(title, defaultName string) (string, error) {
	buf := make([]uint16, 32768)
	name, _ := syscall.UTF16FromString(defaultName)
	copy(buf, name)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	filter, _ := syscall.UTF16PtrFromString("Markdown\x00*.md\x00HTML\x00*.html\x00文本\x00*.txt\x00所有文件\x00*.*\x00")
	ofn := openFileName{
		lpstrFilter: filter,
		nFilterIndex: 1,
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  titlePtr,
		flags:       ofnExplorer | ofnOverwritePrompt | ofnPathMustExist,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", ErrCanceled
	}
	return syscall.UTF16ToString(buf), nil
}
