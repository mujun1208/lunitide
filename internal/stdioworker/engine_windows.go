//go:build windows

package stdioworker

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Production spawn engine (5B) — a Job Object envelope per run:
// kill-on-close (host death reaps the tree), active-process and commit
// quotas, explicit sorted environment block (the parent environment never
// leaks), suspended start → assign → resume so no child escapes the job.

var (
	swCreateJobObjectW         = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateJobObjectW")
	swSetInformationJobObject  = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetInformationJobObject")
	swAssignProcessToJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("AssignProcessToJobObject")
	swTerminateJobObject       = windows.NewLazySystemDLL("kernel32.dll").NewProc("TerminateJobObject")
	swResumeThread             = windows.NewLazySystemDLL("kernel32.dll").NewProc("ResumeThread")
)

const (
	swJobObjectExtendedLimitInformation = 9

	swLimitActiveProcess           = 0x00000008
	swLimitDieOnUnhandledException = 0x00000400
	swLimitJobMemory               = 0x00000200
	swLimitKillOnJobClose          = 0x00002000

	swCreateSuspended          = 0x00000004
	swCreateUnicodeEnvironment = 0x00000400
	swCreateNoWindow           = 0x08000000
)

type swJobBasicLimit struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}

type swJobIOCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type swJobExtendedLimit struct {
	BasicLimitInformation                                                        swJobBasicLimit
	IoInfo                                                                       swJobIOCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

// engineProc is the OS handle bundle of one spawned run.
type engineProc struct {
	stdin  *os.File
	stdout *os.File
	pid    int
	job    windows.Handle
	proc   windows.Handle

	killOnce bool
}

// engineSpawn launches cmd under a fresh Job Object with explicit env.
func engineSpawn(cmd string, args []string, dir string, env []string, q Quotas) (*engineProc, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	noInherit := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if err := windows.CreatePipe(&inRead, &inWrite, noInherit, 0); err != nil {
		return nil, fmt.Errorf("stdioworker: stdin pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, noInherit, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("stdioworker: stdout pipe: %w", err)
	}
	for _, h := range []windows.Handle{inRead, outWrite} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			windows.CloseHandle(inRead)
			windows.CloseHandle(inWrite)
			windows.CloseHandle(outRead)
			windows.CloseHandle(outWrite)
			return nil, fmt.Errorf("stdioworker: inherit pipe: %w", err)
		}
	}

	r, _, e := swCreateJobObjectW.Call(0, 0)
	if r == 0 {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("stdioworker: CreateJobObject: %w", e)
	}
	job := windows.Handle(r)
	limits := swJobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = swLimitKillOnJobClose | swLimitActiveProcess | swLimitJobMemory | swLimitDieOnUnhandledException
	limits.BasicLimitInformation.ActiveProcessLimit = q.MaxProcs
	limits.JobMemoryLimit = uintptr(q.MemoryCapBytes)
	r, _, e = swSetInformationJobObject.Call(uintptr(job), swJobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if r == 0 {
		windows.CloseHandle(job)
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("stdioworker: SetInformationJobObject: %w", e)
	}

	appName, err := windows.UTF16PtrFromString(cmd)
	if err != nil {
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{cmd}, args...)))
	if err != nil {
		return nil, err
	}
	cwd, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	envPtr := (*uint16)(nil)
	if block := swEnvBlock(env); len(block) > 0 {
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
		swCreateSuspended|swCreateNoWindow|swCreateUnicodeEnvironment, envPtr, cwd, &si, &pi); err != nil {
		windows.CloseHandle(job)
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("stdioworker: CreateProcess: %w", err)
	}
	if r, _, e := swAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process)); r == 0 {
		swTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(job)
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("stdioworker: AssignProcessToJobObject: %w", e)
	}
	if r, _, _ := swResumeThread.Call(uintptr(pi.Thread)); r == 0xffffffff {
		swTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(job)
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("stdioworker: ResumeThread failed")
	}
	windows.CloseHandle(pi.Thread)

	// Parent copies of child ends close so EOF semantics work.
	stdin := os.NewFile(uintptr(inWrite), "sw-stdin")
	stdout := os.NewFile(uintptr(outRead), "sw-stdout")
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)

	return &engineProc{
		stdin:  stdin,
		stdout: stdout,
		pid:    int(pi.ProcessId),
		job:    job,
		proc:   pi.Process,
	}, nil
}

// kill terminates the whole tree.
func (p *engineProc) kill() error {
	if p == nil || p.killOnce {
		return nil
	}
	p.killOnce = true
	if p.job != 0 {
		if r, _, e := swTerminateJobObject.Call(uintptr(p.job), 1); r == 0 {
			return fmt.Errorf("stdioworker: TerminateJobObject: %w", e)
		}
	}
	return nil
}

// close releases the OS handles (fires kill-on-close as the last resort).
func (p *engineProc) close() {
	if p == nil {
		return
	}
	p.kill()
	_ = p.stdin.Close()
	_ = p.stdout.Close()
	if p.proc != 0 {
		windows.CloseHandle(p.proc)
		p.proc = 0
	}
	if p.job != 0 {
		windows.CloseHandle(p.job)
		p.job = 0
	}
}

// wait blocks for the root process exit; ctx cancellation kills the tree.
func (p *engineProc) wait(ctx context.Context) (int, error) {
	type res struct {
		code uint32
		err  error
	}
	done := make(chan res, 1)
	go func() {
		windows.WaitForSingleObject(p.proc, windows.INFINITE)
		var code uint32
		err := windows.GetExitCodeProcess(p.proc, &code)
		done <- res{code: code, err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return -1, r.err
		}
		return int(r.code), nil
	case <-ctx.Done():
		_ = p.kill()
		r := <-done
		_ = r
		return -1, ctx.Err()
	}
}

// swEnvBlock builds the sorted, double-NUL-terminated environment block.
func swEnvBlock(env []string) []uint16 {
	sorted := append([]string(nil), env...)
	sort.Slice(sorted, func(i, j int) bool { return strings.ToUpper(sorted[i]) < strings.ToUpper(sorted[j]) })
	return utf16.Encode([]rune(strings.Join(sorted, "\x00") + "\x00\x00"))
}
