// M7 slice 5 domain values (T-7.5.1): the AppUpdate split-track. This is a
// physically isolated domain - the entities below never reference project
// release rows (no cross-domain foreign keys, no shared IDs; 02-技术设计
// §05). update packages carry a JCS-canonical manifest
// (version/min_version/package_digest/nonce/issued_at/not_before/expires_at/
// key_id) whose signature is verified before any install; nonces are
// single-consumption and device versions move monotonically (normal updates
// never downgrade).
package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Update channel names (update_channels.name CHECK set).
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// Update channel states.
const (
	ChActive  = "active"
	ChRetired = "retired"
)

// Update package states (update_packages.state CHECK set).
const (
	UpdBuilding  = "building"
	UpdPublished = "published"
	UpdRevoked   = "revoked"
)

// Update installation states (update_installations.state CHECK set).
const (
	UpdPending     = "pending"
	UpdDownloading = "downloading"
	UpdInstalling  = "installing"
	UpdSucceeded   = "succeeded"
	UpdFailed      = "failed"
	UpdRolledBack  = "rolled_back"
)

// Update rollback attempt states (update_rollback_attempts.state CHECK set).
const (
	UpdRbPending   = "pending"
	UpdRbRunning   = "running"
	UpdRbSucceeded = "succeeded"
	UpdRbFailed    = "failed"
)

// appUpdate install wire states (appUpdate.install response projection).
// Prefixed UpdWire* to avoid clashing with the promotion wire constants in
// this shared package.
const (
	WireInstalled     = "installed"
	UpdWireRolledBack = "rolled_back"
)

// UpdateChannel is one update distribution channel (stable/beta).
type UpdateChannel struct {
	ID        string
	Name      string
	State     string
	CreatedAt time.Time
}

// UpdatePackage is one published app-update package on a channel. Signature
// covers the canonical manifest (CanonicalUpdateManifest); expires_at/not_before
// bound the trusted install window.
type UpdatePackage struct {
	ID            string
	ChannelID     string
	AppVersion    string
	MinVersion    string
	PackageDigest string
	Signature     string
	Nonce         string
	NotBefore     time.Time
	ExpiresAt     time.Time
	KeyID         string
	State         string
	CreatedAt     time.Time
}

// UpdateInstallation is one device install attempt of one package.
type UpdateInstallation struct {
	ID          string
	PackageID   string
	DeviceID    string
	State       string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

// UpdateReceipt is the append-only success receipt of one installation.
type UpdateReceipt struct {
	ID             string
	InstallationID string
	ReceiptJSON    string
	Digest         string
	CreatedAt      time.Time
}

// UpdateRollbackAttempt is one append-only auto-rollback attempt.
type UpdateRollbackAttempt struct {
	ID             string
	InstallationID string
	State          string
	OperatorID     string
	ResultJSON     string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

// installationTransitions is the legal UpdateInstallation state machine:
// pending -> downloading -> installing -> succeeded, with failed reachable
// from any pre-success step and rolled_back reachable from failed/installing
// (auto rollback on install or post-install verification failure).
var installationTransitions = map[string]map[string]bool{
	UpdPending:     {UpdDownloading: true, UpdFailed: true},
	UpdDownloading: {UpdInstalling: true, UpdFailed: true},
	UpdInstalling:  {UpdSucceeded: true, UpdFailed: true, UpdRolledBack: true},
	UpdSucceeded:   {},
	UpdFailed:      {UpdRolledBack: true},
	UpdRolledBack:  {},
}

// LegalInstallationTransition guards the installation state machine.
func LegalInstallationTransition(from, to string) bool {
	return installationTransitions[from][to]
}

// rollbackTransitions is the legal update rollback-attempt state machine.
var rollbackTransitions = map[string]map[string]bool{
	UpdRbPending:   {UpdRbRunning: true},
	UpdRbRunning:   {UpdRbSucceeded: true, UpdRbFailed: true},
	UpdRbSucceeded: {},
	UpdRbFailed:    {},
}

// LegalUpdateRollbackTransition guards the rollback-attempt state machine.
func LegalUpdateRollbackTransition(from, to string) bool {
	return rollbackTransitions[from][to]
}

// UpdateManifest is the JCS-canonical signed document of one package. Field
// order is fixed; timestamps are UTC RFC3339. Any stored-field change breaks
// the signature verification (M7-UPD-001).
type UpdateManifest struct {
	Version       string `json:"version"`
	MinVersion    string `json:"min_version"`
	PackageDigest string `json:"package_digest"`
	Nonce         string `json:"nonce"`
	IssuedAt      string `json:"issued_at"`
	NotBefore     string `json:"not_before"`
	ExpiresAt     string `json:"expires_at"`
	KeyID         string `json:"key_id"`
}

// ManifestOf builds the canonical manifest of one stored package.
func ManifestOf(p UpdatePackage) UpdateManifest {
	return UpdateManifest{
		Version:       p.AppVersion,
		MinVersion:    p.MinVersion,
		PackageDigest: p.PackageDigest,
		Nonce:         p.Nonce,
		IssuedAt:      rfc3339(p.CreatedAt),
		NotBefore:     rfc3339(p.NotBefore),
		ExpiresAt:     rfc3339(p.ExpiresAt),
		KeyID:         p.KeyID,
	}
}

// Canonical serializes the manifest with the fixed field order above (struct
// marshalling keeps declaration order, so the document is stable).
func (m UpdateManifest) Canonical() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// Digest is the SHA-256 of the canonical manifest document.
func (m UpdateManifest) Digest() string {
	sum := sha256.Sum256([]byte(m.Canonical()))
	return hex.EncodeToString(sum[:])
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// ParseVersion splits a dotted numeric version ("1.2.10"). Non-numeric
// segments answer ok=false - callers fail closed instead of guessing.
func ParseVersion(v string) ([]int64, bool) {
	if v == "" || len(v) > 32 {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// CompareVersions answers -1/0/1 for a vs b (missing segments count as 0).
// Non-numeric input orders lexicographically as a deterministic fallback
// after the numeric attempt (both malformed -> string compare).
func CompareVersions(a, b string) int {
	na, aok := ParseVersion(a)
	nb, bok := ParseVersion(b)
	if aok && bok {
		n := len(na)
		if len(nb) > n {
			n = len(nb)
		}
		for i := 0; i < n; i++ {
			var x, y int64
			if i < len(na) {
				x = na[i]
			}
			if i < len(nb) {
				y = nb[i]
			}
			if x != y {
				if x < y {
					return -1
				}
				return 1
			}
		}
		return 0
	}
	return strings.Compare(a, b)
}