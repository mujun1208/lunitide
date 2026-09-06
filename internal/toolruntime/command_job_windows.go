//go:build windows

package toolruntime

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// superviseProcessTree assigns an already-started command.run child to a fresh
// Job Object configured with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (S-03). The
// context timeout on exec.CommandContext only terminates the direct child; any
// grandchildren it spawned (a shell that launched a daemon, a build that forked
// a watcher) outlive that kill and escape the deadline. Pinning the whole tree
// to a Job Object means the returned closer — run after Wait, or on
// timeout/cancel via defer — reaps every process still in the job when the last
// handle closes. This mirrors internal/command/job.go's attachJobObject.
//
// It is best-effort: if the job cannot be created or the process already exited
// the closer is a no-op and the caller continues, so a Job Object failure never
// blocks a command that would otherwise run.
func superviseProcessTree(cmd *exec.Cmd) func() {
	noop := func() {}
	if cmd == nil || cmd.Process == nil {
		return noop
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return noop
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return noop
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return noop
	}
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(h)
		_ = windows.CloseHandle(job)
		return noop
	}
	_ = windows.CloseHandle(h)
	return func() {
		// Closing the final job handle terminates every process still assigned
		// to it (KILL_ON_JOB_CLOSE), reaping grandchildren that survived the
		// context-driven kill of the direct child.
		_ = windows.CloseHandle(job)
	}
}