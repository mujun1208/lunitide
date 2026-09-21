package webviewhost

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/zzl/go-win32api/v2/win32"
)

// A wrong ExtractIconExW calling convention returns 0 silently and the icon
// quietly falls back to the working-directory .ico lookup, which is exactly the
// regression this path exists to kill. Prove the call works against a binary
// Windows guarantees carries an icon.
func TestExtractIconExConventionPullsIconFromBinary(t *testing.T) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		t.Skip("SystemRoot unset")
	}
	binary := filepath.Join(root, "explorer.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("no reference binary: %v", err)
	}
	path, err := syscall.UTF16PtrFromString(binary)
	if err != nil {
		t.Fatal(err)
	}
	var big, small win32.HICON
	extractIconEx.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&big)), uintptr(unsafe.Pointer(&small)), 1)
	if big == 0 && small == 0 {
		t.Fatal("ExtractIconExW returned no icon; argument order or widths are wrong")
	}
}

// The embedded path only has something to extract while the linker keeps the
// icon resource. Losing the .syso re-couples the title bar icon to the process
// working directory without any build error.
func TestDesktopBinaryEmbedsIconResource(t *testing.T) {
	info, err := os.Stat(filepath.Join("..", "..", "cmd", "desktop", "lunitide.syso"))
	if err != nil {
		t.Fatalf("desktop icon resource missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("desktop icon resource is empty")
	}
}

func TestIconMetricFallsBackToStandardSizes(t *testing.T) {
	if got := iconMetric(win32.SM_CXSMICON); got <= 0 {
		t.Fatalf("small icon metric %d", got)
	}
	if got := iconMetric(win32.SM_CXICON); got <= 0 {
		t.Fatalf("large icon metric %d", got)
	}
}
