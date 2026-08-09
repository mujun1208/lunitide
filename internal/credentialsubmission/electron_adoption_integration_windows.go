//go:build windows && integration

package credentialsubmission

import (
	"context"

	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// RunElectronCredentialAdoptionForIntegration injects only candidate discovery;
// decryption, planning, DPAPI storage, adoption, and recovery stay production-real.
// It is excluded from normal builds.
func (h *HostHandler) RunElectronCredentialAdoptionForIntegration(ctx context.Context, discover func(context.Context, func(storage.ElectronCredentialCandidate) error) error) error {
	return h.runElectronCredentialAdoption(ctx, discover)
}
