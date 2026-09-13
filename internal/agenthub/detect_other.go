//go:build !windows

package agenthub

import "os/exec"

func extraPathDirs() []string { return nil }

func vendorInstallDirs() []string { return nil }

func hideVersionCmd(cmd *exec.Cmd) {}
