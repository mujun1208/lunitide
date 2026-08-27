//go:build windows

package omni

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// applyRuntimeSetup runs the pinned NSIS installer silently into dest.
// /D= must be last and unquoted. Does not taskkill a global Comni.exe.
func applyRuntimeSetup(setup, dest string) error {
	if _, err := os.Stat(setup); err != nil {
		return fmt.Errorf("%w: %v", ErrMissingRuntime, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(setup, "/S", "/D="+dest)
	cmd.Dir = dest
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("omni: 静默安装 llama-omni-server 失败: %w", err)
	}
	return nil
}
