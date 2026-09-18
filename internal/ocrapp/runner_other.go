//go:build !windows

package ocrapp

import "os/exec"

func hideOCRWindow(cmd *exec.Cmd) {}
