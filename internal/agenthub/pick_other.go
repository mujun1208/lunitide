//go:build !windows

package agenthub

import "fmt"

func pickWorkDirOS() (string, error) {
	return "", fmt.Errorf("AgentHub 仅支持 Windows")
}
