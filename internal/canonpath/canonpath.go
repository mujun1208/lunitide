// Package canonpath answers one question the same way everywhere: what is
// this path's real name, the one the operating system itself would print?
//
// Every containment check in the tree compares a child against a root, and
// the comparison only means anything if both sides are spelled the same way.
// On Windows one file has several legal spellings: 8.3 short components
// (C:\Users\RUNNER~1\...), and reparse points such as the junction that
// OneDrive leaves behind when it redirects Documents.
//
// filepath.EvalSymlinks is not a safe way to settle this on Windows. It
// expands short components, so comparing its output against a root that was
// never expanded reports an escape that did not happen; and it fails with
// ErrNotExist on any path that traverses a junction, so a workspace under a
// redirected folder looks empty rather than readable. Both were shipping:
// the first denied every file under a short-name temp directory, the second
// denied every file under a redirected Documents folder.
//
// Canonical resolves through a real handle on Windows, which is the only
// spelling the kernel treats as authoritative.
package canonpath

// Canonical returns the operating system's own name for an existing path:
// absolute, with short components expanded and reparse points followed.
//
// It follows reparse points on purpose. Callers use it to establish where a
// path really leads, which is what a containment test needs on both sides.
// Refusing a redirected path is a separate decision, and the handle guards
// that make it still apply it only below the root they are protecting.
//
// The path must exist. A missing path reports an error satisfying
// errors.Is(err, os.ErrNotExist), the same as filepath.EvalSymlinks.
func Canonical(path string) (string, error) { return canonical(path) }
