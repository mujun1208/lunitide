//go:build windows

package agenthub

import (
	"os/exec"
	"testing"
)

func TestHideVersionCmdSetsNoWindow(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ver")
	hideVersionCmd(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("%#v", cmd.SysProcAttr)
	}
}
