//go:build !windows

package app

import (
	"os/exec"
	"runtime"
)

func openLocalArtifactPath(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	command := exec.Command(opener, path)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
