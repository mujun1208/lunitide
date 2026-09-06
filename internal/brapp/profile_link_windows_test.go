//go:build windows

package brapp

import (
	"os/exec"
	"strings"
	"syscall"
)

func makeBrowserTestLink(target, link string) error {
	// Junction creation needs no Developer Mode privilege. Paths are entirely
	// inside the test's two temporary roots and quoted as PowerShell literals.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ItemType Junction -Path "+quote(link)+" -Target "+quote(target)+" -ErrorAction Stop | Out-Null")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
