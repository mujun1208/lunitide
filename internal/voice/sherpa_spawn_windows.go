package voice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Starting the recognizer.
//
// Two things have to be true of this child that are not true by default.
// It must not flash a console window — it is started mid-conversation, and a
// black rectangle appearing over the companion is not acceptable. And it must
// not outlive us: it holds the model in memory, so an orphan is a few hundred
// megabytes the user cannot account for and cannot find. The Job Object with
// KILL_ON_JOB_CLOSE is how the rest of this codebase reaps process trees, and
// it works even when we die without running any cleanup.

const createNoWindow = 0x08000000

func spawnServer(executable string, args []string, port int) (*sherpaServer, error) {
	if _, err := os.Stat(executable); err != nil {
		return nil, fmt.Errorf("%w: recognizer not installed: %v", ErrBackendUnavailable, err)
	}

	// Before the child ever listens, so the firewall has a rule to match
	// against and never raises its prompt.
	ensureFirewallRule(executable)

	log := newTailBuffer(8 << 10)
	cmd := exec.Command(executable, args...)
	// The runtime's DLLs sit beside the executable, and Windows searches the
	// working directory before most of the path.
	cmd.Dir = filepath.Dir(executable)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: start recognizer: %v", ErrBackendUnavailable, err)
	}
	if err := confineToJob(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	// Reaping the child keeps ProcessState accurate, which is what tells a
	// later turn the server died rather than that its port went quiet.
	go func() { _ = cmd.Wait() }()

	return &sherpaServer{cmd: cmd, port: port, log: log}, nil
}

func (s *sherpaServer) stop() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
}

// confineToJob puts the child in a job that dies when our last handle to it
// closes, which includes the case where this process is killed outright.
//
// The handle is deliberately never closed: holding it open is the mechanism.
// It is released when the process exits, and that release is what terminates
// the child.
func confineToJob(pid int) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("voice: create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("voice: configure job object: %w", err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("voice: open recognizer process: %w", err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("voice: confine recognizer: %w", err)
	}
	return nil
}
