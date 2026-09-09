//go:build windows && !lunitide_e2e

package datadir

func prepareProductionOverride() (*SecureRoot, bool, error) {
	return nil, false, nil
}
