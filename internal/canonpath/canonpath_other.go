//go:build !windows

package canonpath

import "path/filepath"

// EvalSymlinks is the right answer off Windows: there are no short-name
// aliases, and it resolves symlinks and reports ErrNotExist for a missing
// path, which is exactly the contract Canonical documents.
func canonical(path string) (string, error) { return filepath.EvalSymlinks(path) }
