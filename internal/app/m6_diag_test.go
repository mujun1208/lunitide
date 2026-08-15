package app

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/extension"
	"github.com/lunitide/lunitide/internal/m6app"
)

func TestM6DiagInstallError(t *testing.T) {
	h := newM6Harness(t)
	digest := sha256Hex("diag-bytes")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", digest), digest, m6supply.RiskLow)
	verifier := &extension.Verifier{
		TrustedKeys:       map[string]ed25519.PublicKey{"m6-test-key": h.pub},
		RevokedKeys:       map[string]bool{},
		RevokedPublishers: map[string]bool{},
		RevokedDigests:    map[string]bool{},
		Policy:            extension.Policy{LicenseAllowlist: []string{"MIT"}, RuntimeAllowlist: []string{"wasm"}},
	}
	svc := m6app.NewExtensionService(h.repo, verifier)
	res, err := svc.Install(context.Background(), "diag-key", "tester", "local-user", "personal", id, "1.2.0",
		extension.GrantDecision{Granted: []string{"fs.read"}, ConfirmedDeltaDigest: extension.DeltaDigest([]string{"fs.read"})})
	t.Logf("res=%+v err=%+v", res, err)
	if err != nil {
		t.Fatal(err)
	}
}
