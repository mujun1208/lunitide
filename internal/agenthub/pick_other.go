//go:build !windows

package agenthub

import "fmt"

func pickWorkDirOS() (string, error) {
	return "", fmt.Errorf("Agent 调度台仅支持 Windows")
}
