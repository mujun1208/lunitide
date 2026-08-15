//go:build windows

// M5 T-5.3.3 job execution on Windows: every command.start spawns inside a
// Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so Cancel reaps the
// whole process tree even when the child spawned grandchildren, and so an
// agent crash still reaps orphans via the last handle close (TASK-001).
// Watchdogs flip the job to background on elapsed or output thresholds and
// the timeout cancels the tree.

package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Default backgrounding thresholds for JobOptions.
const (
	DefaultBackgroundAfterMs    int64 = 10000
	DefaultBackgroundAfterBytes int64 = 1 << 20
)

// Backgrounding reason labels (TASK-001 events).
const (
	ReasonElapsed = "elapsed"
	ReasonOutput  = "output"
)

// JobOptions tunes one job run.
type JobOptions struct {
	TimeoutMs            int64
	BackgroundAfterMs    int64 // default DefaultBackgroundAfterMs
	BackgroundAfterBytes int64 // default DefaultBackgroundAfterBytes
	Clock                Clock
}

// lockedBuffer accumulates stdout+stderr concurrently (os/exec copies the
// pipes on two goroutines).
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Job is one running process tree pinned to a Job Object.
type Job struct {
	cmd       *exec.Cmd
	jobHandle windows.Handle
	logBuf    *lockedBuffer

	// mu guards the mutable fields below (logBuf has its own lock).
	mu               sync.Mutex
	backgrounded     bool
	backgroundReason string
	exitCode         int
	waitErr          error
	handleClosed     bool
	bgBytes          int64
	tokenSeed        string
	cancelOnce       sync.Once

	startedAt time.Time
	done      chan struct{}
}

// StartJob launches argv inside a new Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: Cancel (or the loss of the last job
// handle on agent death) terminates the entire tree, which is the TASK-001
// orphan-reaping guarantee. Watchdogs mark the job backgrounded once
// BackgroundAfterMs elapses or accumulated output passes
// BackgroundAfterBytes; TimeoutMs (when > 0) auto-cancels the tree.
func StartJob(argv []string, cwd string, env []string, opts JobOptions) (*Job, error) {
	if len(argv) == 0 {
		return nil, errors.New("command: empty argv")
	}
	if opts.BackgroundAfterMs <= 0 {
		opts.BackgroundAfterMs = DefaultBackgroundAfterMs
	}
	if opts.BackgroundAfterBytes <= 0 {
		opts.BackgroundAfterBytes = DefaultBackgroundAfterBytes
	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}
	startedAt := opts.Clock.Now().UTC()
	lb := &lockedBuffer{}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdout = lb
	cmd.Stderr = lb
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("command: start %s: %w", argv[0], err)
	}
	job, seed, err := attachJobObject(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if seed == "" {
		// Creation time unavailable; fall back to the launch wall clock.
		seed = fmt.Sprintf("%d|%d", cmd.Process.Pid, startedAt.UnixNano())
	}
	j := &Job{
		cmd: cmd, jobHandle: job, logBuf: lb,
		startedAt: startedAt,
		done:      make(chan struct{}),
		bgBytes:   opts.BackgroundAfterBytes,
		tokenSeed: seed,
	}
	// Reap goroutine: record the result, run the final output check (so
	// the backgrounded flag is stable once Wait returns), close the job
	// handle and unlock Wait.
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
				err = nil // a non-zero exit is a valid result
			}
		}
		if int64(j.logBuf.Len()) >= j.bgBytes {
			j.MarkBackground(ReasonOutput)
		}
		j.mu.Lock()
		j.exitCode, j.waitErr = code, err
		if !j.handleClosed {
			windows.CloseHandle(j.jobHandle)
			j.handleClosed = true
			j.jobHandle = 0
		}
		j.mu.Unlock()
		close(j.done)
	}()
	// Elapsed watchdog: first threshold wins, MarkBackground is idempotent.
	go func() {
		t := time.NewTimer(time.Duration(opts.BackgroundAfterMs) * time.Millisecond)
		defer t.Stop()
		select {
		case <-t.C:
			j.MarkBackground(ReasonElapsed)
		case <-j.done:
		}
	}()
	// Output watchdog: poll accumulated stdout+stderr bytes.
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if int64(j.logBuf.Len()) >= j.bgBytes {
					j.MarkBackground(ReasonOutput)
					return
				}
			case <-j.done:
				return
			}
		}
	}()
	// Timeout watchdog: cancel the whole tree when TimeoutMs lapses.
	if opts.TimeoutMs > 0 {
		go func() {
			t := time.NewTimer(time.Duration(opts.TimeoutMs) * time.Millisecond)
			defer t.Stop()
			select {
			case <-t.C:
				_ = j.Cancel()
			case <-j.done:
			}
		}()
	}
	return j, nil
}

// attachJobObject creates the Job Object, arms kill-on-close and assigns
// the root process. It returns the handle plus a "pid|creationTime" seed
// for PidToken (empty when the creation time cannot be read).
func attachJobObject(pid int) (windows.Handle, string, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, "", fmt.Errorf("command: create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, "", fmt.Errorf("command: configure job object: %w", err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0, "", fmt.Errorf("command: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return 0, "", fmt.Errorf("command: assign process %d to job object: %w", pid, err)
	}
	seed := ""
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
		seed = fmt.Sprintf("%d|%d", pid, creation.Nanoseconds())
	}
	return job, seed, nil
}

// MarkBackground flags the job as moved to background; idempotent, the
// first reason wins.
func (j *Job) MarkBackground(reason string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.backgrounded {
		return
	}
	j.backgrounded = true
	j.backgroundReason = reason
}

// Backgrounded reports whether the job was backgrounded and why
// ("elapsed" | "output").
func (j *Job) Backgrounded() (bool, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.backgrounded, j.backgroundReason
}

// Logs returns the accumulated stdout+stderr so far.
func (j *Job) Logs() string { return j.logBuf.String() }

// Wait blocks until the tree's root process exits and reports its exit
// code; a non-zero exit is a result, not an error.
func (j *Job) Wait() (int, error) {
	<-j.done
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.exitCode, j.waitErr
}

// Cancel terminates the whole job tree via TerminateJobObject and closes
// the handle; repeat calls are idempotent and return nil.
func (j *Job) Cancel() error {
	j.cancelOnce.Do(func() {
		j.mu.Lock()
		h := j.jobHandle
		j.mu.Unlock()
		if h == 0 {
			return
		}
		_ = windows.TerminateJobObject(h, 1)
	})
	return nil
}

// PidToken fingerprints the process as sha256(pid|creation time) hex so a
// recycled PID yields a different token (PID-reuse defence).
func (j *Job) PidToken() string {
	j.mu.Lock()
	seed := j.tokenSeed
	j.mu.Unlock()
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
