//go:build windows

package agentrunapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func stagedChangeSetFile(abs string, body []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(abs), ".lunitide-changeset-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err = f.Chmod(0o666); err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func moveChangeSetFile(abs string, body []byte, replace bool) error {
	name, err := stagedChangeSetFile(abs, body)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	from, _ := windows.UTF16PtrFromString(name)
	to, _ := windows.UTF16PtrFromString(abs)
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}

func atomicReplace(abs string, body []byte) error { return moveChangeSetFile(abs, body, true) }
func atomicCreate(abs string, body []byte) error  { return moveChangeSetFile(abs, body, false) }
func atomicDelete(abs string) error               { return os.Remove(abs) }

// changeSetPathGuard pins root and parents without SHARE_DELETE. Opening each
// component with OPEN_REPARSE_POINT rejects junction/symlink substitution.
type changeSetPathGuard struct{ handles []windows.Handle }

func guardChangeSetPath(access fsAccess, rel string) (*changeSetPathGuard, error) {
	if !validFsRelPath(rel) || !scopeAllows(access.patterns, rel) {
		return nil, ErrFsScopeDenied
	}
	g := &changeSetPathGuard{}
	current := filepath.Clean(access.root)
	// The root is pinned like every other component but is not held to the
	// reparse rule: a junction on the way *to* the workspace is the address
	// the user registered, and OneDrive hands out redirected Documents
	// folders by default. Refusing it rejected every file operation in such
	// a workspace. Only components below the root can redirect a relative
	// path out of it, and those are still opened with OPEN_REPARSE_POINT
	// and refused.
	if err := g.pin(current, true); err != nil {
		return nil, err
	}
	if parent := filepath.Dir(filepath.FromSlash(rel)); parent != "." {
		for _, component := range strings.Split(parent, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			if err := g.pin(current, false); err != nil {
				return nil, err
			}
		}
	}
	// Inspect an existing leaf without following it. Missing is expected for an
	// apply-create and for reverting an applied delete.
	leaf := filepath.Join(filepath.Clean(access.root), filepath.FromSlash(rel))
	p, err := windows.UTF16PtrFromString(leaf)
	if err != nil {
		g.Close()
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err == nil {
		var info windows.ByHandleFileInformation
		err = windows.GetFileInformationByHandle(h, &info)
		windows.CloseHandle(h)
		if err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
			g.Close()
			return nil, fmt.Errorf("reparse/non-file leaf rejected: %s", leaf)
		}
	} else if err != windows.ERROR_FILE_NOT_FOUND && err != windows.ERROR_PATH_NOT_FOUND {
		g.Close()
		return nil, err
	}
	return g, nil
}

// pin opens one directory component and holds the handle for the lifetime of
// the guard, so it cannot be swapped between this check and the write. isRoot
// follows a reparse point instead of refusing it; see guardChangeSetPath.
func (g *changeSetPathGuard) pin(dir string, isRoot bool) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		g.Close()
		return err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !isRoot {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		g.Close()
		return err
	}
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(h, &info)
	reparse := !isRoot && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	if err != nil || reparse || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		windows.CloseHandle(h)
		g.Close()
		return fmt.Errorf("reparse/non-directory component rejected: %s", dir)
	}
	g.handles = append(g.handles, h)
	return nil
}

func (g *changeSetPathGuard) Close() error {
	for i := len(g.handles) - 1; i >= 0; i-- {
		_ = windows.CloseHandle(g.handles[i])
	}
	g.handles = nil
	return nil
}
