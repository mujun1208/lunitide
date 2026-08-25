//go:build windows

package canonpath

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func canonical(path string) (string, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", &os.PathError{Op: "canonical", Path: path, Err: err}
	}
	// FILE_READ_ATTRIBUTES is enough to name a file and is granted where
	// read access is not. BACKUP_SEMANTICS lets directories open. No
	// OPEN_REPARSE_POINT: the caller is asking where this path leads, so
	// the link has to be followed rather than described.
	h, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &os.PathError{Op: "canonical", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, 512)
	for {
		// Flags 0 is FILE_NAME_NORMALIZED|VOLUME_NAME_DOS: the long,
		// drive-lettered form.
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", &os.PathError{Op: "canonical", Path: path, Err: err}
		}
		if n >= uint32(len(buf)) {
			buf = make([]uint16, n+1)
			continue
		}
		return trimExtendedPrefix(filepath.Clean(windows.UTF16ToString(buf[:n]))), nil
	}
}

// trimExtendedPrefix drops the \\?\ escape the kernel prefixes, so results
// compare equal to the ordinary paths callers hold. A UNC result comes back
// as \\?\UNC\server\share and folds to \\server\share.
func trimExtendedPrefix(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	return strings.TrimPrefix(p, `\\?\`)
}
