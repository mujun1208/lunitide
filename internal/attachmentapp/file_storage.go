// Package attachmentapp provides the attachment application service that
// orchestrates secure file ingestion, parsing, and context injection
// (ADR-005 §7: attachment isolation).
//
// The service owns the lifecycle of user-supplied files: validating content,
// writing to a controlled data directory via FileStorage, recording metadata
// in SQLite via Store, extracting text, and exposing parsed excerpts for
// context assembly.
package attachmentapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileStorage abstracts controlled file I/O for attachment content.
// Implementations must confine writes to a pinned, DACL-protected directory
// (datadir.SecureRoot on Windows) and reject path-traversal attempts.
//
// The name parameter is a single ordinary filename (no path separators);
// the implementation maps it to a concrete path within the secure root.
type FileStorage interface {
	// WriteFile atomically writes content to name within the secure root.
	// If a file with the same name already exists it is overwritten.
	WriteFile(ctx context.Context, name string, content []byte) error
	// ReadFile reads the full content of name within the secure root.
	// Returns an error if the file does not exist.
	ReadFile(ctx context.Context, name string) ([]byte, error)
	// DeleteFile removes name from the secure root. Idempotent: a missing
	// file is not an error.
	DeleteFile(ctx context.Context, name string) error
}

// dirFileStorage implements FileStorage using an ordinary directory path.
// This is used in production via datadir.SecureRoot.Path() and in tests
// via a temp directory. The path is expected to already be created and
// secured by the caller.
type dirFileStorage struct {
	dir string
}

// NewDirFileStorage creates a FileStorage backed by an explicit directory.
// The directory must already exist and be secured (DACL-protected on Windows).
func NewDirFileStorage(dir string) FileStorage {
	return &dirFileStorage{dir: dir}
}

func (f *dirFileStorage) WriteFile(_ context.Context, name string, content []byte) error {
	if !safeName(name) {
		return fmt.Errorf("unsafe attachment filename %q", name)
	}
	path := filepath.Join(f.dir, name)
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("write attachment file %s: %w", name, err)
	}
	return nil
}

func (f *dirFileStorage) ReadFile(_ context.Context, name string) ([]byte, error) {
	if !safeName(name) {
		return nil, fmt.Errorf("unsafe attachment filename %q", name)
	}
	path := filepath.Join(f.dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read attachment file %s: %w", name, err)
	}
	return data, nil
}

func (f *dirFileStorage) DeleteFile(_ context.Context, name string) error {
	if !safeName(name) {
		return fmt.Errorf("unsafe attachment filename %q", name)
	}
	path := filepath.Join(f.dir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete attachment file %s: %w", name, err)
	}
	return nil
}

// safeName returns true when name is a single ordinary filename with no
// path separators, drive letters, or traversal segments.
func safeName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if filepath.Base(name) != name {
		return false
	}
	for _, c := range name {
		if c == '/' || c == '\\' || c == ':' {
			return false
		}
	}
	return true
}
