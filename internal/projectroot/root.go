package projectroot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("project root path is invalid")
	ErrBusy     = errors.New("project root path is busy")
	ErrReadonly = errors.New("project root path is not writable")
)

type Lock struct {
	ProjectID   string `json:"projectId"`
	ProjectCode string `json:"projectCode"`
	Name        string `json:"name"`
	BoundAt     string `json:"boundAt"`
}

func IsInvalid(err error) bool { return errors.Is(err, ErrInvalid) }
func IsBusy(err error) bool    { return errors.Is(err, ErrBusy) }
func IsReadonly(err error) bool {
	return errors.Is(err, ErrReadonly)
}

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalid
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", ErrInvalid
	}
	cleaned := filepath.Clean(abs)
	if len(cleaned) > 1024 {
		return "", ErrInvalid
	}
	return cleaned, nil
}

func Probe(root string) error {
	root, err := Normalize(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return ErrInvalid
	}
	meta := filepath.Join(root, ".lunitide")
	if err = os.MkdirAll(meta, 0o755); err != nil {
		return ErrReadonly
	}
	probe := filepath.Join(meta, ".write-probe")
	if err = os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return ErrReadonly
	}
	_ = os.Remove(probe)
	return nil
}

func lockPath(root string) string {
	return filepath.Join(root, ".lunitide", "project.json")
}

func ReadLock(root string) (Lock, error) {
	var lock Lock
	root, err := Normalize(root)
	if err != nil {
		return lock, err
	}
	raw, err := os.ReadFile(lockPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return lock, nil
		}
		return lock, err
	}
	if err = json.Unmarshal(raw, &lock); err != nil {
		return Lock{}, ErrBusy
	}
	return lock, nil
}

func WriteLock(root string, lock Lock) error {
	root, err := Normalize(root)
	if err != nil {
		return err
	}
	if err = Probe(root); err != nil {
		return err
	}
	if lock.BoundAt == "" {
		lock.BoundAt = time.Now().UTC().Format(time.RFC3339)
	}
	body, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath(root), body, 0o644)
}

func Bind(root string, lock Lock) error {
	root, err := Normalize(root)
	if err != nil {
		return err
	}
	if err = Probe(root); err != nil {
		return err
	}
	existing, err := ReadLock(root)
	if err != nil {
		return err
	}
	if existing.ProjectID != "" && existing.ProjectID != lock.ProjectID {
		return ErrBusy
	}
	return WriteLock(root, lock)
}

func RemoveLock(root string) error {
	root, err := Normalize(root)
	if err != nil {
		return err
	}
	err = os.Remove(lockPath(root))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
