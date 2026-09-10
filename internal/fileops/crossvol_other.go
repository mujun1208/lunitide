//go:build !windows

package fileops

import (
	"errors"
	"path/filepath"
	"syscall"
)

func isCrossVolume(err error) bool {
	return errors.Is(unwrapLink(err), syscall.EXDEV)
}

func isCrossVolumePath(from, to string) bool {
	d1, ok1 := volumeDevice(from)
	d2, ok2 := volumeDevice(to)
	return ok1 && ok2 && d1 != d2
}

func volumeDevice(p string) (uint64, bool) {
	p = resolveForVolume(p)
	var st syscall.Stat_t
	for i := 0; i < 8; i++ {
		if err := syscall.Stat(p, &st); err == nil {
			return uint64(st.Dev), true
		}
		next := filepath.Dir(p)
		if next == p {
			return 0, false
		}
		p = next
	}
	return 0, false
}
