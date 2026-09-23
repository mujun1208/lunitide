//go:build !windows

package toolruntime

import "os/exec"

func detachProcess(cmd *exec.Cmd) {}
