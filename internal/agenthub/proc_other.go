//go:build !windows

package agenthub

import (
	"context"
	"fmt"
)

func startProcess(context.Context, ProcSpec, func(string)) (int64, bool, error) {
	return 0, false, fmt.Errorf("AgentHub 仅支持 Windows")
}

func startPersistent(context.Context, ProcSpec) (*PersistentProc, error) {
	return nil, fmt.Errorf("AgentHub 仅支持 Windows")
}
