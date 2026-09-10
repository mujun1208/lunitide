package fileops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func mapRenameError(err error) error {
	if err == nil {
		return nil
	}
	if isCrossVolume(err) {
		return fmt.Errorf("%w", ErrCrossVolume)
	}
	if os.IsNotExist(err) || os.IsNotExist(unwrapLink(err)) {
		return ErrSourceMissing
	}
	return err
}

func unwrapLink(err error) error {
	var link *os.LinkError
	if errors.As(err, &link) {
		return link.Err
	}
	return err
}

func rejectCrossVolumePaths(from, to string) error {
	if isCrossVolumePath(from, to) {
		return ErrCrossVolume
	}
	return nil
}

func resolveForVolume(p string) string {
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := filepath.Dir(p)
	if dir == p {
		return p
	}
	return filepath.Join(resolveForVolume(dir), filepath.Base(p))
}
