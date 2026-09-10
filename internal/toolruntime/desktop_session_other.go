//go:build !windows

package toolruntime

func workstationLocked() bool {
	return false
}
