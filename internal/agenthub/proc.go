package agenthub

import (
	"context"
	"os"
	"time"
)

type ProcSpec struct {
	Exe     string
	Dir     string
	Args    []string
	Stdin   []byte
	Timeout time.Duration
}

func clampTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return 30 * time.Minute
	}
	if d > 120*time.Minute {
		return 120 * time.Minute
	}
	return d
}

func StartProcess(ctx context.Context, spec ProcSpec, onLine func(string)) (exit int64, timedOut bool, err error) {
	return startProcess(ctx, spec, onLine)
}

func writeAndClose(file *os.File, body []byte) error {
	_, err := file.Write(body)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
