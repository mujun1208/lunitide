package fileops

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
)

func isCrossVolume(err error) bool {
	var errno syscall.Errno
	return errors.As(unwrapLink(err), &errno) && errno == 17
}

func isCrossVolumePath(from, to string) bool {
	from, to = resolveForVolume(from), resolveForVolume(to)
	vf, vt := filepath.VolumeName(from), filepath.VolumeName(to)
	return vf != "" && vt != "" && !strings.EqualFold(vf, vt)
}
