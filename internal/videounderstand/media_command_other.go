//go:build !windows

package videounderstand

import "os/exec"

func hideMediaCommand(*exec.Cmd) {}
