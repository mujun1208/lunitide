//go:build windows

package datadir

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

const MaintenanceLockFile = ".maintenance.lock"

var ErrMaintenanceBusy = errors.New("data directory is in use; exit the desktop and engine before maintenance")

// DataLock uses Windows sharing semantics, so process termination releases it
// even after a crash. Both host and engine hold a shared handle for their entire
// lifetime; maintenance holds the only exclusive handle. The file is never
// removed or included in snapshots, preventing stale-lock/inode replacement.
type DataLock struct {
	mu sync.Mutex
	h  windows.Handle
}

func (r *SecureRoot) AcquireDataLock(exclusive bool) (*DataLock, error) {
	path, err := r.FilePath(MaintenanceLockFile)
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access, share := uint32(windows.GENERIC_READ), uint32(windows.FILE_SHARE_READ)
	if exclusive {
		access, share = windows.GENERIC_READ|windows.GENERIC_WRITE, 0
	}
	h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_ALWAYS, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return nil, ErrMaintenanceBusy
	}
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("unsafe maintenance lock file: %w", errors.Join(err, errors.New("ordinary single-link file required")))
	}
	return &DataLock{h: h}, nil
}

func (l *DataLock) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.h == 0 {
		return nil
	}
	err := windows.CloseHandle(l.h)
	l.h = 0
	return err
}
