//go:build !windows

// T-5.2.1 handle layer fallback: non-Windows hosts delegate containment to
// os.Root, whose Open rejects ".." traversal and refuses to cross symlinks
// out of the root.
package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// OpenSecure opens an existing regular file inside the root.
func (r *SecureRoot) OpenSecure(rel string) (*os.File, error) {
	if err := ValidateRelPath(rel); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(strings.ReplaceAll(rel, `\`, "/"))
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

// WriteAtomic stages data in a temp file then rename(2)s it into place.
func (r *SecureRoot) WriteAtomic(rel string, data []byte, perm os.FileMode) error {
	if err := ValidateRelPath(rel); err != nil {
		return err
	}
	full := filepath.Join(r.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
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
	return os.Rename(name, full)
}
