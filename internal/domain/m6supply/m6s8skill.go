// Legacy S8 skill entity chain and the governed import pipeline
// (migration 0053): m6_skill / m6_skill_version / m6_skill_dependency /
// m6_skill_install / m6_skill_trigger plus m6_import_candidate.
//
// Manifest contract (M6/02 §07 Legacy S8): every skill version pins an
// immutable manifest reference and a package hash; permissions are an
// explicit JSON object; dependencies are typed and locked.
//
// Import pipeline (M6/02 §08): every imported asset travels the fixed
// governance walk
//
//	discovered -> pinned -> inspected -> scanned -> evaluated
//	          -> awaiting_approval -> approved
//	             (any pre-approval state) -> rejected
//	             approved -> revoked
//
// with one audit action per step (skill.import.*). Approval is the only
// gate that installs; revocation is terminal.
package m6supply

import (
	"encoding/json"
	"fmt"
	"time"
)

// ── Skill entity chain ──────────────────────────────────────────────────────

// Skill states (m6_skill.status CHECK set).
const (
	SkillDiscovered  = "discovered"
	SkillVerified    = "verified"
	SkillInstalled   = "installed"
	SkillEnabled     = "enabled"
	SkillPaused      = "paused"
	SkillQuarantined = "quarantined"
	SkillBlocked     = "blocked"
	SkillUninstalled = "uninstalled"
)

// skillTransitions guards the lifecycle. quarantined and blocked are
// recoverable by re-verification; uninstalled is terminal.
var skillTransitions = map[string]map[string]bool{
	SkillDiscovered:  {SkillVerified: true, SkillQuarantined: true, SkillBlocked: true},
	SkillVerified:    {SkillInstalled: true, SkillBlocked: true},
	SkillInstalled:   {SkillEnabled: true, SkillPaused: true, SkillUninstalled: true, SkillQuarantined: true},
	SkillEnabled:     {SkillPaused: true, SkillUninstalled: true, SkillQuarantined: true},
	SkillPaused:      {SkillEnabled: true, SkillUninstalled: true, SkillQuarantined: true},
	SkillQuarantined: {SkillVerified: true, SkillUninstalled: true},
	SkillBlocked:     {SkillVerified: true},
	SkillUninstalled: {},
}

// SkillTransitionAllowed guards the lifecycle CAS.
func SkillTransitionAllowed(from, to string) bool {
	if _, ok := skillTransitions[from]; !ok {
		return false
	}
	return skillTransitions[from][to]
}

// Skill is the named head of the version chain.
type Skill struct {
	ID               string
	Name             string
	Publisher        string
	Status           string
	CurrentVersionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Signature statuses (m6_skill_version.signature_status CHECK set).
// SignatureVerified is shared with the extension artifact signature states
// and stays defined in m6supply.go.
const (
	SignatureUnverified = "unverified"
	SignatureInvalid    = "invalid"
)

// SkillVersion is one pinned, immutable version of a skill.
type SkillVersion struct {
	ID              string
	SkillID         string
	Semver          string
	ManifestRef     string
	PackageHash     string
	SignatureStatus string
	PermissionsJSON string
	CreatedAt       time.Time
}

// Dependency types (m6_skill_dependency.dependency_type CHECK set).
const (
	DependencySkill   = "skill"
	DependencyLibrary = "library"
	DependencyRuntime = "runtime"
)

// SkillDependency is one typed, locked dependency edge.
type SkillDependency struct {
	ID                string
	SkillVersionID    string
	DependencyType    string
	Name              string
	VersionConstraint string
	LockedDigest      string
	CreatedAt         time.Time
}

// Install states (m6_skill_install.status CHECK set). Prefixed
// SkillInstall* to stay distinct from the extension InstallState walk.
const (
	SkillInstallInstalled   = "installed"
	SkillInstallEnabled     = "enabled"
	SkillInstallDisabled    = "disabled"
	SkillInstallQuarantined = "quarantined"
)

// SkillInstall binds one skill version into one workspace.
type SkillInstall struct {
	ID            string
	SkillVersionID string
	WorkspaceID   string
	Status        string
	InstalledAt   time.Time
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Trigger states (m6_skill_trigger.status CHECK set).
const (
	TriggerMatched  = "matched"
	TriggerExecuted = "executed"
	TriggerSkipped  = "skipped"
	TriggerDenied   = "denied"
)

// SkillTrigger records one trigger decision in a session.
type SkillTrigger struct {
	ID            string
	SessionID     string
	SkillVersionID string
	Score         float64
	Reason        string
	Status        string
	ResultRef     string
	CreatedAt     time.Time
}

// ValidateSkillTriggerInput checks the trigger payload shape.
func ValidateSkillTriggerInput(score float64, status string) error {
	if score < 0 || score > 1 {
		return fmt.Errorf("score must be within [0,1]")
	}
	switch status {
	case TriggerMatched, TriggerExecuted, TriggerSkipped, TriggerDenied:
	default:
		return fmt.Errorf("status must be matched|executed|skipped|denied")
	}
	return nil
}

// ── Manifest validation ─────────────────────────────────────────────────────

// Manifest is the consumed shape of a skill manifest.
type Manifest struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Publisher   string   `json:"publisher"`
	Description string   `json:"description,omitempty"`
	Permissions struct {
		Tools    []string `json:"tools,omitempty"`
		Network  []string `json:"network,omitempty"`
		Filesystem []string `json:"filesystem,omitempty"`
	} `json:"permissions"`
	Dependencies []struct {
		Type             string `json:"type"`
		Name             string `json:"name"`
		VersionConstraint string `json:"versionConstraint"`
	} `json:"dependencies,omitempty"`
}

// ManifestSchema is the frozen manifest schema identifier.
const ManifestSchema = "lunitide.skill/v1"

// MaxManifestBytes bounds the manifest document.
const MaxManifestBytes = 1 << 20

// ErrManifestInvalid describes a manifest contract violation.
var ErrManifestInvalid = fmt.Errorf("m6supply: manifest invalid")

// ParseManifest validates and decodes a skill manifest document.
//
// Contract: schema must be lunitide.skill/v1; name 1..128 unique form;
// version must be a plain semver string 1..64; publisher 1..256;
// permissions must be explicit (an omitted permissions object is invalid —
// a skill declares what it needs, it never defaults to nothing noticed);
// dependency types are the closed enum and constraints non-empty.
func ParseManifest(raw []byte) (*Manifest, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty document", ErrManifestInvalid)
	}
	if len(raw) > MaxManifestBytes {
		return nil, fmt.Errorf("%w: document over %d bytes", ErrManifestInvalid, MaxManifestBytes)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if m.Schema != ManifestSchema {
		return nil, fmt.Errorf("%w: schema must be %s", ErrManifestInvalid, ManifestSchema)
	}
	if len(m.Name) < 1 || len(m.Name) > 128 {
		return nil, fmt.Errorf("%w: name length must be 1..128", ErrManifestInvalid)
	}
	if len(m.Version) < 1 || len(m.Version) > 64 {
		return nil, fmt.Errorf("%w: version length must be 1..64", ErrManifestInvalid)
	}
	if !validSemver(m.Version) {
		return nil, fmt.Errorf("%w: version must be MAJOR.MINOR.PATCH", ErrManifestInvalid)
	}
	if len(m.Publisher) < 1 || len(m.Publisher) > 256 {
		return nil, fmt.Errorf("%w: publisher length must be 1..256", ErrManifestInvalid)
	}
	if len(m.Permissions.Tools) == 0 && len(m.Permissions.Network) == 0 && len(m.Permissions.Filesystem) == 0 {
		return nil, fmt.Errorf("%w: permissions must be declared explicitly", ErrManifestInvalid)
	}
	for _, dep := range m.Dependencies {
		switch dep.Type {
		case DependencySkill, DependencyLibrary, DependencyRuntime:
		default:
			return nil, fmt.Errorf("%w: dependency type %q invalid", ErrManifestInvalid, dep.Type)
		}
		if len(dep.Name) < 1 || len(dep.Name) > 256 {
			return nil, fmt.Errorf("%w: dependency name length must be 1..256", ErrManifestInvalid)
		}
		if len(dep.VersionConstraint) < 1 || len(dep.VersionConstraint) > 128 {
			return nil, fmt.Errorf("%w: dependency versionConstraint length must be 1..128", ErrManifestInvalid)
		}
	}
	return &m, nil
}

// validSemver checks the MAJOR.MINOR.PATCH core (pre-release/build
// suffixes allowed after the core).
func validSemver(v string) bool {
	core := v
	for i := 0; i < len(v); i++ {
		if v[i] == '-' || v[i] == '+' {
			core = v[:i]
			break
		}
	}
	dots := 0
	start := 0
	for i := 0; i <= len(core); i++ {
		if i == len(core) || core[i] == '.' {
			seg := core[start:i]
			if len(seg) == 0 || len(seg) > 9 {
				return false
			}
			for j := 0; j < len(seg); j++ {
				if seg[j] < '0' || seg[j] > '9' {
					return false
				}
			}
			dots++
			start = i + 1
		}
	}
	return dots == 3
}

// ── Import candidate pipeline ───────────────────────────────────────────────

// Import states (m6_import_candidate.state CHECK set).
const (
	ImportDiscovered      = "discovered"
	ImportPinned          = "pinned"
	ImportInspected       = "inspected"
	ImportScanned         = "scanned"
	ImportEvaluated       = "evaluated"
	ImportAwaitingApproval = "awaiting_approval"
	ImportApproved        = "approved"
	ImportRejected        = "rejected"
	ImportRevoked         = "revoked"
)

// Asset types (m6_import_candidate.asset_type CHECK set).
const (
	AssetSkill        = "skill"
	AssetProfile      = "profile"
	AssetPromptBundle = "prompt_bundle"
)

// importTransitions is the fixed governance walk. Steps are strictly
// sequential; rejection is reachable from every pre-approval state;
// revocation is reachable from approved and rejected; both are terminal.
var importTransitions = map[string]map[string]bool{
	ImportDiscovered:       {ImportPinned: true, ImportRejected: true},
	ImportPinned:           {ImportInspected: true, ImportRejected: true},
	ImportInspected:        {ImportScanned: true, ImportRejected: true},
	ImportScanned:          {ImportEvaluated: true, ImportRejected: true},
	ImportEvaluated:        {ImportAwaitingApproval: true, ImportRejected: true},
	ImportAwaitingApproval: {ImportApproved: true, ImportRejected: true},
	ImportApproved:         {ImportRevoked: true},
	ImportRejected:         {ImportRevoked: true},
	ImportRevoked:          {},
}

// ImportTransitionAllowed guards the pipeline CAS.
func ImportTransitionAllowed(from, to string) bool {
	if _, ok := importTransitions[from]; !ok {
		return false
	}
	return importTransitions[from][to]
}

// ImportAuditAction maps a pipeline step to its audit action
// (audit_events CHECK set, 0053).
func ImportAuditAction(state string) string {
	switch state {
	case ImportDiscovered:
		return "skill.import.discovered"
	case ImportPinned:
		return "skill.import.pinned"
	case ImportInspected:
		return "skill.import.inspected"
	case ImportScanned:
		return "skill.import.scanned"
	case ImportEvaluated:
		return "skill.import.evaluated"
	case ImportAwaitingApproval, ImportApproved:
		return "skill.import.approved"
	case ImportRejected:
		return "skill.import.rejected"
	case ImportRevoked:
		return "skill.import.revoked"
	}
	return "skill.import.discovered"
}

// ImportCandidate is one governed import in flight.
type ImportCandidate struct {
	ID                string
	AssetType         string
	SourceURL         string
	ImmutableCommit   string
	ArchiveHash       string
	License           string
	NoticeRef         string
	Publisher         string
	Signature         string
	SourceAttestation string
	ScanRefs          string
	InjectionScan     string
	EvaluationID      string
	Approval          string
	State             string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ImportEvidence carries the step evidence a pipeline transition may
// attach (license notice, signature, attestation, scan references,
// evaluation id, approval record). Empty fields leave the stored column
// untouched — evidence accumulates along the walk, it is never erased.
type ImportEvidence struct {
	NoticeRef         string
	Signature         string
	SourceAttestation string
	ScanRefs          string
	InjectionScan     string
	EvaluationID      string
	Approval          string
}

// ValidateImportInput checks the discovery payload shape.
func ValidateImportInput(assetType, sourceURL, immutableCommit, archiveHash, license, publisher string) error {
	switch assetType {
	case AssetSkill, AssetProfile, AssetPromptBundle:
	default:
		return fmt.Errorf("assetType must be skill|profile|prompt_bundle")
	}
	if len(sourceURL) < 1 || len(sourceURL) > 2048 {
		return fmt.Errorf("sourceUrl length must be 1..2048")
	}
	if len(immutableCommit) < 1 || len(immutableCommit) > 256 {
		return fmt.Errorf("immutableCommit length must be 1..256")
	}
	if len(archiveHash) != 64 || !isLowerHex(archiveHash) {
		return fmt.Errorf("archiveHash must be a 64-char lowercase hex sha-256")
	}
	if len(license) < 1 || len(license) > 128 {
		return fmt.Errorf("license length must be 1..128")
	}
	if len(publisher) < 1 || len(publisher) > 256 {
		return fmt.Errorf("publisher length must be 1..256")
	}
	return nil
}
