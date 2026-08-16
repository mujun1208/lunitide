package market

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func ringWithKey(t *testing.T) (*KeyRing, *SignerFixture) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ring := NewKeyRing()
	if err := ring.AddKey("k1", pub); err != nil {
		t.Fatal(err)
	}
	return ring, &SignerFixture{keyID: "k1", priv: priv}
}

type SignerFixture struct {
	keyID string
	priv  ed25519.PrivateKey
}

// signManifest signs the canonical manifest digest with the fixture key.
func (s *SignerFixture) signManifest(m Manifest) Signature {
	return Signature{KeyID: s.keyID, SigHex: hex.EncodeToString(ed25519.Sign(s.priv, []byte(ManifestDigest(m))))}
}

func sampleManifest() Manifest {
	return Manifest{
		PackageID: "pkg-aml-check", OrgID: "01JDPOLICYORG00000001", Name: "AML 检查能力包",
		Version: "1.2.0", Permissions: []string{"workspace.read", "workspace.write"},
		ContentDigest: "aa" + "11",
	}
}

func bodyDigestOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func publishSample(t *testing.T, reg *Registry, fixture *SignerFixture) *Package {
	t.Helper()
	m := sampleManifest()
	m.ContentDigest = bodyDigestOf("package-body-v1")
	p, err := reg.Publish(m, ReviewPassed, LicenseEvidence{Required: false, State: LicenseNotRequired}, fixture.signManifest(m))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}
	return p
}

func TestRegistry(t *testing.T) {
	t.Run("review-then-sign-then-publish lists an installable package", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		p := publishSample(t, reg, fixture)
		if p.State != StateListed {
			t.Fatalf("want listed, got %s", p.State)
		}
		installed, err := reg.Install(p.Manifest, p.Manifest.ContentDigest)
		if err != nil {
			t.Fatalf("Install failed: %v", err)
		}
		if !installed.Installed {
			t.Fatal("package must be installed")
		}
	})

	t.Run("unreviewed packages never publish", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		m := sampleManifest()
		if _, err := reg.Publish(m, ReviewPending, LicenseEvidence{}, fixture.signManifest(m)); err == nil {
			t.Fatal("pending review must block publication")
		}
		if _, err := reg.Publish(m, ReviewRejected, LicenseEvidence{}, fixture.signManifest(m)); err == nil {
			t.Fatal("rejected review must block publication")
		}
	})

	t.Run("tampered manifest or body is quarantined and installs zero (T-10, M9-015)", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		// Case 1: the installer presents a manifest with injected permissions.
		p := publishSample(t, reg, fixture)
		live, _ := reg.Get(p.Manifest.OrgID, p.Manifest.PackageID)
		tampered := live.Manifest
		tampered.Permissions = append(tampered.Permissions, "command.run")
		if _, err := reg.Install(tampered, tampered.ContentDigest); !errors.Is(err, ErrSignatureInvalid) || Code(err) != "M9-015" {
			t.Fatalf("tampered manifest must fail M9-015, got %v", err)
		}
		after, _ := reg.Get(p.Manifest.OrgID, p.Manifest.PackageID)
		if after.State != StateQuarantined {
			t.Fatalf("tampered package must be quarantined, got %s", after.State)
		}
		// Quarantined packages refuse reinstall with M9-016.
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); !errors.Is(err, ErrQuarantined) || Code(err) != "M9-016" {
			t.Fatalf("want M9-016 for quarantined package, got %v", err)
		}
		// Case 2 (fresh package): the body was swapped in flight.
		p2 := *publishSample(t, reg, fixture)
		p2.Manifest.PackageID = "pkg-body-swap"
		// Re-publish under the new ID so the registry holds a clean row.
		m := p2.Manifest
		if _, err := reg.Publish(m, ReviewPassed, LicenseEvidence{}, fixture.signManifest(m)); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		if _, err := reg.Install(m, bodyDigestOf("evil-body")); !errors.Is(err, ErrSignatureInvalid) || Code(err) != "M9-015" {
			t.Fatalf("tampered body must fail M9-015, got %v", err)
		}
	})

	t.Run("signature from outside the ring is refused (M9-015)", func(t *testing.T) {
		ring, _ := ringWithKey(t)
		_, outsiderPriv, _ := ed25519.GenerateKey(rand.Reader)
		reg := New(ring)
		m := sampleManifest()
		bad := Signature{KeyID: "k1", SigHex: hex.EncodeToString(ed25519.Sign(outsiderPriv, []byte(ManifestDigest(m))))}
		if _, err := reg.Publish(m, ReviewPassed, LicenseEvidence{}, bad); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("forged signature must fail M9-015, got %v", err)
		}
		unknown := Signature{KeyID: "k-unknown", SigHex: "00"}
		if _, err := reg.Publish(m, ReviewPassed, LicenseEvidence{}, unknown); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("unknown key must fail M9-015, got %v", err)
		}
	})

	t.Run("license review blocks install until passed (T-12, M9-018)", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		m := sampleManifest()
		m.ContentDigest = bodyDigestOf("licensed-body")
		p, err := reg.Publish(m, ReviewPassed, LicenseEvidence{Required: true, State: LicensePending}, fixture.signManifest(m))
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); !errors.Is(err, ErrLicenseReview) || Code(err) != "M9-018" {
			t.Fatalf("pending license must fail M9-018, got %v", err)
		}
		// The catalog row must never present a pending license as passed.
		live, _ := reg.Get(p.Manifest.OrgID, p.Manifest.PackageID)
		if live.License.State == LicensePassed {
			t.Fatal("pending license must not display as passed")
		}
	})

	t.Run("platform read-only catalog bypasses org license review", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		m := sampleManifest()
		m.PlatformReadOnly = true
		m.ContentDigest = bodyDigestOf("platform-body")
		p, err := reg.Publish(m, ReviewPassed, LicenseEvidence{Required: true, State: LicensePending}, fixture.signManifest(m))
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); err != nil {
			t.Fatalf("platform read-only catalog must install without org license review, got %v", err)
		}
	})

	t.Run("revocation propagates tombstones and quarantines installs (T-11, M9-017)", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		p := publishSample(t, reg, fixture)
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); err != nil {
			t.Fatalf("Install failed: %v", err)
		}
		var mu sync.Mutex
		propagated := []string{}
		reg.OnRevoke(func(packageID string) { mu.Lock(); propagated = append(propagated, packageID); mu.Unlock() })
		if _, err := reg.Revoke(p.Manifest.OrgID, p.Manifest.PackageID, "supply-chain incident"); err != nil {
			t.Fatalf("Revoke failed: %v", err)
		}
		mu.Lock()
		if len(propagated) != 1 || propagated[0] != p.Manifest.PackageID {
			mu.Unlock()
			t.Fatalf("tombstone must propagate to subscribers, got %v", propagated)
		}
		mu.Unlock()
		if tombs := reg.Tombstones(); len(tombs) != 1 || tombs[0] != p.Manifest.PackageID {
			t.Fatalf("tombstone ledger broken: %v", tombs)
		}
		live, _ := reg.Get(p.Manifest.OrgID, p.Manifest.PackageID)
		if live.Installed || live.State != StateQuarantined {
			t.Fatalf("installed copy must be quarantined-blocked, got state=%s installed=%v", live.State, live.Installed)
		}
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("revoked-then-quarantined package must refuse install, got %v", err)
		}
		// A not-yet-installed revoked package answers M9-017.
		m2 := sampleManifest()
		m2.PackageID = "pkg-second"
		m2.ContentDigest = bodyDigestOf("second-body")
		p2, _ := reg.Publish(m2, ReviewPassed, LicenseEvidence{}, fixture.signManifest(m2))
		if _, err := reg.Revoke(p2.Manifest.OrgID, p2.Manifest.PackageID, "withdrawn"); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Install(p2.Manifest, p2.Manifest.ContentDigest); !errors.Is(err, ErrRevoked) || Code(err) != "M9-017" {
			t.Fatalf("revoked package must fail M9-017, got %v", err)
		}
	})

	t.Run("key rotation: grace keys verify but no longer sign", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		p := publishSample(t, reg, fixture)
		// Rotation: enroll k2, move k1 to grace.
		pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
		if err := ring.AddKey("k2", pub2); err != nil {
			t.Fatal(err)
		}
		ring.EnterGrace("k1")
		// k1-signed packages still install during the grace window.
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); err != nil {
			t.Fatalf("grace key must still verify, got %v", err)
		}
		// But k1 may not sign new publications anymore.
		m2 := sampleManifest()
		m2.PackageID = "pkg-new"
		if _, err := reg.Publish(m2, ReviewPassed, LicenseEvidence{}, fixture.signManifest(m2)); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("grace key must not sign, got %v", err)
		}
		// After grace closes, k1 packages stop verifying and quarantine.
		ring.RemoveKey("k1")
		if _, err := reg.Install(p.Manifest, p.Manifest.ContentDigest); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("removed key must fail M9-015, got %v", err)
		}
		// k2 takes over signing.
		m3 := sampleManifest()
		m3.PackageID = "pkg-k2"
		m3.ContentDigest = bodyDigestOf("k2-body")
		k2 := &SignerFixture{keyID: "k2", priv: priv2}
		if _, err := reg.Publish(m3, ReviewPassed, LicenseEvidence{}, k2.signManifest(m3)); err != nil {
			t.Fatalf("k2 must sign after rotation, got %v", err)
		}
	})

	t.Run("org isolation: packages never cross org keys", func(t *testing.T) {
		ring, fixture := ringWithKey(t)
		reg := New(ring)
		p := publishSample(t, reg, fixture)
		if _, err := reg.Install(Manifest{OrgID: "01JDOTHERORG00000000000", PackageID: p.Manifest.PackageID}, p.Manifest.ContentDigest); err == nil {
			t.Fatal("cross-org install must fail closed")
		}
	})
}
