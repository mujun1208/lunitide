package sqlite

// DiscoveredSource is a public view of a discovered Electron provider file.
type DiscoveredSource struct {
	Version    string
	Providers  int
	Fingerprint string
}

// DiscoverElectronSources returns discovered Electron legacy sources with public fields.
func DiscoverElectronSources() ([]DiscoveredSource, error) {
	sources, err := discoverElectronProviderMetadata()
	if err != nil {
		return nil, err
	}
	result := make([]DiscoveredSource, len(sources))
	for i, src := range sources {
		result[i] = DiscoveredSource{
			Version:     src.version,
			Providers:   len(src.providers),
			Fingerprint: src.fingerprint,
		}
	}
	return result, nil
}
