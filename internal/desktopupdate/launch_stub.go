//go:build !windows

package desktopupdate

import (
	"fmt"
	"os/exec"
)

func startDetached(*exec.Cmd) error {
	return fmt.Errorf("desktopupdate: silent setup is Windows-only")
}
