//go:build !windows && !darwin && !linux

package commandworker

// processAlive is unsupported here because the worker itself is unsupported;
// the tree-kill test never runs on these platforms.
func processAlive(int) bool { return false }
