//go:build windows && !lunitide_e2e

package datadir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func prepareProductionOverride() (*SecureRoot, bool, error) {
	raw := strings.TrimSpace(os.Getenv("LUNITIDE_DATA_ROOT"))
	if raw == "" {
		return nil, false, nil
	}
	if !filepath.IsAbs(raw) {
		return nil, true, fmt.Errorf("LUNITIDE_DATA_ROOT must be an absolute path")
	}
	root, err := PrepareForTest(raw)
	return root, true, err
}
