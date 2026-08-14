//go:build !windows

package agentrunapp

import (
	"os"
	"path/filepath"
)

func stagedChangeSetFile(abs string, body []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(abs), ".lunitide-changeset-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err = f.Chmod(0o666); err == nil {
		_, err = f.Write(body)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func atomicReplace(abs string, body []byte) error {
	name, err := stagedChangeSetFile(abs, body)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	return os.Rename(name, abs)
}

func atomicCreate(abs string, body []byte) error {
	name, err := stagedChangeSetFile(abs, body)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	return os.Link(name, abs)
}

func atomicDelete(abs string) error { return os.Remove(abs) }

type changeSetPathGuard struct{}

func guardChangeSetPath(_ fsAccess, _ string) (*changeSetPathGuard, error) {
	return &changeSetPathGuard{}, nil
}
func (*changeSetPathGuard) Close() error { return nil }
