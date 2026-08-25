// Package workspace: M5 T-5.2.1 path safety core. Every workspace-relative
// path passes a lexical layer (relative, no traversal, no ADS/drive
// separators, no reserved device names, no UNC/device prefixes) before the
// platform handle layer re-verifies the final path by handle. Any refusal is
// WS-002 and never touches bytes outside the root.
package workspace

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// ErrPathEscape is the WS-002 refusal: the path escapes or attempts to
// escape the workspace root (traversal, junction/symlink, ADS, UNC, device
// path, reserved name, TOCTOU).
var ErrPathEscape = errors.New("workspace: path escapes root (WS-002)")

// MaxRelPathLen bounds a workspace-relative path (schema limit 512).
const MaxRelPathLen = 512

// reservedNames are the Windows device-name aliases; a stem matching one of
// these (case-insensitive, extension stripped) resolves to a device.
var reservedNames = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true}
	for i := 1; i <= 9; i++ {
		m["COM"+itoa(i)] = true
		m["LPT"+itoa(i)] = true
	}
	return m
}()

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// ValidateRelPath is the lexical layer. It accepts clean relative paths
// using / or \ separators and rejects everything that could leave the root
// or address a stream/device/UNC target once Win32 name mangling applies.
func ValidateRelPath(rel string) error {
	if rel == "" || len(rel) > MaxRelPathLen {
		return ErrPathEscape
	}
	if strings.ContainsRune(rel, 0) {
		return ErrPathEscape
	}
	p := strings.ReplaceAll(rel, `\`, "/")
	// Absolute (/x), UNC (//server/share) and Win32 device (//./, //?/)
	// forms all begin with a separator run after normalisation.
	if strings.HasPrefix(p, "/") {
		return ErrPathEscape
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ErrPathEscape
		}
		// Any colon in a relative component is a drive separator or an
		// alternate data stream (file:stream:$DATA).
		if strings.Contains(seg, ":") {
			return ErrPathEscape
		}
		// Win32 strips trailing dots and spaces: "x." aliases "x", which can
		// smuggle a different final path past the lexical check.
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return ErrPathEscape
		}
		stem := strings.ToUpper(strings.SplitN(seg, ".", 2)[0])
		if reservedNames[stem] {
			return ErrPathEscape
		}
	}
	return nil
}

// SecureRoot pins one canonical workspace root for path-safe operations.
type SecureRoot struct {
	root string

	// The handle layer compares against a path the OS produced, which has
	// 8.3 components expanded and every reparse point followed. The root as
	// the caller spelled it has been through neither, so the two are not
	// comparable until the root is put in the same namespace: a workspace
	// under a junction — OneDrive redirects Documents this way — or under a
	// short-name profile directory otherwise fails containment on every
	// single file inside it. Resolved on first use rather than in the
	// constructor because callers routinely pin a root that WriteAtomic
	// creates later, and cached because it is on every read.
	canonMu   sync.Mutex
	canonical string
}

// NewSecureRoot validates that the root is a clean absolute local path (no
// UNC/device prefix) and returns a pinned root.
func NewSecureRoot(rootCanonical string) (*SecureRoot, error) {
	abs := filepath.Clean(rootCanonical)
	if !filepath.IsAbs(abs) {
		return nil, ErrPathEscape
	}
	if strings.HasPrefix(abs, `\\`) {
		return nil, ErrPathEscape // UNC or Win32 device prefix
	}
	return &SecureRoot{root: abs}, nil
}

// Root returns the pinned canonical root.
func (r *SecureRoot) Root() string { return r.root }

// Resolve runs the lexical layer and joins the relative path under the
// root, then re-checks containment. It does not touch the filesystem; the
// handle layer (OpenSecure) must still run before any IO.
func (r *SecureRoot) Resolve(rel string) (string, error) {
	if err := ValidateRelPath(rel); err != nil {
		return "", err
	}
	full := filepath.Join(r.root, filepath.FromSlash(rel))
	if !withinRoot(r.root, full) {
		return "", ErrPathEscape
	}
	return full, nil
}

func withinRoot(base, child string) bool {
	b := strings.TrimPrefix(filepath.Clean(base), `\\?\`)
	c := strings.TrimPrefix(filepath.Clean(child), `\\?\`)
	b = strings.ToLower(filepath.Clean(b))
	c = strings.ToLower(filepath.Clean(c))
	rel, err := filepath.Rel(b, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, `..\`)
}
