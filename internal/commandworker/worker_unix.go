//go:build darwin || linux

package commandworker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Unix fallback: the child leads its own process group (Setpgid); timeout,
// cancel and normal exit all SIGKILL the whole group, mirroring the
// kill-on-close Job Object semantics of the Windows backend.
func PinWorkingDirectory(_, _ string) (StartGuard, error) { return noopStartGuard{}, nil }

func run(ctx context.Context, spec Spec, guard StartGuard, sink *cappedSink) (Outcome, error) {
	defer guard.Close()
	cmd := exec.Command(spec.Exe, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil // child stdin is the null device: immediate EOF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pipeRead, pipeWrite, err := os.Pipe()
	if err != nil {
		return Outcome{}, err
	}
	defer pipeRead.Close()
	cmd.Stdout = pipeWrite
	cmd.Stderr = pipeWrite
	if err := cmd.Start(); err != nil {
		pipeWrite.Close()
		return Outcome{}, err
	}
	_ = guard.Close()
	pgid := cmd.Process.Pid
	pipeWrite.Close() // parent copy: pipe EOFs when the group is dead

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32<<10)
		for {
			n, readErr := pipeRead.Read(buf)
			if n > 0 {
				sink.Write(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()

	type waitResult struct{ err error }
	waitDone := make(chan waitResult, 1)
	go func() {
		waitDone <- waitResult{err: cmd.Wait()}
	}()
	killGroup := func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}

	timer := time.NewTimer(spec.Timeout)
	defer timer.Stop()
	var outcome Outcome
	waited := false
	select {
	case res := <-waitDone:
		waited = true
		if res.err != nil {
			var exitErr *exec.ExitError
			if errors.As(res.err, &exitErr) {
				outcome.ExitCode = int64(exitErr.ExitCode())
			} else {
				killGroup()
				<-readDone
				return Outcome{}, res.err
			}
		}
	case <-timer.C:
		outcome.TimedOut = true
		outcome.ExitCode = -1
	case <-ctx.Done():
		killGroup()
		<-waitDone
		<-readDone
		return Outcome{}, ctx.Err()
	}
	// Mirror the Windows tree kill: no group member outlives the job.
	killGroup()
	if !waited {
		<-waitDone
	}
	<-readDone
	return outcome, nil
}
