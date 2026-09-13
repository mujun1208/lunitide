package agenthub

import (
	"context"
	"io"
	"os"
	"sync"
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

type PersistentProc struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	logs   *safeLogBuf
	closer func() error
	once   sync.Once
}

func StartPersistent(ctx context.Context, spec ProcSpec) (*PersistentProc, error) {
	return startPersistent(ctx, spec)
}

func (p *PersistentProc) Close() error {
	if p == nil {
		return nil
	}
	var err error
	p.once.Do(func() {
		if p.closer != nil {
			err = p.closer()
		}
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
	})
	return err
}

func (p *PersistentProc) logText() string {
	if p == nil || p.logs == nil {
		return ""
	}
	return p.logs.String()
}

type safeLogBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *safeLogBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const capBytes = 4096
	if len(s.b) >= capBytes {
		return len(p), nil
	}
	need := capBytes - len(s.b)
	if len(p) > need {
		p = p[:need]
	}
	s.b = append(s.b, p...)
	return len(p), nil
}

func (s *safeLogBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.b)
}

func writeAndClose(file *os.File, body []byte) error {
	_, err := file.Write(body)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
