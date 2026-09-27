//go:build !windows

package people

import (
	"os/exec"
	"runtime"
)

var openPathFn = openLocalPath

func openLocalPath(path string) error {
	if suppressShell.Load() {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
