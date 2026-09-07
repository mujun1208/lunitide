//go:build !windows

package atomicfile

import "os"

func Replace(from, to string) error { return os.Rename(from, to) }
