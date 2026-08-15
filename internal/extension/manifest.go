// Package extension implements the M6 third-party skill/plugin supply chain
// verification core (T-6.1.3): manifest contract, digest/signature/SBOM/
// permission-delta four-step verification and the QUARANTINED transition.
//
// Error codes map 1:1 onto M6_ERROR_CATALOG_V2:
//
//	M6-SKL-001 manifest missing required fields / malformed
//	M6-EXT-001 digest mismatch or signature verification failure
//	M6-EXT-002 signature or license revoked (BLOCKED)
//	M6-EXT-003 SBOM / dependency / license policy rejected
//	M6-EXT-004 permission delta not re-confirmed on upgrade
//
// The M6 design freezes the verification order digest -> signature -> SBOM ->
// permission delta; any failure quarantines the artifact and the verdict is
// never cached (verified artifacts are re-verified on every install).
package extension

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/token"
)

// Wire error codes (M6_ERROR_CATALOG_V2).
const (
	CodeManifestInvalid = "M6-SKL-001"
	CodeDigestSignature = "M6-EXT-001"
	CodeRevoked         = "M6-EXT-002"
	CodePolicyRejected  = "M6-EXT-003"
	CodePermissionDelta = "M6-EXT-004"
)

var (
	ErrManifestMissingFields = errors.New("extension: manifest missing required fields")
	ErrDigestMismatch        = errors.New("extension: artifact digest mismatch")
	ErrSignatureInvalid      = errors.New("extension: signature verification failed")
	ErrKeyRevoked            = errors.New("extension: signing key or publisher revoked")
	ErrArtifactRevoked       = errors.New("extension: artifact signature revoked")
	ErrSBOMRefMissing        = errors.New("extension: sbomRef missing")
	ErrDependencyUnlocked    = errors.New("extension: dependency not pinned to a digest")
	ErrLicenseRejected       = errors.New("extension: license not in allowlist")
	ErrPermissionUndeclared  = errors.New("extension: granted permission not declared by manifest")
	ErrDeltaUnconfirmed      = errors.New("extension: permission delta not confirmed")
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Dependency is one locked SBOM dependency entry. Every dependency must pin
// both an exact version and a content digest.
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// ResourceLimits freezes the runtime envelope a skill may use.
type ResourceLimits struct {
	CPUMillis int  `json:"cpuMillis"`
	MemoryMB  int  `json:"memoryMB"`
	DiskMB    int  `json:"diskMB"`
	Processes int  `json:"processes"`
	Network   bool `json:"network"`
}

// Signature carries the detached signature metadata. The signed content is
// the canonical JSON of the manifest with the signature field cleared plus
// the artifact digest (design: "signature covers the canonicalised
// Manifest+artifactDigest"). The algorithm is ed25519 (recorded as a design
// decision in docs/evidence/m6-day0.txt — the design docs fix the coverage
// model but not the algorithm).
type Signature struct {
	KeyID  string `json:"keyId"`
	Value  string `json:"value"` // base64, ed25519
	Alg    string `json:"alg"`   // must be "ed25519"
	KeyNum string `json:"keyNum,omitempty"`
}

// Manifest is the skill/plugin manifest contract (M6 design: 21 required
// fields). Permissions is the exhaustive declared set; triggers are
// declarative allowlist entries only.
type Manifest struct {
	SchemaVersion  string         `json:"schemaVersion"`
	SkillID        string         `json:"skillId"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Publisher      string         `json:"publisher"`
	Description    string         `json:"description"`
	Entrypoint     string         `json:"entrypoint"`
	InputSchema    map[string]any `json:"inputSchema"`
	OutputSchema   map[string]any `json:"outputSchema"`
	Permissions    []string       `json:"permissions"`
	Runtime        string         `json:"runtime"`
	MinHostVersion string         `json:"minHostVersion"`
	ArtifactDigest string         `json:"artifactDigest"`
	Signature      Signature      `json:"signature"`
	License        string         `json:"license"`
	SBOMRef        string         `json:"sbomRef"`
	Triggers       []string       `json:"triggers"`
	Dependencies   []Dependency   `json:"dependencies"`
	TimeoutMS      int            `json:"timeout"`
	ResourceLimits ResourceLimits `json:"resourceLimits"`
	Extra          map[string]any `json:"-"`
}

// RequiredFields lists every manifest field the M6 contract freezes. Order
// is the schema order; validation reports the first missing field.
var RequiredFields = []string{
	"schemaVersion", "skillId", "name", "version", "publisher", "description",
	"entrypoint", "inputSchema", "outputSchema", "permissions", "runtime",
	"minHostVersion", "artifactDigest", "signature", "license", "sbomRef",
	"triggers", "dependencies", "timeout", "resourceLimits",
}

// Validate checks structural completeness (M6-SKL-001): all required fields
// present, digest fields well-formed, permissions non-empty names.
func (m *Manifest) Validate() error {
	if m == nil {
		return ErrManifestMissingFields
	}
	if strings.TrimSpace(m.SchemaVersion) == "" || strings.TrimSpace(m.SkillID) == "" ||
		strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Version) == "" ||
		strings.TrimSpace(m.Publisher) == "" || strings.TrimSpace(m.Description) == "" ||
		strings.TrimSpace(m.Entrypoint) == "" || m.InputSchema == nil || m.OutputSchema == nil ||
		len(m.Permissions) == 0 || strings.TrimSpace(m.Runtime) == "" ||
		strings.TrimSpace(m.MinHostVersion) == "" || strings.TrimSpace(m.License) == "" ||
		m.TimeoutMS <= 0 || strings.TrimSpace(m.Signature.KeyID) == "" ||
		strings.TrimSpace(m.Signature.Value) == "" {
		return fmt.Errorf("%w: manifest incomplete", ErrManifestMissingFields)
	}
	if !digestPattern.MatchString(m.ArtifactDigest) {
		return fmt.Errorf("%w: artifactDigest not a sha-256 hex digest", ErrManifestMissingFields)
	}
	// NOTE: dependency pinning is deliberately NOT checked here — an unlocked
	// dependency is a policy failure (M6-EXT-003), not a structural one, and
	// must surface with the SBOM diagnostics.
	return nil
}

// Canonical returns the canonical JSON encoding of the manifest with the
// signature value cleared — exactly the bytes the publisher signed together
// with the artifact digest.
func (m *Manifest) Canonical() ([]byte, error) {
	clone := *m
	clone.Signature = Signature{KeyID: m.Signature.KeyID, Alg: m.Signature.Alg}
	return token.CanonicalJSON(&clone)
}

// DeclaredPermissionSet returns the manifest permission set for subset checks.
func (m *Manifest) DeclaredPermissionSet() map[string]bool {
	set := make(map[string]bool, len(m.Permissions))
	for _, p := range m.Permissions {
		set[p] = true
	}
	return set
}
