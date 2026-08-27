//go:build !windows

package omni

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func startOmniServer(executable string, args []string) (*exec.Cmd, error) {
	if _, err := os.Stat(executable); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMissingRuntime, err)
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = filepath.Dir(executable)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("omni: start llama-omni-server: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return cmd, nil
}
