//go:build !windows

package webviewhost

func ActivateExistingMainWindow() bool { return false }
