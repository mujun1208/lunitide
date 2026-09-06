package m8app

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// PersonaBodyStore addresses the persona read-only directory: the canonical
// six-section body keyed by persona_ref. Production wires the on-disk
// directory under the data root; tests use the memory store.
type PersonaBodyStore interface {
	Put(personaRef string, body []byte) error
	Get(personaRef string) ([]byte, bool, error)
}

// MemoryPersonaStore is the in-memory PersonaBodyStore (tests).
type MemoryPersonaStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

// Put stores one body.
func (s *MemoryPersonaStore) Put(ref string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[ref] = append([]byte(nil), body...)
	return nil
}

// Get answers one body.
func (s *MemoryPersonaStore) Get(ref string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.m[ref]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), b...), true, nil
}

// FilePersonaStore is the on-disk PersonaBodyStore: the persona read-only
// directory sharded by the first two digest hex chars (immutable files,
// one canonical six-section body per persona_ref).
type FilePersonaStore struct {
	root     string
	mu       sync.RWMutex
	gcShard  int
	gcOffset int
}

// NewFilePersonaStore wires the on-disk store over one directory.
func NewFilePersonaStore(root string) *FilePersonaStore { return &FilePersonaStore{root: root} }

// Put writes one immutable body file (digest-addressed, so an existing
// file with identical content is a no-op).
func (s *FilePersonaStore) Put(ref string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validPersonaRef(ref) || len(body) > maxPersonaBodyBytes {
		return ErrPayloadInvalid
	}
	dir := filepath.Join(s.root, ref[:2])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, ref+".json")
	if existing, err := readPersonaFile(path); err == nil {
		if !bytes.Equal(existing, body) {
			return ErrExpertBodyUnavailable
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(dir, ".persona-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Get answers one body file.
func (s *FilePersonaStore) Get(ref string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !validPersonaRef(ref) {
		return nil, false, ErrPayloadInvalid
	}
	b, err := readPersonaFile(filepath.Join(s.root, ref[:2], ref+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// Includes the worst-case JSON escaping of six 64 KiB sections.
const maxPersonaBodyBytes = 4 << 20

func readPersonaFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPersonaBodyBytes {
		return nil, ErrExpertBodyUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPersonaBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPersonaBodyBytes {
		return nil, ErrExpertBodyUnavailable
	}
	return b, nil
}

func validPersonaRef(ref string) bool {
	if len(ref) != 64 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}
