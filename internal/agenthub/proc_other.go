//go:build !windows

package agenthub

import (
	"context"
	"fmt"
)

func startProcess(context.Context, ProcSpec, func(string)) (int64, bool, error) {
	return 0, false, fmt.Errorf("Agent 调度台仅支持 Windows")
}
