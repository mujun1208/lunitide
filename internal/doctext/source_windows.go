//go:build windows

package doctext

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/commandworker"
	"golang.org/x/sys/windows"
)

// Only ordinary local-drive files are accepted. Check the real drive type,
// pin each parent without traversing junctions, then open the leaf without
// following a reparse point or sharing writes/deletes during the snapshot read.
func openSource(path string) (*os.File, error) {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || strings.Contains(path[2:], ":") {
		return nil, errors.New("文档必须是本地磁盘文件")
	}
	drive := volume + string(filepath.Separator)
	drivePtr, err := windows.UTF16PtrFromString(drive)
	if err != nil {
		return nil, err
	}
	switch windows.GetDriveType(drivePtr) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_CDROM, windows.DRIVE_RAMDISK:
	default:
		return nil, errors.New("不支持网络盘或不可用的文档磁盘")
	}
	guard, err := commandworker.PinWorkingDirectory(drive, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer guard.Close()
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(ptr, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(handle)
		return nil, ErrUnsupportedFormat
	}
	return os.NewFile(uintptr(handle), path), nil
}
