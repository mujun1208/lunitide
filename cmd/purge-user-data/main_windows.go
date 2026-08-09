//go:build windows

// purge-user-data is an intentionally argument-free maintenance command.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const dataDirectoryName = "Lunitide"

type fileID struct{ volume, high, low uint32 }

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "purge-user-data accepts no arguments")
		os.Exit(2)
	}
	known, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err == nil {
		err = purge(known, filepath.Join(known, dataDirectoryName))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "safe Lunitide data purge failed:", err)
		os.Exit(1)
	}
}

// The parameters are private test seams. The executable exposes no path input.
func purge(known, target string) error {
	if known == "" || !filepath.IsAbs(known) {
		return fmt.Errorf("invalid LocalAppData known-folder path")
	}
	want := filepath.Join(filepath.Clean(known), dataDirectoryName)
	if !samePath(target, want) {
		return fmt.Errorf("refusing non-Lunitide target %q", target)
	}
	root, info, err := openPinned(target)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pin data root: %w", err)
	}
	defer func() {
		if root != windows.InvalidHandle {
			_ = windows.CloseHandle(root)
		}
	}()
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("data root is not an ordinary directory")
	}
	knownFinal, err := finalPathForDirectory(known)
	if err != nil {
		return fmt.Errorf("verify LocalAppData: %w", err)
	}
	rootFinal, err := finalPath(root)
	if err != nil {
		return fmt.Errorf("resolve pinned root: %w", err)
	}
	if !samePath(rootFinal, filepath.Join(knownFinal, dataDirectoryName)) || !within(knownFinal, rootFinal) {
		return fmt.Errorf("data root escaped or is not exact Lunitide directory: %q", rootFinal)
	}
	rootID := identity(info)
	if err := removeChildren(root, target, rootFinal); err != nil {
		return err
	}
	if err := requireIdentity(root, rootID, true, false); err != nil {
		return fmt.Errorf("root identity changed: %w", err)
	}
	if err := markDelete(root); err != nil {
		return fmt.Errorf("delete data root: %w", err)
	}
	if err := windows.CloseHandle(root); err != nil {
		return fmt.Errorf("close deleted data root: %w", err)
	}
	root = windows.InvalidHandle
	return nil
}

func removeChildren(dir windows.Handle, path, rootFinal string) error {
	before, err := handleInfo(dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("enumerate %q: %w", path, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("unsafe directory entry %q", name)
		}
		childPath := filepath.Join(path, name)
		child, childInfo, err := openPinned(childPath)
		if err != nil {
			return fmt.Errorf("pin child %q: %w", childPath, err)
		}
		childID := identity(childInfo)
		childFinal, finalErr := finalPath(child)
		if finalErr != nil || !within(rootFinal, childFinal) {
			windows.CloseHandle(child)
			if finalErr != nil {
				return fmt.Errorf("resolve child %q: %w", childPath, finalErr)
			}
			return fmt.Errorf("child escaped data root: %q", childFinal)
		}
		isReparse := childInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
		isDir := childInfo.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
		if isDir && !isReparse {
			err = removeChildren(child, childPath, rootFinal)
		}
		if err == nil {
			err = requireIdentity(child, childID, isDir, isReparse)
		}
		if err == nil {
			err = markDelete(child)
		}
		closeErr := windows.CloseHandle(child)
		if err != nil {
			return fmt.Errorf("remove child %q: %w", childPath, err)
		}
		if closeErr != nil {
			return fmt.Errorf("close deleted child %q: %w", childPath, closeErr)
		}
		if err := requireIdentity(dir, identity(before), true, false); err != nil {
			return fmt.Errorf("parent identity changed while deleting %q: %w", childPath, err)
		}
	}
	return nil
}

func openPinned(path string) (windows.Handle, windows.ByHandleFileInformation, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, windows.ByHandleFileInformation{}, err
	}
	// OPEN_REPARSE_POINT avoids following the opened component. No SHARE_DELETE
	// pins its name; DELETE allows handle-relative disposition.
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES|windows.DELETE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, windows.ByHandleFileInformation{}, err
	}
	info, err := handleInfo(h)
	if err != nil {
		windows.CloseHandle(h)
		return 0, windows.ByHandleFileInformation{}, err
	}
	return h, info, nil
}

func handleInfo(h windows.Handle) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(h, &info)
	return info, err
}
func identity(info windows.ByHandleFileInformation) fileID {
	return fileID{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow}
}
func requireIdentity(h windows.Handle, want fileID, directory, reparse bool) error {
	got, err := handleInfo(h)
	if err != nil {
		return err
	}
	if identity(got) != want {
		return fmt.Errorf("file identity changed")
	}
	if (got.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (got.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0) != reparse {
		return fmt.Errorf("file type changed")
	}
	return nil
}
func markDelete(h windows.Handle) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
}
func finalPathForDirectory(path string) (string, error) {
	h, _, err := openPinned(path)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return finalPath(h)
}
func finalPath(h windows.Handle) (string, error) {
	b := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &b[0], uint32(len(b)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(b)) {
			return filepath.Clean(windows.UTF16ToString(b[:n])), nil
		}
		b = make([]uint16, n+1)
	}
}
func normalized(path string) string { return strings.TrimPrefix(filepath.Clean(path), `\\?\`) }
func samePath(a, b string) bool     { return strings.EqualFold(normalized(a), normalized(b)) }
func within(base, child string) bool {
	rel, err := filepath.Rel(normalized(base), normalized(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, `..\`)
}
