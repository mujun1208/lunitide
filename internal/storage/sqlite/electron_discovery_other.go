//go:build !windows

package sqlite

// Electron legacy discovery is Windows-only. Keeping this stub allows the
// engine and parser tests to build on other development platforms.
func discoverElectronProviderMetadata() ([]inspectedElectronFile, error) { return nil, nil }
