//go:build windows

package agenthub

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

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
	limitActiveProcess                = 0x00000008
	limitDieOnUnhandledException      = 0x00000400
	limitJobMemory                    = 0x00000200
	limitKillOnJobClose               = 0x00002000
	createSuspended                   = 0x00000004
	createUnicodeEnvironment          = 0x00000400
	createNoWindow                    = 0x08000000
	maxJobProcesses                   = 64
	jobMemoryCapBytes                 = 4 << 30
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

func startProcess(ctx context.Context, spec ProcSpec, onLine func(string)) (int64, bool, error) {
	if spec.Exe == "" || !filepath.IsAbs(spec.Exe) || spec.Dir == "" || !filepath.IsAbs(spec.Dir) {
		return 0, false, fmt.Errorf("进程路径无效")
	}
	spec.Timeout = clampTimeout(spec.Timeout)
	var inRead, inWrite, outRead, outWrite windows.Handle
	noInherit := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if err := windows.CreatePipe(&inRead, &inWrite, noInherit, 0); err != nil {
		return 0, false, err
	}
	defer closeHandle(&inRead)
	defer closeHandle(&inWrite)
	if err := windows.CreatePipe(&outRead, &outWrite, noInherit, 0); err != nil {
		return 0, false, err
	}
	defer closeHandle(&outRead)
	defer closeHandle(&outWrite)
	for _, h := range []windows.Handle{inRead, outWrite} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return 0, false, err
		}
	}
	r, _, e := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return 0, false, e
	}
	job := windows.Handle(r)
	defer windows.CloseHandle(job)
	limits := jobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = limitKillOnJobClose | limitActiveProcess | limitJobMemory | limitDieOnUnhandledException
	limits.BasicLimitInformation.ActiveProcessLimit = maxJobProcesses
	limits.JobMemoryLimit = jobMemoryCapBytes
	if r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); r == 0 {
		return 0, false, e
	}
	appName, err := windows.UTF16PtrFromString(spec.Exe)
	if err != nil {
		return 0, false, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{spec.Exe}, spec.Args...)))
	if err != nil {
		return 0, false, err
	}
	cwd, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		return 0, false, err
	}
	block := utf16Environment(os.Environ())
	var envPtr *uint16
	if len(block) > 0 {
		envPtr = &block[0]
	}
	si := windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES, StdInput: inRead, StdOutput: outWrite, StdErr: outWrite}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err = windows.CreateProcess(appName, cmdline, nil, nil, true, createSuspended|createNoWindow|createUnicodeEnvironment, envPtr, cwd, &si, &pi); err != nil {
		return 0, false, err
	}
	defer windows.CloseHandle(pi.Process)
	defer func() {
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
	}()
	assigned, _, assignErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process))
	if assigned == 0 {
		_ = windows.TerminateProcess(pi.Process, 1)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		return 0, false, assignErr
	}
	if err = ctx.Err(); err != nil {
		return 0, false, err
	}
	if r, _, e = procResumeThread.Call(uintptr(pi.Thread)); r == 0xffffffff {
		return 0, false, e
	}
	windows.CloseHandle(pi.Thread)
	pi.Thread = 0
	closeHandle(&inRead)
	closeHandle(&outWrite)
	if spec.Stdin != nil {
		file := os.NewFile(uintptr(inWrite), "agent-hub-stdin")
		inWrite = 0
		if err = writeAndClose(file, spec.Stdin); err != nil {
			procTerminateJobObject.Call(uintptr(job), 1)
			return 0, false, err
		}
	} else {
		closeHandle(&inWrite)
	}
	output := os.NewFile(uintptr(outRead), "agent-hub-output")
	outRead = 0
	defer output.Close()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		br := bufio.NewReaderSize(output, 64<<10)
		for {
			line, readErr := br.ReadBytes('\n')
			if len(line) > 0 && onLine != nil {
				onLine(strings.TrimRight(string(line), "\r\n"))
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
		err := windows.GetExitCodeProcess(pi.Process, &code)
		waitDone <- waitResult{code: code, err: err}
	}()
	timer := time.NewTimer(spec.Timeout)
	defer timer.Stop()
	var timedOut bool
	var exit int64
	waited := false
	select {
	case res := <-waitDone:
		waited = true
		if res.err != nil {
			procTerminateJobObject.Call(uintptr(job), 1)
			<-readDone
			return 0, false, res.err
		}
		exit = int64(res.code)
	case <-timer.C:
		timedOut = true
		exit = -1
	case <-ctx.Done():
		procTerminateJobObject.Call(uintptr(job), 1)
		<-waitDone
		<-readDone
		return 0, false, ctx.Err()
	}
	procTerminateJobObject.Call(uintptr(job), 1)
	if !waited {
		<-waitDone
	}
	<-readDone
	return exit, timedOut, nil
}

func closeHandle(h *windows.Handle) {
	if h != nil && *h != 0 {
		windows.CloseHandle(*h)
		*h = 0
	}
}

func utf16Environment(env []string) []uint16 {
	return utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
}

func startPersistent(ctx context.Context, spec ProcSpec) (*PersistentProc, error) {
	if spec.Exe == "" || !filepath.IsAbs(spec.Exe) || spec.Dir == "" || !filepath.IsAbs(spec.Dir) {
		return nil, fmt.Errorf("进程路径无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var inRead, inWrite, outRead, outWrite, errRead, errWrite windows.Handle
	noInherit := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if err := windows.CreatePipe(&inRead, &inWrite, noInherit, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outRead, &outWrite, noInherit, 0); err != nil {
		closeHandle(&inRead)
		closeHandle(&inWrite)
		return nil, err
	}
	if err := windows.CreatePipe(&errRead, &errWrite, noInherit, 0); err != nil {
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		return nil, err
	}
	for _, h := range []windows.Handle{inRead, outWrite, errWrite} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			closeHandle(&inRead)
			closeHandle(&inWrite)
			closeHandle(&outRead)
			closeHandle(&outWrite)
			closeHandle(&errRead)
			closeHandle(&errWrite)
			return nil, err
		}
	}
	r, _, e := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, e
	}
	job := windows.Handle(r)
	limits := jobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = limitKillOnJobClose | limitActiveProcess | limitJobMemory | limitDieOnUnhandledException
	limits.BasicLimitInformation.ActiveProcessLimit = maxJobProcesses
	limits.JobMemoryLimit = jobMemoryCapBytes
	if r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); r == 0 {
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, e
	}
	appName, err := windows.UTF16PtrFromString(spec.Exe)
	if err != nil {
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{spec.Exe}, spec.Args...)))
	if err != nil {
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, err
	}
	cwd, err := windows.UTF16PtrFromString(spec.Dir)
	if err != nil {
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, err
	}
	block := utf16Environment(os.Environ())
	var envPtr *uint16
	if len(block) > 0 {
		envPtr = &block[0]
	}
	si := windows.StartupInfo{Flags: windows.STARTF_USESTDHANDLES, StdInput: inRead, StdOutput: outWrite, StdErr: errWrite}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err = windows.CreateProcess(appName, cmdline, nil, nil, true, createSuspended|createNoWindow|createUnicodeEnvironment, envPtr, cwd, &si, &pi); err != nil {
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, err
	}
	assigned, _, assignErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process))
	if assigned == 0 {
		_ = windows.TerminateProcess(pi.Process, 1)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		windows.CloseHandle(pi.Process)
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, assignErr
	}
	if err = ctx.Err(); err != nil {
		procTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, err
	}
	if r, _, e = procResumeThread.Call(uintptr(pi.Thread)); r == 0xffffffff {
		procTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(job)
		closeHandle(&inRead)
		closeHandle(&inWrite)
		closeHandle(&outRead)
		closeHandle(&outWrite)
		closeHandle(&errRead)
		closeHandle(&errWrite)
		return nil, e
	}
	windows.CloseHandle(pi.Thread)
	closeHandle(&inRead)
	closeHandle(&outWrite)
	closeHandle(&errWrite)
	logs := &safeLogBuf{}
	stderrFile := os.NewFile(uintptr(errRead), "agent-hub-acp-stderr")
	go func() {
		_, _ = io.Copy(logs, stderrFile)
		_ = stderrFile.Close()
	}()
	stop := make(chan struct{})
	proc := &PersistentProc{
		stdin:  os.NewFile(uintptr(inWrite), "agent-hub-acp-stdin"),
		stdout: os.NewFile(uintptr(outRead), "agent-hub-acp-stdout"),
		logs:   logs,
	}
	proc.closer = func() error {
		close(stop)
		procTerminateJobObject.Call(uintptr(job), 1)
		windows.CloseHandle(pi.Process)
		windows.CloseHandle(job)
		return nil
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = proc.Close()
		case <-stop:
		}
	}()
	return proc, nil
}
