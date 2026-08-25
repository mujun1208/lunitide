//go:build windows

package commandworker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows backend: the child starts suspended, joins a fresh Job Object
// (kill-on-close + active-process + memory limits), and only then resumes.
// TerminateJobObject kills the whole tree on timeout, cancel and normal
// exit, so no grandchildren outlive the job. The job is a resource and
// lifecycle boundary, not a sandbox.

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")

var (
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procResumeThread             = kernel32.NewProc("ResumeThread")
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

	// maxJobProcesses bounds the tree (compilers link and spawn helpers).
	maxJobProcesses = 64
	// jobMemoryCapBytes bounds commit charged to the whole tree.
	jobMemoryCapBytes = 4 << 30
)

type jobBasicLimit struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}

type jobIOCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type jobExtendedLimit struct {
	BasicLimitInformation                                                        jobBasicLimit
	IoInfo                                                                       jobIOCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

type directoryStartGuard struct {
	handles []windows.Handle
}

func (g *directoryStartGuard) Close() error {
	if g == nil {
		return nil
	}
	var first error
	for i := len(g.handles) - 1; i >= 0; i-- {
		if err := windows.CloseHandle(g.handles[i]); err != nil && first == nil {
			first = err
		}
	}
	g.handles = nil
	return first
}

// PinWorkingDirectory opens root and every directory down to cwd without
// following reparse points. FILE_SHARE_DELETE is deliberately omitted, so
// rename/delete substitution is blocked until CreateProcess consumes cwd.
// Both arguments must already be spelled the same way; canonicalizing them
// here would be wrong, because following a reparse point to compare it is
// also following it past the refusal below.
func PinWorkingDirectory(root, cwd string) (StartGuard, error) {
	root = filepath.Clean(root)
	cwd = filepath.Clean(cwd)
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: cwd outside workspace", ErrInvalidSpec)
	}
	paths := []string{root}
	if rel != "." {
		current := root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			paths = append(paths, current)
		}
	}
	guard := &directoryStartGuard{}
	for _, path := range paths {
		ptr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			_ = guard.Close()
			return nil, err
		}
		h, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			_ = guard.Close()
			return nil, fmt.Errorf("pin command cwd %q: %w", path, err)
		}
		guard.handles = append(guard.handles, h)
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			_ = guard.Close()
			return nil, fmt.Errorf("inspect command cwd %q: %w", path, err)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = guard.Close()
			return nil, fmt.Errorf("%w: cwd component is not a plain directory", ErrInvalidSpec)
		}
	}
	return guard, nil
}

// utf16Environment builds the sorted, double-NUL-terminated environment
// block CreateProcess expects with CREATE_UNICODE_ENVIRONMENT.
func utf16Environment(env []string) []uint16 {
	sorted := append([]string(nil), env...)
	sort.Slice(sorted, func(i, j int) bool { return strings.ToUpper(sorted[i]) < strings.ToUpper(sorted[j]) })
	return utf16.Encode([]rune(strings.Join(sorted, "\x00") + "\x00\x00"))
}

func run(ctx context.Context, spec Spec, guard StartGuard, sink *cappedSink) (Outcome, error) {
	defer guard.Close()
	// Pipes start non-inheritable; only the child-side ends are marked
	// inheritable so the tree cannot hold handles that defeat pipe EOF.
	var inRead, inWrite, outRead, outWrite windows.Handle
	noInherit := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if err := windows.CreatePipe(&inRead, &inWrite, noInherit, 0); err != nil {
		return Outcome{}, err
	}
	defer func() {
		for _, h := range []*windows.Handle{&inRead, &inWrite, &outRead, &outWrite} {
			if *h != 0 {
				windows.CloseHandle(*h)
			}
		}
	}()
	if err := windows.CreatePipe(&outRead, &outWrite, noInherit, 0); err != nil {
		return Outcome{}, err
	}
	for _, h := range []windows.Handle{inRead, outWrite} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return Outcome{}, err
		}
	}
	var job windows.Handle
	r, _, e := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return Outcome{}, e
	}
	job = windows.Handle(r)
	defer windows.CloseHandle(job)
	limits := jobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = limitKillOnJobClose | limitActiveProcess | limitJobMemory | limitDieOnUnhandledException
	limits.BasicLimitInformation.ActiveProcessLimit = maxJobProcesses
	limits.JobMemoryLimit = jobMemoryCapBytes
	r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if r == 0 {
		return Outcome{}, e
	}
	appName, err := windows.UTF16PtrFromString(spec.Exe)
	if err != nil {
		return Outcome{}, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{spec.Exe}, spec.Args...)))
	if err != nil {
		return Outcome{}, err
	}
	cwd, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		return Outcome{}, err
	}
	var envPtr *uint16
	if block := utf16Environment(spec.Env); len(block) > 0 {
		envPtr = &block[0]
	}
	si := windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = inRead
	si.StdOutput = outWrite
	si.StdErr = outWrite
	var pi windows.ProcessInformation
	err = windows.CreateProcess(appName, cmdline, nil, nil, true, createSuspended|createNoWindow|createUnicodeEnvironment, envPtr, cwd, &si, &pi)
	_ = guard.Close()
	if err != nil {
		return Outcome{}, err
	}
	defer windows.CloseHandle(pi.Process)
	defer func() {
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
	}()
	r, _, e = procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process))
	if r == 0 {
		return Outcome{}, e
	}
	r, _, e = procResumeThread.Call(uintptr(pi.Thread))
	if r == 0xffffffff {
		return Outcome{}, e
	}
	windows.CloseHandle(pi.Thread)
	pi.Thread = 0
	// Parent copies of the child ends close now: stdin sees immediate EOF
	// and the output pipe reaches EOF once the whole tree is dead.
	windows.CloseHandle(inRead)
	inRead = 0
	windows.CloseHandle(outWrite)
	outWrite = 0
	windows.CloseHandle(inWrite)
	inWrite = 0

	output := os.NewFile(uintptr(outRead), "command-job-output")
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 32<<10)
		for {
			n, readErr := output.Read(buf)
			if n > 0 {
				sink.Write(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()

	type waitResult struct {
		code uint32
		err  error
	}
	waitDone := make(chan waitResult, 1)
	go func() {
		windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		waitResult := waitResult{err: windows.GetExitCodeProcess(pi.Process, &code)}
		waitResult.code = code
		waitDone <- waitResult
	}()

	timer := time.NewTimer(spec.Timeout)
	defer timer.Stop()
	var outcome Outcome
	select {
	case res := <-waitDone:
		if res.err != nil {
			procTerminateJobObject.Call(uintptr(job), 1)
			<-readDone
			return Outcome{}, res.err
		}
		outcome.ExitCode = int64(res.code)
	case <-timer.C:
		outcome.TimedOut = true
		outcome.ExitCode = -1
	case <-ctx.Done():
		procTerminateJobObject.Call(uintptr(job), 1)
		<-waitDone
		<-readDone
		return Outcome{}, ctx.Err()
	}
	// Every exit path kills the remaining tree before the job handle
	// closes, so grandchildren never outlive the job.
	procTerminateJobObject.Call(uintptr(job), 1)
	<-readDone
	return outcome, nil
}
