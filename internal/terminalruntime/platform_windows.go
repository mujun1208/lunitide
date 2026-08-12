//go:build windows

package terminalruntime

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var (
	procCreatePseudoConsole      = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole      = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole       = kernel32.NewProc("ClosePseudoConsole")
	procCreateJobObject          = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procResumeThread             = kernel32.NewProc("ResumeThread")
)

const (
	procThreadAttributePseudoConsole  = 0x00020016
	extendedStartupInfoPresent        = 0x00080000
	createSuspended                   = 0x00000004
	createUnicodeEnvironment          = 0x00000400
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
)

type coord struct{ X, Y int16 }
type ioCounters struct{ ReadOperationCount, WriteOperationCount, OtherOperationCount, ReadTransferCount, WriteTransferCount, OtherTransferCount uint64 }
type jobBasicLimit struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}
type jobExtendedLimit struct {
	BasicLimitInformation                                                        jobBasicLimit
	IoInfo                                                                       ioCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

type winSession struct {
	mu      sync.Mutex
	input   *os.File
	output  *os.File
	pseudo  windows.Handle
	job     windows.Handle
	process windows.Handle
	closed  bool
}

func startPlatform(root string, cols, rows uint16, onOutput func([]byte), onExit func(uint32, error)) (platformSession, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	pipeSecurity := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	if err := windows.CreatePipe(&inRead, &inWrite, pipeSecurity, 0); err != nil {
		return nil, err
	}
	defer func() {
		if inRead != 0 {
			windows.CloseHandle(inRead)
		}
		if inWrite != 0 {
			windows.CloseHandle(inWrite)
		}
		if outRead != 0 {
			windows.CloseHandle(outRead)
		}
		if outWrite != 0 {
			windows.CloseHandle(outWrite)
		}
	}()
	if err := windows.CreatePipe(&outRead, &outWrite, pipeSecurity, 0); err != nil {
		return nil, err
	}
	var pseudo windows.Handle
	r, _, e := procCreatePseudoConsole.Call(uintptr(*(*uint32)(unsafe.Pointer(&coord{int16(cols), int16(rows)}))), uintptr(inRead), uintptr(outWrite), 0, uintptr(unsafe.Pointer(&pseudo)))
	if r != 0 {
		return nil, e
	}
	defer func() {
		if pseudo != 0 {
			procClosePseudoConsole.Call(uintptr(pseudo))
		}
	}()
	windows.CloseHandle(inRead)
	inRead = 0
	windows.CloseHandle(outWrite)
	outWrite = 0
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	pseudoAttributeValue := *(*unsafe.Pointer)(unsafe.Pointer(&pseudo))
	if err = attrs.Update(procThreadAttributePseudoConsole, pseudoAttributeValue, unsafe.Sizeof(pseudo)); err != nil {
		return nil, err
	}
	var job windows.Handle
	r, _, e = procCreateJobObject.Call(0, 0)
	if r == 0 {
		return nil, e
	}
	job = windows.Handle(r)
	defer func() {
		if job != 0 {
			windows.CloseHandle(job)
		}
	}()
	limits := jobExtendedLimit{}
	limits.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if r == 0 {
		return nil, e
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	for _, dir := range []string{
		filepath.Join(root, ".terminal-tmp"),
		filepath.Join(root, "AppData", "Local"),
		filepath.Join(root, "AppData", "Roaming"),
	} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	exe := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	appName, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{exe, "-NoLogo", "-NoProfile"}))
	if err != nil {
		return nil, err
	}
	cwd, _ := windows.UTF16PtrFromString(root)
	var envPtr *uint16
	if env := utf16Environment(sanitizedEnvironment(systemRoot, root)); len(env) > 0 {
		envPtr = &env[0]
	}
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()
	var pi windows.ProcessInformation
	processSecurity := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	threadSecurity := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	err = windows.CreateProcess(appName, cmdline, processSecurity, threadSecurity, false, extendedStartupInfoPresent|createSuspended|createUnicodeEnvironment, envPtr, cwd, &si.StartupInfo, &pi)
	if err != nil {
		return nil, err
	}
	defer func() {
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
	}()
	defer func() {
		if pi.Process != 0 {
			windows.CloseHandle(pi.Process)
		}
	}()
	r, _, e = procAssignProcessToJobObject.Call(uintptr(job), uintptr(pi.Process))
	if r == 0 {
		return nil, e
	}
	r, _, e = procResumeThread.Call(uintptr(pi.Thread))
	if r == 0xffffffff {
		return nil, e
	}
	windows.CloseHandle(pi.Thread)
	pi.Thread = 0
	s := &winSession{input: os.NewFile(uintptr(inWrite), "conpty-input"), output: os.NewFile(uintptr(outRead), "conpty-output"), pseudo: pseudo, job: job, process: pi.Process}
	inWrite = 0
	outRead = 0
	pseudo = 0
	job = 0
	pi.Process = 0
	go s.readLoop(onOutput)
	go s.waitLoop(onExit)
	return s, nil
}
func utf16Environment(env []string) []uint16 {
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	u := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	return u
}
func (s *winSession) readLoop(cb func([]byte)) {
	b := make([]byte, 32<<10)
	for {
		n, e := s.output.Read(b)
		if n > 0 {
			cb(b[:n])
		}
		if e != nil {
			return
		}
	}
}
func (s *winSession) waitLoop(cb func(uint32, error)) {
	e, waitErr := windows.WaitForSingleObject(s.process, windows.INFINITE)
	var code uint32
	err := windows.GetExitCodeProcess(s.process, &code)
	if waitErr != nil {
		err = waitErr
	} else if e != windows.WAIT_OBJECT_0 && err == nil {
		err = errors.New("process wait failed")
	}
	_ = windows.CloseHandle(s.process)
	cb(code, err)
}
func (s *winSession) write(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	_, e := s.input.Write(b)
	return e
}
func (s *winSession) resize(c, r uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	v := coord{int16(c), int16(r)}
	x, _, e := procResizePseudoConsole.Call(uintptr(s.pseudo), uintptr(*(*uint32)(unsafe.Pointer(&v))))
	if x != 0 {
		return e
	}
	return nil
}
func (s *winSession) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	in, out, pseudo, job := s.input, s.output, s.pseudo, s.job
	s.input = nil
	s.output = nil
	s.pseudo = 0
	s.job = 0
	s.mu.Unlock()
	if in != nil {
		_ = in.Close()
	}
	if job != 0 {
		_ = windows.CloseHandle(job)
	}
	if pseudo != 0 {
		procClosePseudoConsole.Call(uintptr(pseudo))
	}
	if out != nil {
		_ = out.Close()
	}
	return nil
}
