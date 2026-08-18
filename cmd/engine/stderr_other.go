//go:build !windows

package main

import "os"

// redirectStderr routes Go-level stderr into the rotating engine log. On
// non-Windows platforms stdout/stderr survive process inheritance, so the
// os.Stderr rebind is sufficient for library-level crash output.
func redirectStderr(f *os.File) {
	os.Stderr = f
}
