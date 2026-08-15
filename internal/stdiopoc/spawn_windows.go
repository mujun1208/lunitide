//go:build windows

package stdiopoc

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows spawn engine: the probe child starts suspended, joins a fresh Job
// Object (kill-on-close, active-process limit, commit limit) and only then
// resumes. The environment is an explicit sorted block — the parent's
// environment never leaks in — and stdio is a pair of pipes the host frames.
// TerminateJobObject reaps the entire tree, so no grandchild survives.

var (
	procCreateJobObjectW         = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateJobObjectW")
	procSetInformationJobObject  = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = windows.NewLazySystemDLL("kernel32.dll").NewProc("TerminateJobObject")
	procResumeThread             = windows.NewLazySystemDLL("kernel32.dll").NewProc("ResumeThread")
)

const (
	jobObjectExtendedLimitInformation = 9

	limitActiveProcess           = 0x00000008
	limitDieOnUnhandledException = 0x00000400
	limitJobMemory               = 0x00000200
	limitKillOnJobClose          = 0x00002000

	createSuspended          = 0x00000004
	createUnicodeEnvironment = 0x00000400
	createNoWindow           = 0x08000000

	// defaultSpawnProcs / defaultSpawnMem apply when the spec leaves quotas
	// zero (tests that only exercise the protocol layer).
	defaultSpawnProcs = 8
	defaultSpawnMem   = 1 << 30
)

type pocJobBasicLimit struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}

type pocJobIOCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type pocJobExtendedLimit struct {
	BasicLimitInformation pocJobBasicLimit
	IoInfo                pocJobIOCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

// spawn launches the probe per spec. See Proc for the returned surface.
func spawn(_ context.Context, spec SpawnSpec) (*Proc, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	maxProcs := spec.MaxProcs
	if maxProcs == 0 {
		maxProcs = defaultSpawnProcs
	}
	memCap := spec.MemoryCapBytes
	if memCap == 0 {
		memCap = defaultSpawnMem
	}

	// Pipes start non-inheritable; only the child-side ends are inheritable.
	var inRead, inWrite, outRead, outWrite windows.Handle
	noInherit := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if err := windows.CreatePipe(&inRead, &inWrite, noInherit, 0); err != nil {
		return nil, fmt.Errorf("stdiopoc: stdin pipe: %w", err)
	}
	defer func() {
		for _, ph := range []*windows.Handle{&inRead, &inWrite, &outRead, &outWrite} {
			if *ph != 0 {
				windows.CloseHandle(*ph)
			}
		}
	}()
	if err := windows.CreatePipe(&outRead, &outWrite, noInherit, 0); err != nil {
		return nil, fmt.Errorf("stdiopoc: stdout pipe: %w", err)
	}
	for _, h := range []windows.Handle{inRead, outWrite} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, fmt.Errorf("stdiopoc: inherit pipe: %w", err)
		}
	}

	// Job Object: process-tree and commit quotas, kill-on-close.
	r, _, e := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return nil, fmt.Errorf("stdiopoc: CreateJobObject: %w", e)
	}
	job := windows.Handle(r)
	// NOTE: the job and process handles outlive this function — the Proc
	// closers own them. Closing the job here would fire KILL_ON_JOB_CLOSE
	// and reap the tree instantly.
	limits := pocJobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = limitKillOnJobClose | limitActiveProcess | limitJobMemory | limitDieOnUnhandledException
	limits.BasicLimitInformation.ActiveProcessLimit = maxProcs
	limits.JobMemoryLimit = uintptr(memCap)
	r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if r == 0 {
		return nil, fmt.Errorf("stdiopoc: SetInformationJobObject: %w", e)
	}

	appName, err := windows.UTF16PtrFromString(spec.Exe)
	if err != nil {
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{spec.Exe}, spec.Args...)))
	if err != nil {
		return nil, err
	}
	cwd, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		return nil, err
	}
	envPtr := (*uint16)(nil)
	if block := utf16EnvBlock(spec.Env); len(block) > 0 {
		envPtr = &block[0]
	}
	si := windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = inRead
	si.StdOutput = outWrite
	si.StdErr = outWrite
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(appName, cmdline, nil, nil, true,
		createSuspended|createNoWindow|createUnicodeEnvironment, envPtr, cwd, &si, &pi); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("stdiopoc: CreateProcess: %w", err)
	}
	defer func() {
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
	}()
	if r, _, e := procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process)); r == 0 {
		procTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(job)
		return nil, fmt.Errorf("stdiopoc: AssignProcessToJobObject: %w", e)
	}
	if r, _, e := procResumeThread.Call(uintptr(pi.Thread)); r == 0xffffffff {
		procTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(job)
		return nil, fmt.Errorf("stdiopoc: ResumeThread: %w", e)
	}
	windows.CloseHandle(pi.Thread)
	pi.Thread = 0

	// Parent copies of the child pipe ends close now so stdin sees EOF when
	// we are done and stdout reaches EOF once the tree is dead.
	childIn := os.NewFile(uintptr(inWrite), "poc-stdin")
	childOut := os.NewFile(uintptr(outRead), "poc-stdout")
	windows.CloseHandle(inRead)
	inRead = 0
	windows.CloseHandle(outWrite)
	outWrite = 0
	inWrite = 0
	outRead = 0

	p := &Proc{
		Stdin:  NewFrameWriter(childIn),
		Stdout: NewFrameReader(childOut),
		Pid:    int(pi.ProcessId),
	}
	var closed bool
	closeHandles := func() {
		if !closed {
			closed = true
			windows.CloseHandle(pi.Process)
			windows.CloseHandle(job)
		}
	}
	p.kill = func() error {
		if closed {
			return nil
		}
		if r, _, e := procTerminateJobObject.Call(uintptr(job), 1); r == 0 {
			return fmt.Errorf("stdiopoc: TerminateJobObject: %w", e)
		}
		return nil
	}
	p.wait = func(ctx context.Context) (int, error) {
		defer closeHandles()
		type res struct {
			code uint32
			err  error
		}
		done := make(chan res, 1)
		go func() {
			windows.WaitForSingleObject(pi.Process, windows.INFINITE)
			var code uint32
			err := windows.GetExitCodeProcess(pi.Process, &code)
			done <- res{code: code, err: err}
		}()
		timer := time.NewTimer(spec.Timeout)
		defer timer.Stop()
		select {
		case r := <-done:
			if r.err != nil {
				return -1, r.err
			}
			return int(r.code), nil
		case <-timer.C:
			p.Kill()
			r := <-done
			if r.err != nil {
				return -1, fmt.Errorf("stdiopoc: timeout then wait: %w", r.err)
			}
			return int(r.code), fmt.Errorf("stdiopoc: spawn timed out after %s", spec.Timeout)
		case <-ctx.Done():
			p.Kill()
			r := <-done
			_ = r
			return -1, ctx.Err()
		}
	}
	return p, nil
}

// utf16EnvBlock builds the sorted, double-NUL-terminated environment block.
func utf16EnvBlock(env []string) []uint16 {
	sorted := append([]string(nil), env...)
	sort.Slice(sorted, func(i, j int) bool { return strings.ToUpper(sorted[i]) < strings.ToUpper(sorted[j]) })
	return utf16.Encode([]rune(strings.Join(sorted, "\x00") + "\x00\x00"))
}
