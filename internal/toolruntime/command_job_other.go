//go:build !windows

package toolruntime

import "os/exec"

// superviseProcessTree is a no-op on non-Windows platforms (S-03). The Job
// Object grandchild-reaping guarantee is Windows-specific; on other systems the
// context timeout on exec.CommandContext handles child termination and the
// production target for command.run is Windows. Returning an empty closer keeps
// the call sites in runtime.go platform-agnostic.
func superviseProcessTree(cmd *exec.Cmd) func() {
	_ = cmd
	return func() {}
}