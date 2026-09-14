//go:build !windows

package desktopupdate

import "os/exec"

func startDetached(cmd *exec.Cmd) error { return cmd.Start() }
