package canonpath

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Resolve returns the operating system's own name for path. If path itself
// does not exist, the longest existing ancestor is resolved and the missing
// tail is appended, so a not-yet-created child can be compared against a
// root the same way an existing file is.
func Resolve(path string) string {
	path = filepath.Clean(path)
	if path == "" {
		return path
	}
	if resolved, err := Canonical(path); err == nil {
		return resolved
	}
	var rest []string
	cur := path
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		if resolved, err := Canonical(parent); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		cur = parent
	}
}

// Contained reports whether path is inside root after both sides are
// resolved to the same spelling. That is what makes an 8.3 temp directory
// and a symlink alias of a workspace compare equal to their long form,
// instead of looking like an escape.
func Contained(root, path string) bool {
	root = fold(Resolve(root))
	path = fold(Resolve(path))
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func fold(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
