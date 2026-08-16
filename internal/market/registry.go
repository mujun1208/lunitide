// Package market implements the M9 slice-3 capability-market registry
// overlay (T-9.3.1, ADR-017). It reuses the M6 lifecycle semantics
// (list / quarantine / revoke, no second lifecycle) and the delegation
// envelope's keyID-versioned ed25519 Signer/KeyResolver mechanism; M9 adds
// only the organization overlay: org-scoped package keys, organization
// review, organization signing key ring and legal-review evidence.
//
// Publication flow (ADR-017 decision 2): review → sign → publish. The
// signature covers the SHA-256 digest of the manifest's canonical JSON;
// installation verifies it against the key ring before anything else, so a
// tampered manifest or body is quarantined and never installed (T-10).
// Revocation propagates a tombstone to every registered cache subscriber
// (Broker / Runner caches, T-11) and moves already-installed copies into
// quarantine - blocked, never silently uninstalled.
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-015 PACKAGE_SIGNATURE_INVALID
//	M9-016 PACKAGE_QUARANTINED
//	M9-017 PACKAGE_REVOKED
//	M9-018 LICENSE_REVIEW_REQUIRED
package market

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/lunitide/lunitide/internal/domain/token"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

var (
	ErrSignatureInvalid = &ConceptError{"M9-015", "PACKAGE_SIGNATURE_INVALID"}
	ErrQuarantined      = &ConceptError{"M9-016", "PACKAGE_QUARANTINED"}
	ErrRevoked          = &ConceptError{"M9-017", "PACKAGE_REVOKED"}
	ErrLicenseReview    = &ConceptError{"M9-018", "LICENSE_REVIEW_REQUIRED"}
)

// Code extracts the M9 concept code when err carries one.
func Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Package lifecycle states (M6 semantics, no second lifecycle).
const (
	StateListed      = "listed"
	StateQuarantined = "quarantined"
	StateRevoked     = "revoked"
)

// Review conclusions and legal-review states stay separated from the
// signature (审核结论与签名彼此分离).
const (
	ReviewPending = "pending"
	ReviewPassed  = "passed"
	ReviewRejected = "rejected"

	LicenseNotRequired = "none"
	LicensePending     = "pending"
	LicensePassed      = "passed"
)

// Manifest is the content-addressed package declaration. ContentDigest is
// the SHA-256 of the package body - held outside the signed manifest so a
// body swap is detectable independently of manifest tampering.
type Manifest struct {
	PackageID     string   `json:"packageId"`
	OrgID         string   `json:"orgId"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Permissions   []string `json:"permissions"`
	ContentDigest string   `json:"contentDigest"`
	// PlatformReadOnly marks the platform-global read-only catalog: the only
	// packages exempt from organization legal review (02 技术设计).
	PlatformReadOnly bool `json:"platformReadOnly,omitempty"`
}

// Signature is an ed25519 signature over ManifestDigest(manifest) made by a
// keyID-versioned ring member.
type Signature struct {
	KeyID  string `json:"keyId"`
	SigHex string `json:"sig"`
}

// LicenseEvidence carries the legal review conclusion attached to the
// package (license_evidence). A required-but-unreviewed license blocks
// installation with M9-018 (T-12).
type LicenseEvidence struct {
	Required bool
	State    string // LicenseNotRequired | LicensePending | LicensePassed
}

// Package is one published capability package.
type Package struct {
	Manifest  Manifest
	Signature Signature
	Review    string // ReviewPassed | ReviewRejected | ReviewPending
	License   LicenseEvidence
	State     string // StateListed | StateQuarantined | StateRevoked
	Installed bool
}

// ManifestDigest returns the canonical-JSON SHA-256 digest of a manifest.
func ManifestDigest(m Manifest) string {
	raw, err := token.CanonicalJSON(map[string]any{
		"packageId": m.PackageID, "orgId": m.OrgID, "name": m.Name, "version": m.Version,
		"permissions": stringsToAny(m.Permissions), "contentDigest": m.ContentDigest,
		"platformReadOnly": m.PlatformReadOnly,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// keyState describes one ring member across the rotation lifecycle
// (ADR-017 decision 4): active keys sign and verify; grace keys only verify;
// removed keys are gone.
type keyState struct {
	public ed25519.PublicKey
	grace  bool
}

// KeyRing is the versioned ed25519 key ring: the market signing root.
type KeyRing struct {
	mu    sync.RWMutex
	keys  map[string]keyState
	order []string
}

// NewKeyRing builds an empty ring.
func NewKeyRing() *KeyRing { return &KeyRing{keys: make(map[string]keyState)} }

// AddKey enrolls a new signing key (rotation step 1).
func (r *KeyRing) AddKey(keyID string, public ed25519.PublicKey) error {
	if keyID == "" || len(public) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid key enrollment")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.keys[keyID]; !dup {
		r.order = append(r.order, keyID)
	}
	r.keys[keyID] = keyState{public: public}
	return nil
}

// EnterGrace moves a key into the verify-only grace window (rotation step 3).
func (r *KeyRing) EnterGrace(keyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.keys[keyID]; ok {
		r.keys[keyID] = keyState{public: k.public, grace: true}
	}
}

// RemoveKey drops a key whose grace window closed (rotation step 4).
func (r *KeyRing) RemoveKey(keyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.keys, keyID)
	for i, id := range r.order {
		if id == keyID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// KeyIDs lists ring members in enrollment order.
func (r *KeyRing) KeyIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string{}, r.order...)
}

// resolve returns the public key for verification (active or grace).
func (r *KeyRing) resolve(keyID string) (ed25519.PublicKey, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[keyID]
	return k.public, ok
}

// canSign reports whether keyID may still create signatures.
func (r *KeyRing) canSign(keyID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[keyID]
	return ok && !k.grace
}

// Registry is the org-scoped capability package registry (M6 lifecycle
// reuse + M9 organization overlay). Safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	ring    *KeyRing
	packages map[string]*Package // key: orgID + "/" + packageID
	tombstones []string
	onRevoke  []func(packageID string)
}

// New creates a registry bound to a signing key ring.
func New(ring *KeyRing) *Registry { return &Registry{ring: ring, packages: make(map[string]*Package)} }

func key(orgID, packageID string) string { return orgID + "/" + packageID }

// Publish performs the review → sign → publish flow. The manifest must have
// passed organization review, the signer key must be an active ring member,
// and the signature must verify against the canonical manifest digest.
func (r *Registry) Publish(m Manifest, review string, license LicenseEvidence, sig Signature) (*Package, error) {
	if m.PackageID == "" || m.OrgID == "" || m.ContentDigest == "" {
		return nil, fmt.Errorf("manifest requires packageId, orgId and contentDigest")
	}
	if review != ReviewPassed {
		return nil, fmt.Errorf("package %q must pass review before publication", m.PackageID)
	}
	if !r.ring.canSign(sig.KeyID) {
		return nil, fmt.Errorf("%w: signing key %q is not an active ring member", ErrSignatureInvalid, sig.KeyID)
	}
	pub, ok := r.ring.resolve(sig.KeyID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown signing key %q", ErrSignatureInvalid, sig.KeyID)
	}
	sigBytes, err := hex.DecodeString(sig.SigHex)
	if err != nil || !ed25519.Verify(pub, []byte(ManifestDigest(m)), sigBytes) {
		return nil, fmt.Errorf("%w: manifest signature does not verify", ErrSignatureInvalid)
	}
	sort.Strings(m.Permissions)
	r.mu.Lock()
	defer r.mu.Unlock()
	p := &Package{Manifest: m, Signature: sig, Review: review, License: license, State: StateListed}
	r.packages[key(m.OrgID, m.PackageID)] = p
	return p, nil
}

// Install is the installation gate. The installer presents the manifest it
// downloaded plus the body digest it measured: the registry verifies the
// presented manifest against the recorded signature, so any in-flight
// tampering of the manifest or the body quarantines the package and means
// zero installation (T-10, M9-015/016/017/018).
func (r *Registry) Install(presented Manifest, bodyDigest string) (*Package, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.packages[key(presented.OrgID, presented.PackageID)]
	if !ok {
		return nil, fmt.Errorf("package %q not listed for org %s", presented.PackageID, presented.OrgID)
	}
	if p.State == StateRevoked {
		return nil, fmt.Errorf("%w: package %q was revoked", ErrRevoked, presented.PackageID)
	}
	if p.State == StateQuarantined {
		return nil, fmt.Errorf("%w: package %q is quarantined", ErrQuarantined, presented.PackageID)
	}
	pub, ok := r.ring.resolve(p.Signature.KeyID)
	if !ok {
		p.State = StateQuarantined
		return nil, fmt.Errorf("%w: signing key %q left the ring", ErrSignatureInvalid, p.Signature.KeyID)
	}
	sigBytes, err := hex.DecodeString(p.Signature.SigHex)
	if err != nil || !ed25519.Verify(pub, []byte(ManifestDigest(presented)), sigBytes) {
		p.State = StateQuarantined
		return nil, fmt.Errorf("%w: presented manifest does not verify against the recorded signature", ErrSignatureInvalid)
	}
	if bodyDigest != presented.ContentDigest {
		p.State = StateQuarantined
		return nil, fmt.Errorf("%w: body digest %q does not match manifest %q", ErrSignatureInvalid, bodyDigest, presented.ContentDigest)
	}
	if !p.Manifest.PlatformReadOnly && p.License.Required && p.License.State != LicensePassed {
		return nil, fmt.Errorf("%w: license review is %q", ErrLicenseReview, p.License.State)
	}
	p.Installed = true
	return p, nil
}

// Revoke withdraws a package: state moves to revoked, a tombstone is
// recorded and every cache subscriber (Broker / Runner caches) is notified;
// installed copies flip to quarantine and are blocked - never silently
// uninstalled (ADR-017 decision 3).
func (r *Registry) Revoke(orgID, packageID, reason string) (*Package, error) {
	r.mu.Lock()
	p, ok := r.packages[key(orgID, packageID)]
	if !ok {
		r.mu.Unlock()
		return nil, fmt.Errorf("package %q not listed for org %s", packageID, orgID)
	}
	p.State = StateRevoked
	if p.Installed {
		p.Installed = false
		p.State = StateQuarantined
	}
	r.tombstones = append(r.tombstones, packageID)
	subscribers := append([]func(string){}, r.onRevoke...)
	r.mu.Unlock()
	for _, notify := range subscribers {
		notify(packageID)
	}
	return p, nil
}

// OnRevoke registers a tombstone propagation subscriber.
func (r *Registry) OnRevoke(notify func(packageID string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRevoke = append(r.onRevoke, notify)
}

// Tombstones returns the accumulated revocation tombstones.
func (r *Registry) Tombstones() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string{}, r.tombstones...)
}

// Get exposes the live package row (for projections and tests).
func (r *Registry) Get(orgID, packageID string) (*Package, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.packages[key(orgID, packageID)]
	if !ok {
		return nil, false
	}
	copyOf := *p
	return &copyOf, true
}
