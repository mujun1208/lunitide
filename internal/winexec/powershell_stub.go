//go:build !windows

package winexec

import (
	"context"
	"os/exec"
)

// HiddenPowerShell is a no-op stub on non-Windows platforms.
func HiddenPowerShell(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "powershell", args...)
}
