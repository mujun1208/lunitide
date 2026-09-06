// Package bootstrap holds the engine startup wiring extracted from
// cmd/engine/main.go so that main() stays a thin entry point and the
// individual startup helpers can be unit-tested in isolation. This first
// slice only carries the dependency-free helpers; the heavier service
// wiring is migrated here incrementally (A-03).
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/ipc"
)

// CompactionRecoveryError folds a compaction restart-recovery pass into a
// single fail-closed error: the top-level scanner error takes precedence,
// otherwise the first per-checkpoint reconciliation failure is surfaced.
// A nil return means every checkpoint reconciled cleanly.
func CompactionRecoveryError(results []compactionapp.RecoveryResult, recoveryErr error) error {
	if recoveryErr != nil {
		return recoveryErr
	}
	for _, result := range results {
		if result.Err != nil {
			return fmt.Errorf("checkpoint %s reconciliation incomplete: %w", result.CheckpointID, result.Err)
		}
	}
	return nil
}

// ShutdownAfterSession cancels the engine context only when the RPC session
// ended because the handshake ACK could not be written. A clean owner
// disconnect must leave the engine running for other clients.
func ShutdownAfterSession(err error, cancel context.CancelFunc) {
	if errors.Is(err, ipc.ErrHandshakeACK) {
		cancel()
	}
}

// ReadWorkspaceRoot parses the host-written workspace-root.json. The heavy
// validation (fixed drive, no reparse point) already happened in the host
// picker; here we re-check existence and directory-ness so a stale config
// degrades to the sandbox instead of a confusing write error.
func ReadWorkspaceRoot(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > 4096 {
		return "", errors.New("invalid workspace root config")
	}
	var c struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &c); err != nil || c.Path == "" || len(c.Path) > 1024 {
		return "", errors.New("invalid workspace root config")
	}
	clean := filepath.Clean(c.Path)
	if !filepath.IsAbs(clean) || strings.HasPrefix(clean, `\\`) {
		return "", errors.New("invalid workspace root path")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("workspace root is not a plain directory")
	}
	return clean, nil
}