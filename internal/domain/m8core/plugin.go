// M8 FR-18 domain (T-8.9.x): the unified plugin bundle runtime.
//
// Plugins are not a second executor: after the verification chain passes,
// capabilities hot-register into the EXISTING registries (mcp_endpoint /
// m6_registry / tool_registry / persona read-only directory) - this package
// only models the versioned bundle chain, the per-subject install state
// machine and the active->revoked capability bindings. Verification-chain
// failure codes: M8-035 signature/checksum invalid (quarantine, zero
// registration), M8-036 manifest invalid, M8-037 permission beyond grant,
// M8-038 probe failed (retryable), M8-039 permission expansion (409,
// quarantined pending review), M8-040 binding not active (zero side
// effects), M8-041 uninstall chain failure (all-or-nothing rollback).
package m8core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Plugin bundle states (migration 0067 CHECK).
const (
	PluginVerified    = "verified"
	PluginQuarantined = "quarantined"
)

// Signature verification statuses (migration 0067 CHECK).
const (
	SignatureVerified   = "verified"
	SignatureUnverified = "unverified"
	SignatureInvalid    = "invalid"
)

// Install states (migration 0067 CHECK): the safe path
// installed->enabled<->disabled->uninstalled and the security path
// ->quarantined->uninstalled.
const (
	InstallInstalled   = "installed"
	InstallEnabled     = "enabled"
	InstallDisabled    = "disabled"
	InstallQuarantined = "quarantined"
	InstallUninstalled = "uninstalled"
)

// Capability binding states (migration 0067 CHECK).
const (
	BindingActive  = "active"
	BindingRevoked = "revoked"
)

// Plugin kinds (migration 0067 CHECK).
const (
	KindMCP       = "mcp"
	KindSkill     = "skill"
	KindWorkflow  = "workflow"
	KindTemplate  = "template"
	KindTool      = "tool"
	KindAgentPack = "agent-pack"
)

// Hot-registration routing targets (migration 0067 CHECK): each kind
// registers into an EXISTING registry - no new registry is created.
const (
	TargetMCPEndpoint      = "mcp_endpoint"
	TargetM6Registry       = "m6_registry"
	TargetToolRegistry     = "tool_registry"
	TargetTemplate         = "template"
	TargetPersonaDirectory = "persona_directory"
)

// ValidPluginKind answers whether the kind is one of the six bundle kinds.
func ValidPluginKind(kind string) bool {
	switch kind {
	case KindMCP, KindSkill, KindWorkflow, KindTemplate, KindTool, KindAgentPack:
		return true
	}
	return false
}

// RouteTarget answers the hot-registration target registry for one kind
// (FR-18 routing table). agent-pack mounts into the persona read-only
// directory.
func RouteTarget(kind string) string {
	switch kind {
	case KindMCP:
		return TargetMCPEndpoint
	case KindSkill, KindWorkflow:
		return TargetM6Registry
	case KindTool:
		return TargetToolRegistry
	case KindTemplate:
		return TargetTemplate
	case KindAgentPack:
		return TargetPersonaDirectory
	}
	return ""
}

// PluginBundle is one append-only (plugin_id, semver) version row.
type PluginBundle struct {
	BundleID         string
	PluginID         string
	Semver           string
	Publisher        string
	Kind             string
	ManifestRef      string
	Entrypoint       string
	CapabilitiesJSON string
	PermissionsJSON  string
	RequiresJSON     string
	PackageHash      string
	SignatureStatus  string
	State            string
	CreatedAt        string
}

// PluginInstall is one per-subject install row (UNIQUE subject+plugin).
type PluginInstall struct {
	InstallID            string
	BundleID             string
	PluginID             string
	SubjectID            string
	Origin               string
	State                string
	PermissionGrantDigest string
	InstalledAt          string
	UpdatedAt            string
}

// PluginCapabilityBinding is one active->revoked hot-registration binding.
type PluginCapabilityBinding struct {
	BindingID       string
	InstallID       string
	TargetType      string
	TargetID        string
	CapabilityDigest string
	State           string
	CreatedAt       string
	RevokedAt       string
}

// PermissionDoc is the decoded permissions document:
// { "<scope>": ["action", ...], ... } (e.g. {"tool":["http_get"]}).
type PermissionDoc map[string][]string

// ParseSemver splits a strict "major.minor.patch[-pre]" string.
func ParseSemver(v string) (major, minor, patch int64, pre string, err error) {
	core := v
	if i := strings.IndexByte(v, '-'); i >= 0 {
		core, pre = v[:i], v[i+1:]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, "", fmt.Errorf("m8core: semver %q: want major.minor.patch", v)
	}
	nums := make([]int64, 3)
	for i, p := range parts {
		if p == "" {
			return 0, 0, 0, "", fmt.Errorf("m8core: semver %q: empty component", v)
		}
		n, perr := strconv.ParseInt(p, 10, 64)
		if perr != nil || n < 0 {
			return 0, 0, 0, "", fmt.Errorf("m8core: semver %q: non-numeric component", v)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], pre, nil
}

// SemverCompare answers -1/0/+1 comparing two strict semvers; a pre-release
// sorts before its release.
func SemverCompare(a, b string) int {
	amaj, amin, apat, apre, aerr := ParseSemver(a)
	bmaj, bmin, bpat, bpre, berr := ParseSemver(b)
	if aerr != nil || berr != nil {
		return strings.Compare(a, b)
	}
	for _, pair := range [][3]int64{{amaj, bmaj}, {amin, bmin}, {apat, bpat}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	if apre != bpre {
		if apre == "" {
			return 1
		}
		if bpre == "" {
			return -1
		}
		return strings.Compare(apre, bpre)
	}
	return 0
}

// ManifestError marks an invalid plugin manifest (M8-036).
type ManifestError struct{ Reason string }

func (e *ManifestError) Error() string { return "m8core: plugin manifest invalid: " + e.Reason }

// ValidateManifest enacts the manifest schema check (M8-036): plugin id,
// strict semver, publisher, one of the six kinds and a non-empty
// capabilities array.
func ValidateManifest(pluginID, semver, publisher, kind, capabilitiesJSON string) error {
	if len(pluginID) < 1 || len(pluginID) > 128 {
		return &ManifestError{Reason: "pluginId length"}
	}
	if _, _, _, _, err := ParseSemver(semver); err != nil {
		return &ManifestError{Reason: "semver"}
	}
	if len(publisher) < 1 || len(publisher) > 128 {
		return &ManifestError{Reason: "publisher length"}
	}
	if !ValidPluginKind(kind) {
		return &ManifestError{Reason: "kind"}
	}
	var caps []json.RawMessage
	if err := json.Unmarshal([]byte(capabilitiesJSON), &caps); err != nil || len(caps) < 1 {
		return &ManifestError{Reason: "capabilities must be a non-empty array"}
	}
	return nil
}

// PermissionsWithin enacts the whitelist comparison (M8-037): every
// (scope, action) the package requests must sit inside the user grant.
func PermissionsWithin(requested, granted PermissionDoc) bool {
	for scope, actions := range requested {
		gset := map[string]bool{}
		for _, a := range granted[scope] {
			gset[a] = true
		}
		for _, a := range actions {
			if !gset[a] {
				return false
			}
		}
	}
	return true
}

// PermissionsSubset answers whether candidate permissions stay inside the
// authorized set - the M8-039 upgrade expansion test (new version
// permissions must be a subset of the standing grant).
func PermissionsSubset(candidate, authorized PermissionDoc) bool {
	return PermissionsWithin(candidate, authorized)
}

// CanonicalGrantDigest answers the digest of the canonical permission
// grant document (sorted scopes, sorted actions).
func CanonicalGrantDigest(grant PermissionDoc) string {
	scopes := make([]string, 0, len(grant))
	for s := range grant {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)
	var b strings.Builder
	for _, s := range scopes {
		actions := append([]string(nil), grant[s]...)
		sort.Strings(actions)
		b.WriteString(s)
		b.WriteByte(':')
		b.WriteString(strings.Join(actions, ","))
		b.WriteByte(';')
	}
	return DigestOf(b.String())
}

// InstallTransition enacts the install state machine: the safe path
// installed->enabled<->disabled->uninstalled and the security path
// ->quarantined->uninstalled (quarantined only allows uninstall or
// explicit review release - never an automatic recovery).
func InstallTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		InstallInstalled:   {InstallEnabled: true, InstallUninstalled: true},
		InstallEnabled:     {InstallDisabled: true, InstallUninstalled: true, InstallQuarantined: true},
		InstallDisabled:    {InstallEnabled: true, InstallUninstalled: true, InstallQuarantined: true},
		InstallQuarantined: {InstallUninstalled: true},
		InstallUninstalled: {},
	}
	if allowed[from] != nil && allowed[from][to] {
		return nil
	}
	return fmt.Errorf("m8core: install transition %s->%s not allowed", from, to)
}
