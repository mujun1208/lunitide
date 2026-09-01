// Package conversationsapp manages the user-configurable root directory
// where each chat session gets its own subfolder for tool outputs.
package conversationsapp

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"
)

type rootConfig struct {
	Path string `json:"path"`
}

// Store reads/writes conversations-root.json under the app data directory.
type Store struct {
	configPath string
	legacyRoot string
	mu         sync.Mutex
}

func New(configPath, legacyToolWorkspacesRoot string) *Store {
	return &Store{configPath: configPath, legacyRoot: legacyToolWorkspacesRoot}
}

type Status struct {
	Path       string `json:"path"`
	Configured bool   `json:"configured"`
	LegacyPath string `json:"legacyPath,omitempty"`
}

func (s *Store) Status() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok, err := s.readLocked()
	if err != nil {
		return Status{}, err
	}
	out := Status{LegacyPath: s.legacyRoot}
	if ok {
		out.Path = cfg.Path
		out.Configured = true
	}
	return out, nil
}

func (s *Store) SetRoot(path string) (migrated int, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, errors.New("conversations root path is empty")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, errors.New("conversations root must be an existing directory")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return 0, err
	}
	migrated, err = s.migrateLegacy(path)
	if err != nil {
		return migrated, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(rootConfig{Path: path})
	return migrated, atomicWrite(s.configPath, data)
}

func (s *Store) EffectiveRoot() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok, err := s.readLocked()
	if err != nil {
		return "", false, err
	}
	if !ok || strings.TrimSpace(cfg.Path) == "" {
		return s.legacyRoot, false, nil
	}
	return cfg.Path, true, nil
}

func (s *Store) SessionDir(sessionID string) (string, error) {
	if err := validSessionID(sessionID); err != nil {
		return "", err
	}
	root, _, err := s.EffectiveRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, sessionID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Store) migrateLegacy(destRoot string) (int, error) {
	entries, err := os.ReadDir(s.legacyRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := validSessionID(name); err != nil {
			continue
		}
		src := filepath.Join(s.legacyRoot, name)
		dst := filepath.Join(destRoot, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyTree(src, dst); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func validSessionID(id string) error {
	if len(id) != 26 || strings.ContainsAny(id, `/\`) {
		return errors.New("invalid session id")
	}
	if _, err := ulid.ParseStrict(id); err != nil {
		return errors.New("invalid session id")
	}
	return nil
}

func (s *Store) readLocked() (rootConfig, bool, error) {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return rootConfig{}, false, nil
		}
		return rootConfig{}, false, err
	}
	var cfg rootConfig
	if json.Unmarshal(data, &cfg) != nil || strings.TrimSpace(cfg.Path) == "" {
		return rootConfig{}, false, nil
	}
	cfg.Path = filepath.Clean(cfg.Path)
	return cfg, true, nil
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".conv-root-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
