// M8 FR-19 domain (T-8.10.x): the expert center.
//
// Experts are the productized form of the M7 AgentPersona job description.
// This package models the catalog state machine (enabled<->disabled->
// archived, archived terminal), the append-only ExpertVersion WORM chain
// (six_section_digest = sha256 of the canonical six-section body), the
// project-phase mounting (version pinned at mount time, <=4 mounted
// experts per phase) and the nine-phase default mapping carried over from
// the M7 recommendation. The six-section validation chain (frontmatter
// schema -> section bounds -> prompt-injection scan -> digest) fails as
// M8-042 with zero persistence. Experts carry no permissions: readCaps
// stay decided by capability_digest alone (M7 orthogonality).
package m8core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Expert catalog states (migration 0068 CHECK).
const (
	ExpertEnabled  = "enabled"
	ExpertDisabled = "disabled"
	ExpertArchived = "archived"
)

// Mounting states (migration 0068 CHECK).
const (
	MountingMounted   = "mounted"
	MountingUnmounted = "unmounted"
)

// Expert sources (migration 0068 CHECK): pack experts follow the FR-18
// plugin lifecycle, local experts are six-section creations, builtin
// experts ship with M7 (disable allowed, archive forbidden).
const (
	ExpertSourcePack    = "pack"
	ExpertSourceLocal   = "local"
	ExpertSourceBuiltin = "builtin"
)

// The M7 closed eight-skeleton division whitelist.
const (
	DivisionEngineering       = "engineering"
	DivisionDesign            = "design"
	DivisionProduct           = "product"
	DivisionProjectManagement = "project-management"
	DivisionTesting           = "testing"
	DivisionSecurity          = "security"
	DivisionOperations        = "operations"
	DivisionData              = "data"
)

// The M7 fixed nine phase keys.
const (
	PhaseInitiationBoundary      = "INITIATION_BOUNDARY"
	PhaseResearchEvidence        = "RESEARCH_EVIDENCE"
	PhaseRequirementDefinition   = "REQUIREMENT_DEFINITION"
	PhaseSolutionExperience      = "SOLUTION_EXPERIENCE"
	PhaseArchitecturePlan        = "ARCHITECTURE_PLAN"
	PhaseDevelopmentChange       = "DEVELOPMENT_CHANGE"
	PhaseVerificationAcceptance  = "VERIFICATION_ACCEPTANCE"
	PhaseReleaseDelivery         = "RELEASE_DELIVERY"
	PhaseOperationsRetrospective = "OPERATIONS_RETROSPECTIVE"
)

// PhaseKeys lists the nine fixed phase keys in canonical order.
var PhaseKeys = []string{
	PhaseInitiationBoundary, PhaseResearchEvidence, PhaseRequirementDefinition,
	PhaseSolutionExperience, PhaseArchitecturePlan, PhaseDevelopmentChange,
	PhaseVerificationAcceptance, PhaseReleaseDelivery, PhaseOperationsRetrospective,
}

// MaxMountedExpertsPerPhase is the per-(project, phase) mounted cap
// (migration 0068 trg_mount_limit, M8-044).
const MaxMountedExpertsPerPhase = 4

// ValidDivision answers whether the division sits in the eight skeleton.
func ValidDivision(division string) bool {
	switch division {
	case DivisionEngineering, DivisionDesign, DivisionProduct,
		DivisionProjectManagement, DivisionTesting, DivisionSecurity,
		DivisionOperations, DivisionData:
		return true
	}
	return false
}

// ValidPhaseKey answers whether the phase key is one of the nine.
func ValidPhaseKey(key string) bool {
	for _, k := range PhaseKeys {
		if k == key {
			return true
		}
	}
	return false
}

// ExpertCatalog is one expert directory row (UNIQUE subject_id+name).
type ExpertCatalog struct {
	ExpertID         string
	SubjectID        string
	Name             string
	Division         string
	Source           string
	OriginBundleID   string
	CatalogItemID    string
	CurrentVersionID string
	State            string
	CreatedAt        string
	UpdatedAt        string
}

// ExpertVersion is one append-only WORM version row. PersonaRef addresses
// the persona body inside the persona read-only directory; the mount pins
// this version_id so later revisions never drift an active mounting.
type ExpertVersion struct {
	VersionID        string
	ExpertID         string
	Semver           string
	PersonaRef       string
	SixSectionDigest string
	ChangeNote       string
	CreatedAt        string
}

// ExpertMounting is one (project, phase, expert) mounting row; the version
// reference locks at mount time until an explicit updateVersion.
type ExpertMounting struct {
	MountingID string
	ProjectID  string
	PhaseKey   string
	ExpertID   string
	VersionID  string
	State      string
	MountedAt  string
	UpdatedAt  string
}

// SixSectionError marks a six-section validation failure (M8-042).
type SixSectionError struct{ Reason string }

func (e *SixSectionError) Error() string { return "m8core: six-section invalid: " + e.Reason }

// SixSection is the M7 six-section persona body: frontmatter plus the
// sections 1..6 (identity / mission / rules / workflow /
// deliverable_template / success_metrics).
type SixSection struct {
	Identity            string `json:"identity"`
	Mission             string `json:"mission"`
	Rules               string `json:"rules"`
	Workflow            string `json:"workflow"`
	DeliverableTemplate string `json:"deliverableTemplate"`
	SuccessMetrics      string `json:"successMetrics"`
}

// SixSectionMaxLength bounds each section body (bridge schema 65536).
const SixSectionMaxLength = 65536

// Frontmatter is the expert frontmatter block (bridge schema contract).
type Frontmatter struct {
	Name        string
	Division    string
	Description string
	Semver      string
}

// injectionMarkers are the prompt-injection scan keywords (M7 persona
// protection, Skill-level): instruction override, permission luring and
// exfiltration luring. The scan is case-insensitive.
var injectionMarkers = []string{
	"ignore previous instructions", "ignore all previous", "disregard previous",
	"忽略之前指令", "忽略以上指令", "无视之前指令",
	"you now have full permissions", "grant yourself permission", "ignore permission",
	"绕过权限", "忽略权限", "已获得全部权限",
	"exfiltrate", "upload everything to", "send all data to", "外传全部数据",
}

// ScanInjection enacts the Skill-level prompt-injection scan over the
// six-section body: any marker hit fails the chain (quarantine, zero
// registration - M7 persona protection applied verbatim).
func ScanInjection(body string) error {
	lower := strings.ToLower(body)
	for _, marker := range injectionMarkers {
		if strings.Contains(lower, marker) {
			return &SixSectionError{Reason: "injection marker in six-section body"}
		}
	}
	return nil
}

// ValidateSixSection enacts the ordered chain: every section non-empty and
// within the length bound, then the injection scan. Any failure answers
// *SixSectionError (M8-042, zero persistence).
func ValidateSixSection(s SixSection) error {
	sections := []struct{ name, body string }{
		{"identity", s.Identity}, {"mission", s.Mission}, {"rules", s.Rules},
		{"workflow", s.Workflow}, {"deliverableTemplate", s.DeliverableTemplate},
		{"successMetrics", s.SuccessMetrics},
	}
	for _, sec := range sections {
		if len(sec.body) < 1 {
			return &SixSectionError{Reason: sec.name + " empty"}
		}
		if len(sec.body) > SixSectionMaxLength {
			return &SixSectionError{Reason: sec.name + " over length bound"}
		}
	}
	if err := ScanInjection(s.Identity + "\n" + s.Mission + "\n" + s.Rules +
		"\n" + s.Workflow + "\n" + s.DeliverableTemplate + "\n" + s.SuccessMetrics); err != nil {
		return err
	}
	return nil
}

// CanonicalJSON answers the canonical encoding of the six-section body
// (sorted keys, no whitespace) - the digest preimage.
func (s SixSection) CanonicalJSON() string {
	// Field order follows the sorted JSON keys: deliverableTemplate,
	// identity, mission, rules, successMetrics, workflow.
	b, err := json.Marshal(struct {
		DeliverableTemplate string `json:"deliverableTemplate"`
		Identity            string `json:"identity"`
		Mission             string `json:"mission"`
		Rules               string `json:"rules"`
		SuccessMetrics      string `json:"successMetrics"`
		Workflow            string `json:"workflow"`
	}{s.DeliverableTemplate, s.Identity, s.Mission, s.Rules, s.SuccessMetrics, s.Workflow})
	if err != nil {
		return ""
	}
	return string(b)
}

// SixSectionDigest answers sha256(canonical six-section body).
func (s SixSection) SixSectionDigest() string { return DigestOf(s.CanonicalJSON()) }

// PersonaRef answers the persona reference digest binding frontmatter and
// body: sha256 over the canonical frontmatter|six-section preimage. The
// value addresses the persona body inside the persona read-only store.
func (f Frontmatter) PersonaRef(s SixSection) string {
	preimage := f.Name + "|" + f.Division + "|" + s.CanonicalJSON()
	return DigestOf(preimage)
}

// ValidateFrontmatter enacts the frontmatter schema check (bridge schema):
// name 1..128, division inside the eight skeleton, description 1..2000,
// strict semver.
func (f Frontmatter) Validate() error {
	if len(f.Name) < 1 || len(f.Name) > 128 {
		return &SixSectionError{Reason: "frontmatter name length"}
	}
	if !ValidDivision(f.Division) {
		return &SixSectionError{Reason: "frontmatter division"}
	}
	if len(f.Description) < 1 || len(f.Description) > 2000 {
		return &SixSectionError{Reason: "frontmatter description length"}
	}
	if _, _, _, _, err := ParseSemver(f.Semver); err != nil {
		return &SixSectionError{Reason: "frontmatter semver"}
	}
	return nil
}

// ExpertTransition enacts the catalog state machine enabled<->disabled->
// archived. archived is terminal (no recovery path); archived refuses
// every transition.
func ExpertTransition(from, to string) error {
	allowed := map[string]map[string]bool{
		ExpertEnabled:  {ExpertDisabled: true, ExpertArchived: true},
		ExpertDisabled: {ExpertEnabled: true, ExpertArchived: true},
		ExpertArchived: {},
	}
	if allowed[from] != nil && allowed[from][to] {
		return nil
	}
	return fmt.Errorf("m8core: expert transition %s->%s not allowed", from, to)
}

// BumpPatch answers the next patch-bumped semver of a strict version
// (1.2.3 -> 1.2.4, preserving any pre-release absent).
func BumpPatch(semver string) string {
	major, minor, patch, _, err := ParseSemver(semver)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

// PhaseDefault is one nine-phase default mapping entry: the recommended
// division order carried from the M7 recommendation table (advisory
// defaults, never a forced binding).
type PhaseDefault struct {
	PhaseKey  string
	Divisions []string
}

// DefaultPhaseMapping is the M7 nine-phase recommended division mapping.
var DefaultPhaseMapping = []PhaseDefault{
	{PhaseInitiationBoundary, []string{DivisionProduct}},
	{PhaseResearchEvidence, []string{DivisionDesign, DivisionEngineering}},
	{PhaseRequirementDefinition, []string{DivisionProduct}},
	{PhaseSolutionExperience, []string{DivisionDesign}},
	{PhaseArchitecturePlan, []string{DivisionEngineering}},
	{PhaseDevelopmentChange, []string{DivisionEngineering}},
	{PhaseVerificationAcceptance, []string{DivisionEngineering, DivisionTesting, DivisionSecurity}},
	{PhaseReleaseDelivery, []string{DivisionEngineering}},
	{PhaseOperationsRetrospective, []string{DivisionEngineering}},
}

// ArchiveConfirmToken answers the deterministic archive confirmation token
// bound to one expertId (the client echoes DigestOf("expert.archive|<id>")
// - one-time because archived is terminal).
func ArchiveConfirmToken(expertID string) string {
	return DigestOf("expert.archive|" + expertID)
}
