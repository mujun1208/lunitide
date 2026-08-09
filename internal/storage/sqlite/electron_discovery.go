package sqlite

import (
	"context"
	"errors"
	"os"
)

// These names are fixed from the shipped Electron productName and the
// historical pre-productName startup path used by this repository.
var electronRoamingDirectoryNames = [...]string{"Lunitide 月汐", "lunitide"}

// RunDiscoveredElectronProviderMetadata securely discovers and imports each
// allowlisted Electron metadata source. Each source is parsed and imported
// from bytes read once from the same pinned handle.
func (s *Store) RunDiscoveredElectronProviderMetadata(ctx context.Context) ([]MetadataMigrationStatus, error) {
	sources, discoveryErr := discoverElectronProviderMetadata()
	statuses := make([]MetadataMigrationStatus, 0, len(sources))
	result := discoveryErr
	for _, source := range sources {
		status, runErr := s.runInspectedElectronProviderMetadata(ctx, source)
		if runErr != nil {
			result = errors.Join(result, runErr)
			continue
		}
		statuses = append(statuses, status)
	}
	return statuses, result
}

func isMissingElectronSource(err error) bool { return errors.Is(err, os.ErrNotExist) }
