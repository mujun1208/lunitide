//go:build !windows

package electronsafestorage

import (
	"errors"
	"testing"
)

func TestNonWindowsFailsClosed(t *testing.T) {
	called := false
	err := WithAPIKey("AAAA", "https://example.test", "openai", func([]byte) error { called = true; return nil })
	if !errors.Is(err, ErrUnavailable) || called {
		t.Fatalf("non-Windows decrypt did not fail closed: called=%v err=%v", called, err)
	}
}
