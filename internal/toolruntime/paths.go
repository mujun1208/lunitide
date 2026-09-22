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
	if r.projectRoot != nil {
		if root, err := r.projectRoot(session); err == nil && root != "" {
			if pinned, ok := pinExistingDir(root); ok {
				return pinned, nil
			}
		}
	}
	if mode == FullAccess && r.fullAccessRoot != nil {
		if root, err := r.fullAccessRoot(); err == nil && root != "" {
			if pinned, ok := pinExistingDir(root); ok {
				return pinned, nil
			}
		}
	}
	return r.sessionPath(session)
}

// pinExistingDir accepts a real directory (not a symlink) and returns the
// operating system's own spelling of it. A project root from t.TempDir on
// hosted Windows is often the 8.3 alias; containment compares a resolved
// child against this value and must not mix the two spellings.
func pinExistingDir(root string) (string, bool) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	real, err := canonpath.Canonical(root)
	if err != nil {
		return "", false
	}
	return real, true
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
	return pinExistingDir(root)
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
	// Full-access reads resolve an absolute path the user gave (Desktop,
	// Documents, other drives) to itself. Writes still require the separate
	// full-disk opt-in; a confined call (subagent, approval replay) cannot
	// write outside the workspace even when that opt-in is on.
	absolute := rel != "" && (filepath.IsAbs(rel) || filepath.VolumeName(rel) != "")
	readAnywhere := absolute && !write && (mode == FullAccess || (unconfined && r.FullDiskEnabled()))
	writeAnywhere := absolute && write && unconfined && r.FullDiskEnabled()
	if readAnywhere || writeAnywhere {
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
	// effectiveRoot already pins project/full-access roots, but the session
	// sandbox path is joined from the caller's spelling. Canonicalize before
	// Rel so an 8.3 root is not compared to a long-form child.
	if realRoot, canonErr := canonpath.Canonical(root); canonErr == nil {
		root = realRoot
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
