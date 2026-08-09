package sqlite

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

// VisitDiscoveredElectronCredentials reuses pinned allowlisted discovery and
// strict parsing. It exposes no path and performs no source writes.
func VisitDiscoveredElectronCredentials(ctx context.Context, visit func(ElectronCredentialCandidate) error) error {
	if visit == nil {
		return errors.New("Electron credential visitor is required")
	}
	sources, result := discoverElectronProviderMetadata()
	return errors.Join(result, visitElectronCredentialSources(ctx, sources, visit))
}

func visitElectronCredentialSources(ctx context.Context, sources []inspectedElectronFile, visit func(ElectronCredentialCandidate) error) (result error) {
	for _, source := range sources {
		for i, item := range source.providers {
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			if source.encrypted[i] == "" {
				continue
			}
			origin, err := provider.NormalizeOrigin(item.BaseURL)
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			candidate := ElectronCredentialCandidate{SourceFingerprint: source.fingerprint, ItemFingerprint: source.itemFP[i], ProviderID: item.ID, LegacyID: item.LegacyID, SourceVersion: source.version, Origin: origin, Protocol: item.Protocol, LegacyProtocol: source.legacyProtocols[i], EncryptedBlob: source.encrypted[i], OSCryptEncryptedKey: source.osCryptEncryptedKey}
			if err = visit(candidate); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}
