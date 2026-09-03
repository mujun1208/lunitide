package datasourceapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type MemorySecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemorySecrets() *MemorySecrets {
	return &MemorySecrets{m: map[string]string{}}
}

func (s *MemorySecrets) Put(ref, dsn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[ref] = dsn
	return nil
}

func (s *MemorySecrets) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dsn, ok := s.m[ref]
	if !ok {
		return "", ErrNotFound
	}
	return dsn, nil
}

type FileSecrets struct {
	path string
	mu   sync.Mutex
}

func NewFileSecrets(path string) *FileSecrets {
	return &FileSecrets{path: path}
}

func (s *FileSecrets) Put(ref, dsn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return err
	}
	m[ref] = dsn
	return s.save(m)
}

func (s *FileSecrets) Get(ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return "", err
	}
	dsn, ok := m[ref]
	if !ok {
		return "", ErrNotFound
	}
	return dsn, nil
}

func (s *FileSecrets) load() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if errorsIsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]string{}, nil
	}
	return m, nil
}

func (s *FileSecrets) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
