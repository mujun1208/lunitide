//go:build windows

package winexec

import (
	"testing"
	"unsafe"
)

func TestMediaInputSizeMatchesWin32INPUT(t *testing.T) {
	if got := unsafe.Sizeof(mediaInput{}); got != 40 {
		t.Fatalf("sizeof mediaInput = %d, want 40 (Win32 INPUT on x64)", got)
	}
}
