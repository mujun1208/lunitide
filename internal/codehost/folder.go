package codehost

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Folder is the directory the editor has open. Writes stay inside it.
type Folder struct {
	mu    sync.Mutex
	root  string
	saved map[string][]byte
}

func OpenFolder(root string) (*Folder, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, errors.New("code root is not a directory")
	}
	return &Folder{root: abs, saved: map[string][]byte{}}, nil
}

func (f *Folder) Write(rel, content string) error {
	abs, err := f.confine(rel)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.saved[abs]; !ok {
		body, readErr := os.ReadFile(abs)
		if readErr == nil {
			f.saved[abs] = append([]byte(nil), body...)
		} else if os.IsNotExist(readErr) {
			f.saved[abs] = nil
		} else {
			return readErr
		}
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0600)
}

func (f *Folder) Diffs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for abs, old := range f.saved {
		body, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(f.root, abs)
		b.WriteString(unified(rel, string(old), string(body)))
	}
	return b.String()
}

func (f *Folder) Accept() int {
	f.mu.Lock()
	n := len(f.saved)
	f.saved = map[string][]byte{}
	f.mu.Unlock()
	return n
}

func (f *Folder) Restore() (int, error) {
	f.mu.Lock()
	saved := f.saved
	f.saved = map[string][]byte{}
	f.mu.Unlock()
	n := 0
	for abs, old := range saved {
		if old == nil {
			if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
				return n, err
			}
		} else if err := os.WriteFile(abs, old, 0600); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (f *Folder) confine(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("path required")
	}
	if filepath.IsAbs(rel) {
		relRel, err := filepath.Rel(f.root, rel)
		if err != nil {
			return "", err
		}
		rel = relRel
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path traversal")
	}
	return filepath.Join(f.root, clean), nil
}

func unified(name, before, after string) string {
	var b strings.Builder
	b.WriteString("--- " + name + "\n+++ " + name + "\n")
	for _, line := range strings.Split(before, "\n") {
		b.WriteString("-" + line + "\n")
	}
	for _, line := range strings.Split(after, "\n") {
		b.WriteString("+" + line + "\n")
	}
	return b.String()
}

// WriteSourceLine replaces one 1-based line and writes the file.
func WriteSourceLine(path string, line int, text string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(body), "\n")
	if line < 1 || line > len(lines) {
		return errors.New("line out of range")
	}
	lines[line-1] = text
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600)
}
