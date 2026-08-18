//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// redirectStderr points the Win32 STD_ERROR_HANDLE at the rotating engine log.
// Go runtime fatal errors (concurrent map write, out of memory, deadlock)
// write their traceback directly to the OS stderr handle via GetStdHandle at
// write time, so replacing only the os.Stderr variable would lose them — and
// a GUI subsystem process inherits no usable console handle from the host in
// the first place, which is why these crashes previously vanished without a
// trace. Re-binding the standard handle here makes the runtime crash output
// land in the same file as the regular engine diagnostics.
func redirectStderr(f *os.File) {
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}
