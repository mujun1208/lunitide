//go:build !windows

package main

import "os/exec"

func configureEngineProcess(_ *exec.Cmd) {}
