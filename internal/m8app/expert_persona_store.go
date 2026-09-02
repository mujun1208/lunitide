package m8app

import (
	"errors"
	"os"
	"path/filepath"
)

// PersonaBodyStore addresses the persona read-only directory: the canonical
// six-section body keyed by persona_ref. Production wires the on-disk
// directory under the data root; tests use the memory store.
type PersonaBodyStore interface {
	Put(personaRef string, body []byte) error
	Get(personaRef string) ([]byte, bool, error)
}

// MemoryPersonaStore is the in-memory PersonaBodyStore (tests).
type MemoryPersonaStore struct{ m map[string][]byte }

// Put stores one body.
func (s *MemoryPersonaStore) Put(ref string, body []byte) error {
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[ref] = append([]byte(nil), body...)
	return nil
}

// Get answers one body.
func (s *MemoryPersonaStore) Get(ref string) ([]byte, bool, error) {
	b, ok := s.m[ref]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), b...), true, nil
}

// FilePersonaStore is the on-disk PersonaBodyStore: the persona read-only
// directory sharded by the first two digest hex chars (immutable files,
// one canonical six-section body per persona_ref).
type FilePersonaStore struct{ root string }

// NewFilePersonaStore wires the on-disk store over one directory.
func NewFilePersonaStore(root string) *FilePersonaStore { return &FilePersonaStore{root: root} }

// Put writes one immutable body file (digest-addressed, so an existing
// file with identical content is a no-op).
func (s *FilePersonaStore) Put(ref string, body []byte) error {
	if len(ref) != 64 {
		return ErrPayloadInvalid
	}
	dir := filepath.Join(s.root, ref[:2])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, ref+".json")
	if _, err := os.Stat(path); err == nil {
		return nil // immutable: the digest address pins the content
	}
	return os.WriteFile(path, body, 0o600)
}

// Get answers one body file.
func (s *FilePersonaStore) Get(ref string) ([]byte, bool, error) {
	if len(ref) != 64 {
		return nil, false, nil
	}
	b, err := os.ReadFile(filepath.Join(s.root, ref[:2], ref+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
