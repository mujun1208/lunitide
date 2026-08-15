package extension

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/lunitide/lunitide/internal/domain/token"
)

// Artifact is the install candidate as discovered from the directory: the
// bytes-hashed archive digest plus its parsed manifest.
type Artifact struct {
	Publisher string
	Name      string
	Version   string
	Digest    string // sha-256 of the artifact bytes (hex)
	Manifest  Manifest
}

// GrantDecision is the confirmed permission grant attached to an install or
// upgrade request (extension.install wire contract).
type GrantDecision struct {
	Granted              []string // permissions the subject confirmed
	ConfirmedDeltaDigest string   // sha-256 of the canonical confirmed delta
}

// Policy carries the supply-chain policy inputs. LicenseAllowlist and
// RuntimeAllowlist default to permissive-empty (deny all) — operators wire
// the real lists at startup.
type Policy struct {
	LicenseAllowlist []string
	RuntimeAllowlist []string
}

// Verifier performs the frozen four-step verification. TrustedKeys maps
// keyId -> ed25519 public key; RevokedKeys/RevokedPublishers/RevokedDigests
// are the revocation inputs that flip a candidate into EXT-002.
type Verifier struct {
	TrustedKeys       map[string]ed25519.PublicKey
	RevokedKeys       map[string]bool
	RevokedPublishers map[string]bool
	RevokedDigests    map[string]bool
	Policy            Policy
}

// Verdict is the verification outcome. Quarantined is true iff any step
// failed; Code is the M6 wire error code and Err the sentinel for errors.Is.
type Verdict struct {
	Quarantined bool
	Blocked     bool // EXT-002: revoked — stronger than quarantine, caches BLOCKED
	Code        string
	Err         error
	Diagnostics string
}

// Verify runs digest -> signature -> SBOM -> permission delta. previously
// Granted is the permission set the subject already granted to the previous
// version (empty for a fresh install). The order is frozen by the M6 design;
// the first failing step short-circuits and quarantines.
func (v *Verifier) Verify(artifact Artifact, grant GrantDecision, previouslyGranted []string) Verdict {
	if err := artifact.Manifest.Validate(); err != nil {
		return Verdict{Quarantined: true, Code: CodeManifestInvalid, Err: err, Diagnostics: "manifest structural validation failed"}
	}
	if verdict, ok := v.verifyDigest(artifact); !ok {
		return verdict
	}
	if verdict, ok := v.verifySignature(artifact); !ok {
		return verdict
	}
	if verdict, ok := v.verifySBOM(artifact); !ok {
		return verdict
	}
	if verdict, ok := v.verifyPermissionDelta(artifact, grant, previouslyGranted); !ok {
		return verdict
	}
	return Verdict{Quarantined: false, Code: "", Err: nil}
}

func (v *Verifier) verifyDigest(artifact Artifact) (Verdict, bool) {
	if !digestPattern.MatchString(artifact.Digest) {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrDigestMismatch, Diagnostics: "artifact digest malformed"}, false
	}
	if artifact.Digest != artifact.Manifest.ArtifactDigest {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrDigestMismatch, Diagnostics: "manifest artifactDigest does not match the artifact bytes"}, false
	}
	return Verdict{}, true
}

func (v *Verifier) verifySignature(artifact Artifact) (Verdict, bool) {
	if v.RevokedPublishers[artifact.Publisher] || v.RevokedDigests[artifact.Digest] || v.RevokedKeys[artifact.Manifest.Signature.KeyID] {
		return Verdict{Quarantined: true, Blocked: true, Code: CodeRevoked, Err: ErrArtifactRevoked, Diagnostics: "publisher, artifact or signing key revoked"}, false
	}
	key, ok := v.TrustedKeys[artifact.Manifest.Signature.KeyID]
	if !ok {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrSignatureInvalid, Diagnostics: "unknown keyId"}, false
	}
	sig, err := base64.StdEncoding.DecodeString(artifact.Manifest.Signature.Value)
	if err != nil {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrSignatureInvalid, Diagnostics: "signature not valid base64"}, false
	}
	content, err := signedContent(&artifact.Manifest)
	if err != nil {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrSignatureInvalid, Diagnostics: "canonical encoding failed"}, false
	}
	if !ed25519.Verify(key, content, sig) {
		return Verdict{Quarantined: true, Code: CodeDigestSignature, Err: ErrSignatureInvalid, Diagnostics: "signature does not cover canonical manifest + artifact digest"}, false
	}
	return Verdict{}, true
}

func (v *Verifier) verifySBOM(artifact Artifact) (Verdict, bool) {
	m := &artifact.Manifest
	if m.SBOMRef == "" {
		return Verdict{Quarantined: true, Code: CodePolicyRejected, Err: ErrSBOMRefMissing, Diagnostics: "sbomRef empty"}, false
	}
	for _, dep := range m.Dependencies {
		if dep.Digest == "" || dep.Version == "" {
			return Verdict{Quarantined: true, Code: CodePolicyRejected, Err: ErrDependencyUnlocked, Diagnostics: fmt.Sprintf("dependency %s not locked to version+digest", dep.Name)}, false
		}
	}
	if len(v.Policy.LicenseAllowlist) > 0 {
		allowed := make(map[string]bool, len(v.Policy.LicenseAllowlist))
		for _, l := range v.Policy.LicenseAllowlist {
			allowed[l] = true
		}
		if !allowed[m.License] {
			return Verdict{Quarantined: true, Code: CodePolicyRejected, Err: ErrLicenseRejected, Diagnostics: fmt.Sprintf("license %s not allowed", m.License)}, false
		}
	}
	return Verdict{}, true
}

func (v *Verifier) verifyPermissionDelta(artifact Artifact, grant GrantDecision, previouslyGranted []string) (Verdict, bool) {
	declared := artifact.Manifest.DeclaredPermissionSet()
	for _, g := range grant.Granted {
		if !declared[g] {
			return Verdict{Quarantined: true, Code: CodePermissionDelta, Err: ErrPermissionUndeclared, Diagnostics: fmt.Sprintf("granted %q not declared by manifest", g)}, false
		}
	}
	delta := PermissionDelta(grant.Granted, previouslyGranted)
	if len(delta) == 0 {
		return Verdict{}, true
	}
	if grant.ConfirmedDeltaDigest != DeltaDigest(delta) {
		return Verdict{Quarantined: true, Code: CodePermissionDelta, Err: ErrDeltaUnconfirmed, Diagnostics: "new permissions present but confirmedDeltaDigest does not match the delta"}, false
	}
	return Verdict{}, true
}

// PermissionDelta returns the sorted set of permissions in granted that are
// absent from previouslyGranted — the permissions an upgrade adds and that
// must be re-confirmed (M6-EXT-004).
func PermissionDelta(granted, previouslyGranted []string) []string {
	previous := make(map[string]bool, len(previouslyGranted))
	for _, p := range previouslyGranted {
		previous[p] = true
	}
	seen := make(map[string]bool, len(granted))
	var delta []string
	for _, p := range granted {
		if !previous[p] && !seen[p] {
			seen[p] = true
			delta = append(delta, p)
		}
	}
	sort.Strings(delta)
	return delta
}

// DeltaDigest is the canonical digest a confirmation must carry.
func DeltaDigest(delta []string) string {
	canonical, err := token.CanonicalJSON(delta)
	if err != nil {
		canonical = []byte(fmt.Sprintf("%v", delta))
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// signedContent builds the exact bytes covered by the manifest signature:
// the canonical manifest (signature value cleared) concatenated with the
// artifact digest.
func signedContent(m *Manifest) ([]byte, error) {
	body, err := m.Canonical()
	if err != nil {
		return nil, err
	}
	return append(body, []byte(m.ArtifactDigest)...), nil
}

// ErrStepOrder guards internal misuse: verification must run the four steps
// in the frozen order.
var ErrStepOrder = errors.New("extension: verification steps must run in order")
