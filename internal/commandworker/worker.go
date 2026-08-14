// Package commandworker executes signed CommandSpec processes with bounded
// lifetime and resources. On Windows the process tree runs inside a Job
// Object (kill-on-close, active-process and memory limits); other platforms
// fall back to a process-group scoped implementation. The Job Object bounds
// lifetime and resources only — it is not a security sandbox
// (PRD M4 信任/失败/恢复: Worker 用 Job Object 限时限额但不得宣称沙箱).
//
// The worker never sees a shell command line: Spec.Exe is an absolute path
// and Spec.Args pass verbatim to CreateProcess/exec. Combined stdout+stderr
// streams to the caller and is capped at the spec's output limit; the child
// is always drained so it never blocks on a full pipe.
package commandworker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultTimeout applies when a spec leaves Timeout zero.
	DefaultTimeout = 2 * time.Minute
	// TimeoutHardCap is the wall-clock ceiling for any job; a spec above it
	// is rejected fail-closed (PRD M4 预算 hard ceiling).
	TimeoutHardCap = 15 * time.Minute
	// DefaultOutputCap applies when a spec leaves MaxOutputBytes zero.
	DefaultOutputCap = 1 << 20
	// OutputHardCap is the combined-output ceiling for any job.
	OutputHardCap = 4 << 20

	maxArgs     = 64
	maxArgLen   = 1024
	maxEnvPairs = 256
)

var (
	// ErrInvalidSpec is returned when the resolved spec violates worker
	// invariants (relative exe, non-absolute dir, out-of-range limits).
	ErrInvalidSpec = errors.New("invalid command spec")
	// ErrUnsupported is returned on platforms without a worker backend.
	ErrUnsupported = errors.New("command worker unsupported on this platform")
)

// Spec is a fully resolved execution request produced from a signed
// CommandSpec template. Fields are validated and limits normalized by
// Validate before any process starts.
type Spec struct {
	Exe  string   // absolute path to the executable image
	Args []string // verbatim argv, no shell expansion
	Dir  string   // absolute working directory inside the granted workspace
	Env  []string // complete KEY=VALUE environment (replaces the parent env)

	Timeout        time.Duration
	MaxOutputBytes int
}

// Validate enforces worker invariants and normalizes the resource limits to
// their defaults. Limits above the hard caps are rejected fail-closed.
func (s *Spec) Validate() error {
	if !filepath.IsAbs(s.Exe) {
		return fmt.Errorf("%w: exe must be an absolute path", ErrInvalidSpec)
	}
	if !filepath.IsAbs(s.Dir) {
		return fmt.Errorf("%w: dir must be an absolute path", ErrInvalidSpec)
	}
	if len(s.Args) > maxArgs {
		return fmt.Errorf("%w: argv exceeds %d entries", ErrInvalidSpec, maxArgs)
	}
	for _, arg := range s.Args {
		if len(arg) > maxArgLen || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%w: argv entry malformed", ErrInvalidSpec)
		}
	}
	if len(s.Env) > maxEnvPairs {
		return fmt.Errorf("%w: environment exceeds %d pairs", ErrInvalidSpec, maxEnvPairs)
	}
	for _, pair := range s.Env {
		key, _, ok := strings.Cut(pair, "=")
		if !ok || key == "" || strings.ContainsAny(pair, "\x00\r\n") {
			return fmt.Errorf("%w: environment entry malformed", ErrInvalidSpec)
		}
	}
	if s.Timeout < 0 || s.Timeout > TimeoutHardCap {
		return fmt.Errorf("%w: timeout outside 0..%s", ErrInvalidSpec, TimeoutHardCap)
	}
	if s.Timeout == 0 {
		s.Timeout = DefaultTimeout
	}
	if s.MaxOutputBytes < 0 || s.MaxOutputBytes > OutputHardCap {
		return fmt.Errorf("%w: output cap outside 0..%d", ErrInvalidSpec, OutputHardCap)
	}
	if s.MaxOutputBytes == 0 {
		s.MaxOutputBytes = DefaultOutputCap
	}
	return nil
}

// Outcome reports how a finished run ended. A non-zero ExitCode is a valid
// outcome (the job failed); only worker-level failures return an error.
type Outcome struct {
	ExitCode    int64
	TimedOut    bool
	OutputBytes int64 // bytes delivered to the sink (<= cap)
	Truncated   bool  // true when output exceeded the cap and was clipped
}

// StartGuard pins process working-directory resolution until process start.
// Close is idempotent; non-Windows implementations are no-ops.
type StartGuard interface{ Close() error }

// Run executes spec, streaming combined output to onOutput until the byte
// cap, and returns the outcome. A timeout kills the whole process tree and
// reports TimedOut with ExitCode -1; caller cancellation kills the tree and
// returns the context error. The tree is terminated on every exit path, so
// no orphaned grandchildren survive (PRD M4 门禁: 取消后无孤儿进程).
func Run(ctx context.Context, spec Spec, guard StartGuard, onOutput func([]byte)) (Outcome, error) {
	if err := spec.Validate(); err != nil {
		if guard != nil {
			_ = guard.Close()
		}
		return Outcome{}, err
	}
	if guard == nil {
		guard = noopStartGuard{}
	}
	if onOutput == nil {
		onOutput = func([]byte) {}
	}
	sink := &cappedSink{limit: spec.MaxOutputBytes, cb: onOutput}
	outcome, err := run(ctx, spec, guard, sink)
	outcome.OutputBytes = sink.delivered
	outcome.Truncated = sink.truncated
	return outcome, err
}

type noopStartGuard struct{}

func (noopStartGuard) Close() error { return nil }

// cappedSink forwards at most limit bytes to the caller. Writes after the
// cap are dropped but counted, so the pipe reader keeps draining.
type cappedSink struct {
	limit     int
	delivered int64
	truncated bool
	cb        func([]byte)
}

func (s *cappedSink) Write(p []byte) {
	accepted := 0
	remaining := s.limit - int(s.delivered)
	if remaining > 0 {
		chunk := p
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		s.cb(chunk)
		accepted = len(chunk)
		s.delivered += int64(accepted)
	}
	if accepted < len(p) {
		s.truncated = true
	}
}
