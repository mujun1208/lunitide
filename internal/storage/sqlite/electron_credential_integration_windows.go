//go:build windows && integration

package sqlite

import (
	"context"
	"path/filepath"
)

// VisitElectronCredentialFileForIntegration uses the production parser and
// candidate extraction on an explicit test corpus. This symbol is excluded
// from normal builds so production discovery remains Known-Folder-only.
func VisitElectronCredentialFileForIntegration(ctx context.Context, path string, visit func(ElectronCredentialCandidate) error) error {
	source, err := inspectElectronFile(path)
	if err != nil {
		return err
	}
	source.osCryptEncryptedKey, err = readElectronOSCryptKey(filepath.Dir(path), filepath.Dir(path))
	if err != nil {
		return err
	}
	return visitElectronCredentialSources(ctx, []inspectedElectronFile{source}, visit)
}
