//go:build !windows

package people

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestPickLocalPathUnsupportedWithoutDialog(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("osascript"); err == nil {
			t.Skip("osascript present; would open a live dialog")
		}
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		t.Skip("zenity present; would open a live dialog")
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		t.Skip("kdialog present; would open a live dialog")
	}
	if _, err := pickLocalPath(true); err != ErrUnsupported {
		t.Fatalf("folder pick without a dialog tool = %v", err)
	}
	if _, err := pickLocalPath(false); err != ErrUnsupported {
		t.Fatalf("file pick without a dialog tool = %v", err)
	}
}
