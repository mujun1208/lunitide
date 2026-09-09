//go:build windows && lunitide_e2e

package datadir

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func prepareProductionOverride() (*SecureRoot, bool, error) {
	known, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return nil, true, fmt.Errorf("resolve e2e LocalAppData: %w", err)
	}
	root, err := PrepareForTest(filepath.Join(known, "Lunitide-E2E"))
	return root, true, err
}
