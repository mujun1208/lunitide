package extension

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

// sigHarness signs a well-formed manifest with a fresh ed25519 key so every
// test case starts from a fully valid candidate and mutates exactly one
// property (T-6.1.3 DoD: 12 malicious cases, EXT-001..004 each >= 3).
type sigHarness struct {
	keyID    string
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	verifier *Verifier
}

func newHarness(t *testing.T) sigHarness {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := sigHarness{keyID: "m6-control-1", pub: pub, priv: priv}
	h.verifier = &Verifier{
		TrustedKeys:       map[string]ed25519.PublicKey{h.keyID: pub},
		RevokedKeys:       map[string]bool{},
		RevokedPublishers: map[string]bool{},
		RevokedDigests:    map[string]bool{},
		Policy:            Policy{LicenseAllowlist: []string{"MIT", "Apache-2.0"}, RuntimeAllowlist: []string{"wasm"}},
	}
	return h
}

func (h sigHarness) artifact(mutate func(m *Manifest)) Artifact {
	m := Manifest{
		SchemaVersion: "1", SkillID: "skill-pdf", Name: "pdf-tools", Version: "1.2.0",
		Publisher: "acme", Description: "PDF helpers", Entrypoint: "main.wasm",
		InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
		Permissions: []string{"fs.read", "fs.write"}, Runtime: "wasm", MinHostVersion: "6.0.0",
		License: "MIT", SBOMRef: "sbom://acme/pdf-tools/1.2.0", Triggers: []string{"onInvoke"},
		Dependencies:   []Dependency{{Name: "pdf-lib", Version: "3.0.1", Digest: sha256Hex("pdf-lib")}},
		TimeoutMS:      30000,
		ResourceLimits: ResourceLimits{CPUMillis: 500, MemoryMB: 128, DiskMB: 64, Processes: 1, Network: false},
	}
	m.ArtifactDigest = sha256Hex("artifact-bytes")
	m.Signature = Signature{KeyID: h.keyID, Alg: "ed25519"}
	if mutate != nil {
		mutate(&m)
	}
	// Re-sign whatever the manifest now says so tampering tests can clear it.
	if m.Signature.Value == "" {
		content, err := signedContent(&m)
		if err != nil {
			panic(err)
		}
		m.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(h.priv, content))
	}
	// Artifact.Digest is the hash of the bytes on disk and stays independent
	// of the manifest pin so mismatch cases can diverge the two.
	return Artifact{Publisher: m.Publisher, Name: m.Name, Version: m.Version, Digest: sha256Hex("artifact-bytes"), Manifest: m}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func grantFor(perms []string, deltaDigest string) GrantDecision {
	return GrantDecision{Granted: perms, ConfirmedDeltaDigest: deltaDigest}
}

// EXT-001 (1/3): artifact bytes digest diverges from the manifest pin.
func TestRejectDigestMismatch(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) { m.ArtifactDigest = sha256Hex("other-bytes") })
	// digest field mutated after signing: signature no longer covers it, but
	// digest check fires first — assert EXT-001 diagnostics mention mismatch.
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodeDigestSignature || !errors.Is(v.Err, ErrDigestMismatch) {
		t.Fatalf("want EXT-001 digest mismatch, got %+v", v)
	}
}

// EXT-001 (2/3): manifest body tampered after signing — signature invalid.
func TestRejectTamperedManifest(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) {
		content, err := signedContent(m)
		if err != nil {
			t.Fatal(err)
		}
		m.Version = "9.9.9" // tamper after computing signature input
		m.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(h.priv, content))
	})
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodeDigestSignature || !errors.Is(v.Err, ErrSignatureInvalid) {
		t.Fatalf("want EXT-001 signature invalid, got %+v", v)
	}
}

// EXT-001 (3/3): keyId not in the trusted key set.
func TestRejectUnknownKeyID(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) { m.Signature.KeyID = "rogue-key" })
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodeDigestSignature || !errors.Is(v.Err, ErrSignatureInvalid) {
		t.Fatalf("want EXT-001 unknown key, got %+v", v)
	}
}

// EXT-002 (1/3): publisher on the revocation list.
func TestRejectRevokedPublisher(t *testing.T) {
	h := newHarness(t)
	h.verifier.RevokedPublishers["acme"] = true
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || !v.Blocked || v.Code != CodeRevoked || !errors.Is(v.Err, ErrArtifactRevoked) {
		t.Fatalf("want EXT-002 revoked publisher, got %+v", v)
	}
}

// EXT-002 (2/3): artifact digest revoked (publisher withdrew the version).
func TestRejectRevokedArtifactDigest(t *testing.T) {
	h := newHarness(t)
	h.verifier.RevokedDigests[sha256Hex("artifact-bytes")] = true
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || !v.Blocked || v.Code != CodeRevoked {
		t.Fatalf("want EXT-002 revoked digest, got %+v", v)
	}
}

// EXT-002 (3/3): signing key revoked (key compromise).
func TestRejectRevokedKey(t *testing.T) {
	h := newHarness(t)
	h.verifier.RevokedKeys[h.keyID] = true
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || !v.Blocked || v.Code != CodeRevoked {
		t.Fatalf("want EXT-002 revoked key, got %+v", v)
	}
}

// EXT-003 (1/3): sbomRef empty.
func TestRejectMissingSBOMRef(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) { m.SBOMRef = "" })
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodePolicyRejected || !errors.Is(v.Err, ErrSBOMRefMissing) {
		t.Fatalf("want EXT-003 missing sbom, got %+v", v)
	}
}

// EXT-003 (2/3): dependency without digest pin.
func TestRejectUnlockedDependency(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) {
		m.Dependencies = []Dependency{{Name: "pdf-lib", Version: "3.0.1", Digest: ""}}
	})
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodePolicyRejected || !errors.Is(v.Err, ErrDependencyUnlocked) {
		t.Fatalf("want EXT-003 unlocked dependency, got %+v", v)
	}
}

// EXT-003 (3/3): license outside the allowlist.
func TestRejectLicenseNotAllowed(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) { m.License = "GPL-3.0" })
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodePolicyRejected || !errors.Is(v.Err, ErrLicenseRejected) {
		t.Fatalf("want EXT-003 license rejected, got %+v", v)
	}
}

// EXT-004 (1/3): granted permission the manifest never declared.
func TestRejectUndeclaredPermission(t *testing.T) {
	h := newHarness(t)
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read", "command.run"}, ""), nil)
	if v.Code != CodePermissionDelta || !errors.Is(v.Err, ErrPermissionUndeclared) {
		t.Fatalf("want EXT-004 undeclared permission, got %+v", v)
	}
}

// EXT-004 (2/3): upgrade adds permissions, confirmation digest wrong.
func TestRejectUnconfirmedDelta(t *testing.T) {
	h := newHarness(t)
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read", "fs.write"}, sha256Hex("wrong-delta")), []string{"fs.read"})
	if v.Code != CodePermissionDelta || !errors.Is(v.Err, ErrDeltaUnconfirmed) {
		t.Fatalf("want EXT-004 unconfirmed delta, got %+v", v)
	}
}

// EXT-004 (3/3): upgrade adds permissions, no confirmation digest at all.
func TestRejectMissingDeltaConfirmation(t *testing.T) {
	h := newHarness(t)
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read", "fs.write"}, ""), []string{"fs.read"})
	if v.Code != CodePermissionDelta || !errors.Is(v.Err, ErrDeltaUnconfirmed) {
		t.Fatalf("want EXT-004 missing confirmation, got %+v", v)
	}
}

// SKL-001: manifest missing required fields quarantines before any step.
func TestRejectManifestMissingFields(t *testing.T) {
	h := newHarness(t)
	a := h.artifact(func(m *Manifest) { m.TimeoutMS = 0 })
	v := h.verifier.Verify(a, grantFor([]string{"fs.read"}, ""), nil)
	if !v.Quarantined || v.Code != CodeManifestInvalid || !errors.Is(v.Err, ErrManifestMissingFields) {
		t.Fatalf("want SKL-001 manifest invalid, got %+v", v)
	}
}

// Positive: fresh install passes when the full permission set is confirmed.
func TestVerifyAcceptsFreshInstall(t *testing.T) {
	h := newHarness(t)
	confirm := DeltaDigest([]string{"fs.read"})
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read"}, confirm), nil)
	if v.Quarantined || v.Code != "" || v.Err != nil {
		t.Fatalf("fresh install should pass, got %+v", v)
	}
}

// Positive: upgrade whose delta is confirmed with the canonical digest.
func TestVerifyAcceptsConfirmedUpgrade(t *testing.T) {
	h := newHarness(t)
	delta := PermissionDelta([]string{"fs.read", "fs.write"}, []string{"fs.read"})
	v := h.verifier.Verify(h.artifact(nil), grantFor([]string{"fs.read", "fs.write"}, DeltaDigest(delta)), []string{"fs.read"})
	if v.Quarantined || v.Code != "" || v.Err != nil {
		t.Fatalf("confirmed upgrade should pass, got %+v", v)
	}
}
