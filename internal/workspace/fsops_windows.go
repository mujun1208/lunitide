//go:build windows

// T-5.2.1 handle layer (Windows): after the lexical layer each parent
// component is opened with OPEN_REPARSE_POINT so a junction/symlink
// substitution is refused before the leaf handle exists; the opened leaf is
// then re-verified via GetFinalPathNameByHandle and must still be inside
// the root. Writes stage a temp file and MoveFileEx-replace atomically.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/canonpath"
	"golang.org/x/sys/windows"
)

// OpenSecure opens an existing regular file inside the root after full
// handle verification. It refuses reparse leaves (WS-002).
func (r *SecureRoot) OpenSecure(rel string) (*os.File, error) {
	full, err := r.Resolve(rel)
	if err != nil {
		return nil, err
	}
	g, err := guardParents(r.root, rel)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	p, err := windows.UTF16PtrFromString(full)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	// Leaf reparse check (symlink/junction hard link escape).
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(h)
		return nil, ErrPathEscape
	}
	// Final path by handle defeats 8.3 short names and any remaining alias.
	final, err := finalPathByHandle(h)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if !withinRoot(r.canonicalRootPath(), final) {
		windows.CloseHandle(h)
		return nil, ErrPathEscape
	}
	return os.NewFile(uintptr(h), full), nil
}

// StatSecure stats a path inside the root with the same guarantees.
func (r *SecureRoot) StatSecure(rel string) (os.FileInfo, error) {
	f, err := r.OpenSecure(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// WriteAtomic stages data in a temp file next to the target then
// MoveFileEx-replaces it (WRITE_THROUGH) so readers never observe a torn
// file and a crash never leaves a half-written target.
func (r *SecureRoot) WriteAtomic(rel string, data []byte, perm os.FileMode) error {
	full, err := r.Resolve(rel)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	g, err := guardParents(r.root, rel)
	if err != nil {
		return err
	}
	defer g.Close()
	f, err := os.CreateTemp(filepath.Dir(full), ".lunitide-ws-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err = f.Chmod(perm); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return err
	}
	from, _ := windows.UTF16PtrFromString(name)
	to, _ := windows.UTF16PtrFromString(full)
	defer os.Remove(name)
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH|windows.MOVEFILE_REPLACE_EXISTING)
}

// canonicalRootPath answers the root the way the OS names it, so it can be
// compared against a GetFinalPathNameByHandle result.
//
// The root is opened without OPEN_REPARSE_POINT on purpose. A junction on the
// way *to* the workspace is the address the user chose and has to be followed;
// only reparse points below the root can redirect a relative path out of it,
// and guardParents still refuses every one of those.
//
// Falls back to the spelled root when the root cannot be opened, which means
// it does not exist yet — and then no leaf inside it exists either, so the
// comparison this feeds is unreachable.
func (r *SecureRoot) canonicalRootPath() string {
	r.canonMu.Lock()
	defer r.canonMu.Unlock()
	if r.canonical != "" {
		return r.canonical
	}
	final, err := canonpath.Canonical(r.root)
	if err != nil {
		return r.root
	}
	r.canonical = final
	return final
}

// parentGuard pins every directory component with OPEN_REPARSE_POINT so a
// swapped-in junction between check and open cannot redirect the leaf.
type parentGuard struct{ handles []windows.Handle }

func guardParents(root, rel string) (*parentGuard, error) {
	g := &parentGuard{}
	current := filepath.Clean(root)
	p := strings.ReplaceAll(rel, `\`, "/")
	var components []string
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		components = strings.Split(p[:idx], "/")
	}
	for _, component := range components {
		current = filepath.Join(current, component)
		cp, err := windows.UTF16PtrFromString(current)
		if err != nil {
			g.Close()
			return nil, err
		}
		h, err := windows.CreateFile(cp, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			g.Close()
			return nil, err
		}
		var info windows.ByHandleFileInformation
		if err = windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			windows.CloseHandle(h)
			g.Close()
			return nil, fmt.Errorf("%w: reparse/non-directory component %s", ErrPathEscape, current)
		}
		g.handles = append(g.handles, h)
	}
	return g, nil
}

func (g *parentGuard) Close() error {
	for i := len(g.handles) - 1; i >= 0; i-- {
		_ = windows.CloseHandle(g.handles[i])
	}
	g.handles = nil
	return nil
}

func finalPathByHandle(h windows.Handle) (string, error) {
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
