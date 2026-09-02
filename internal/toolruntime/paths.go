package toolruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/canonpath"
)

func (r *Runtime) effectiveSessionsRoot() string {
	if r.sessionStorageRoot != nil {
		if root, err := r.sessionStorageRoot(); err == nil && root != "" {
			return root
		}
	}
	return r.root
}

func (r *Runtime) SessionFolder(session string) (string, error) { return r.sessionRoot(session) }

// effectiveRoot returns the directory file tools operate in for this call.
// Full-access rides the user-selected workspace root when one resolves;
// everything else (and any resolver failure) keeps the per-session sandbox.
func (r *Runtime) effectiveRoot(mode Mode, session string) (string, error) {
	if mode == FullAccess && r.fullAccessRoot != nil {
		if root, err := r.fullAccessRoot(); err == nil && root != "" {
			if info, statErr := os.Lstat(root); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return root, nil
			}
		}
	}
	return r.sessionPath(session)
}

// FullAccessRootHint answers the currently resolvable user workspace root.
// Used to tell the model where file tools actually operate.
func (r *Runtime) FullAccessRootHint() (string, bool) {
	if r.fullAccessRoot == nil {
		return "", false
	}
	root, err := r.fullAccessRoot()
	if err != nil || root == "" {
		return "", false
	}
	if info, statErr := os.Lstat(root); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return root, true
}
func (r *Runtime) sessionPath(session string) (string, error) {
	if len(session) != 26 || strings.ContainsAny(session, "/\\") {
		return "", errors.New("invalid session")
	}
	return filepath.Join(r.effectiveSessionsRoot(), session), nil
}
func (r *Runtime) sessionRoot(session string) (string, error) {
	p, err := r.sessionPath(session)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0700); err != nil {
		return "", err
	}
	return p, nil
}

func (r *Runtime) path(mode Mode, session, rel string, write, unconfined bool) (string, error) {
	// Full-disk opt-in lifts the confinement for user conversations: absolute
	// paths on any drive resolve to themselves (still cleaned and
	// length-bounded), so the model can touch Desktop, other drives and any
	// user-writable location. Subagent paths pass unconfined=false and stay
	// confined to the workspace root.
	if unconfined && r.FullDiskEnabled() && rel != "" && (filepath.IsAbs(rel) || filepath.VolumeName(rel) != "") {
		clean := filepath.Clean(rel)
		if len(clean) > 4096 || strings.ContainsRune(clean, 0) {
			return "", errors.New("invalid path")
		}
		if write {
			if err := os.MkdirAll(filepath.Dir(clean), 0700); err != nil {
				return "", err
			}
		}
		return clean, nil
	}
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("relative path required")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path traversal")
	}
	root, err := r.effectiveRoot(mode, session)
	if err != nil {
		return "", err
	}
	if write {
		if err = os.MkdirAll(root, 0700); err != nil {
			return "", err
		}
	} else if info, statErr := os.Stat(root); statErr != nil {
		return "", statErr
	} else if !info.IsDir() {
		return "", errors.New("session workspace is not a directory")
	}
	p := filepath.Join(root, clean)
	// The session root itself (path ".") is a valid read target: resolve
	// symlinks and confirm it stays a directory. It is never a writable
	// target (replacing the workspace root would destroy the sandbox).
	if clean == "." {
		if write {
			return "", errors.New("workspace root is not writable")
		}
		real, err := canonpath.Canonical(p)
		if err != nil {
			return "", err
		}
		return real, nil
	}
	parent := filepath.Dir(p)
	if write {
		if err = os.MkdirAll(parent, 0700); err != nil {
			return "", err
		}
	}
	realParent, err := canonpath.Canonical(parent)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, realParent)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escape")
	}
	if !write {
		real, err := canonpath.Canonical(p)
		if err != nil {
			return "", err
		}
		relCheck, err = filepath.Rel(root, real)
		if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
			return "", errors.New("symlink escape")
		}
		p = real
	}
	return p, nil
}
