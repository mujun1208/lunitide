// Package m7flow holds the M7 nine-stage workflow domain values (T-7.1.x):
// workflow versions, the globally fixed stage definitions, stage runs,
// input snapshots and immutable artifact versions. State strings mirror the
// CHECK constraints in migrations 0051+; the nine stage keys are a global
// fixed set — renaming, adding or skipping one is a schema-level change.
package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var (
	// ErrNotFound answers any missing workflow/stage/snapshot lookup.
	ErrNotFound = errors.New("m7flow: entity not found")
	// ErrVersionConflict is the optimistic-lock guard (WF-004/WF-005).
	ErrVersionConflict = errors.New("m7flow: version conflict")
	// ErrStageFixedSet: the requested stage set does not match the nine
	// fixed keys (WF-002).
	ErrStageFixedSet = errors.New("m7flow: stage set does not match the fixed nine")
	// ErrStageCycle: dependency graph would form a cycle (WF-003).
	ErrStageCycle = errors.New("m7flow: stage dependency cycle")
	// ErrAlreadyPublished: cannot mutate a published version in place
	// (WF-001 — clone a new version instead).
	ErrAlreadyPublished = errors.New("m7flow: workflow version already published")
	// ErrNotPublished: instances can only bind published versions.
	ErrNotPublished = errors.New("m7flow: workflow version not published")
	// ErrSnapshotIncomplete: an input lacks sourceRef or digest (SNP-001).
	ErrSnapshotIncomplete = errors.New("m7flow: snapshot input missing sourceRef/digest")
	// ErrSubprocessAsStage: integration/security/package/deployment tried
	// to register as a stage ordinal — they are stage-6..8 internals.
	ErrSubprocessAsStage = errors.New("m7flow: subprocess cannot register as a stage")
)

// StageKey is one of the nine fixed stage keys.
type StageKey string

const (
	StageInitiation          StageKey = "INITIATION_BOUNDARY"
	StageResearch            StageKey = "RESEARCH_EVIDENCE"
	StageRequirement         StageKey = "REQUIREMENT_DEFINITION"
	StageSolution            StageKey = "SOLUTION_EXPERIENCE"
	StageArchitecture        StageKey = "ARCHITECTURE_PLAN"
	StageDevelopment         StageKey = "DEVELOPMENT_CHANGE"
	StageVerification        StageKey = "VERIFICATION_ACCEPTANCE"
	StageRelease             StageKey = "RELEASE_DELIVERY"
	StageOperations          StageKey = "OPERATIONS_RETROSPECTIVE"
)

// subprocessKeys are internal gate/subflow identifiers that must never
// occupy a stage ordinal (M7 PRD §02 note).
var subprocessKeys = map[string]bool{
	"integration": true, "security": true, "package": true, "deployment": true,
}

// fixedStage binds key, Chinese name and direct dependencies. The order and
// dependency chain are the single global truth; DefinitionDigest is computed
// over exactly this content.
var fixedStages = []struct {
	Key  StageKey
	Name string
	Deps []StageKey
}{
	{StageInitiation, "立项与边界", nil},
	{StageResearch, "调研与证据", []StageKey{StageInitiation}},
	{StageRequirement, "需求定义", []StageKey{StageResearch}},
	{StageSolution, "方案与体验", []StageKey{StageRequirement}},
	{StageArchitecture, "架构与计划", []StageKey{StageSolution}},
	{StageDevelopment, "开发与变更", []StageKey{StageArchitecture}},
	{StageVerification, "验证与验收", []StageKey{StageDevelopment}},
	{StageRelease, "发布与交付", []StageKey{StageVerification}},
	{StageOperations, "运营与复盘", []StageKey{StageRelease}},
}

// FixedStages returns the nine fixed definitions with 1-based ordinals and
// default gate policies (stage 7 carries the M6→7 readiness gate).
func FixedStages() []StageDefinition {
	out := make([]StageDefinition, 0, len(fixedStages))
	for i, s := range fixedStages {
		deps := make([]string, 0, len(s.Deps))
		for _, d := range s.Deps {
			deps = append(deps, string(d))
		}
		gates := []string{"stage.exit"}
		switch s.Key {
		case StageDevelopment:
			gates = []string{"stage.exit", "dev.integration"}
		case StageVerification:
			gates = []string{"stage.exit", "verify.security"}
		case StageRelease:
			gates = []string{"stage.exit", "release.package"}
		}
		pol, _ := json.Marshal(map[string]any{"requiredGates": gates})
		out = append(out, StageDefinition{
			StageKey: string(s.Key), Ordinal: i + 1, Name: s.Name,
			DependencyKeys: canonicalJSON(deps), GatePolicy: string(pol),
		})
	}
	return out
}

// ValidateFixedSet checks that defs is exactly the nine fixed keys with the
// fixed dependency DAG (no adds/removes/renames/skips). It fails with
// ErrSubprocessAsStage when an internal subprocess key tries to pass.
func ValidateFixedSet(defs []StageDefinition) error {
	if len(defs) != len(fixedStages) {
		return ErrStageFixedSet
	}
	seen := map[string]bool{}
	for _, d := range defs {
		if subprocessKeys[d.StageKey] {
			return ErrSubprocessAsStage
		}
		seen[d.StageKey] = true
	}
	for _, s := range fixedStages {
		if !seen[string(s.Key)] {
			return ErrStageFixedSet
		}
	}
	// Dependency DAG check against the fixed chain: every declared dep must
	// exist and the graph must stay acyclic (topological walk).
	return ValidateDAG(defs)
}

// ValidateDAG verifies every dependency exists and the graph is acyclic;
// cycle members are reported through ErrStageCycle.
func ValidateDAG(defs []StageDefinition) error {
	deps := map[string][]string{}
	for _, d := range defs {
		if _, dup := deps[d.StageKey]; dup {
			return ErrStageFixedSet
		}
		ds := []string{}
		if err := json.Unmarshal([]byte(d.DependencyKeys), &ds); err != nil {
			return ErrStageFixedSet
		}
		for _, dep := range ds {
			if subprocessKeys[dep] {
				return ErrSubprocessAsStage
			}
		}
		deps[d.StageKey] = ds
	}
	for k, ds := range deps {
		for _, dep := range ds {
			if _, ok := deps[dep]; !ok {
				return ErrStageFixedSet
			}
			if dep == k {
				return ErrStageCycle
			}
		}
	}
	// Kahn walk.
	indeg := map[string]int{}
	for k, ds := range deps {
		indeg[k] = len(ds)
	}
	queue := []string{}
	for k, d := range indeg {
		if d == 0 {
			queue = append(queue, k)
		}
	}
	visited := 0
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		visited++
		for other, ds := range deps {
			for _, dep := range ds {
				if dep == k {
					indeg[other]--
					if indeg[other] == 0 {
						queue = append(queue, other)
					}
				}
			}
		}
	}
	if visited != len(deps) {
		return ErrStageCycle
	}
	return nil
}

// canonicalJSON marshals v with sorted keys and no insignificant whitespace.
func canonicalJSON(v any) string {
	b, _ := json.Marshal(v) // map marshalling is already key-sorted in Go
	return string(b)
}

// DefinitionDigest hashes the canonical nine-stage definition (key, ordinal,
// name, deps, gatePolicy in fixed order) — the workflow's content address.
func DefinitionDigest(defs []StageDefinition) string {
	type row struct {
		Key      string   `json:"key"`
		Ordinal  int      `json:"ordinal"`
		Name     string   `json:"name"`
		Deps     []string `json:"deps"`
		Policy   string   `json:"gatePolicy"`
	}
	rows := make([]row, 0, len(defs))
	sorted := append([]StageDefinition(nil), defs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ordinal < sorted[j].Ordinal })
	for _, d := range sorted {
		deps := []string{}
		_ = json.Unmarshal([]byte(d.DependencyKeys), &deps)
		rows = append(rows, row{d.StageKey, d.Ordinal, d.Name, deps, d.GatePolicy})
	}
	sum := sha256.Sum256([]byte(canonicalJSON(rows)))
	return hex.EncodeToString(sum[:])
}

// WorkflowVersion states.
const (
	WVDraft     = "draft"
	WVPublished = "published"
)

// WorkflowVersion is one immutable-after-publish definition set.
type WorkflowVersion struct {
	ID               string
	ProjectID        string
	Version          int64
	Status           string
	DefinitionDigest string
	CreatedAt        time.Time
	PublishedAt      *time.Time
}

// StageDefinition is one of the nine fixed stages bound to a version.
type StageDefinition struct {
	ID              string
	WorkflowVersion string
	StageKey        string
	Ordinal         int
	Name            string
	DependencyKeys  string // canonical JSON array
	GatePolicy      string // canonical JSON object
}

// StageRun states (V2 canonical set; stale is a projection, never stored).
const (
	RunDraft         = "draft"
	RunReady         = "ready"
	RunRunning       = "running"
	RunWaitingReview = "waiting_review"
	RunApproved      = "approved"
	RunCompleted     = "completed"
	RunBlocked       = "blocked"
	RunPaused        = "paused"
	RunCancelled     = "cancelled"
)

// terminalRunStates close an attempt (a fresh attempt may then start).
var terminalRunStates = map[string]bool{RunCompleted: true, RunCancelled: true}

// IsTerminalRun reports whether the state closes the stage-run attempt.
func IsTerminalRun(state string) bool { return terminalRunStates[state] }

// stageTransitions is the legal StageRun state machine.
var stageTransitions = map[string]map[string]bool{
	RunDraft:         {RunReady: true, RunBlocked: true, RunCancelled: true},
	RunReady:         {RunRunning: true, RunBlocked: true, RunCancelled: true},
	RunRunning:       {RunWaitingReview: true, RunBlocked: true, RunPaused: true, RunCancelled: true},
	RunWaitingReview: {RunApproved: true, RunRunning: true, RunCancelled: true},
	RunApproved:      {RunCompleted: true, RunCancelled: true},
	RunBlocked:       {RunRunning: true, RunCancelled: true},
	RunPaused:        {RunRunning: true, RunCancelled: true},
	RunCompleted:     {},
	RunCancelled:     {},
}

// LegalRunTransition guards the StageRun state machine (event-side guard;
// the DB partial unique index keeps at most one active attempt per stage).
func LegalRunTransition(from, to string) bool {
	return stageTransitions[from][to]
}

// WorkflowInstance states.
const (
	InstanceRunning   = "running"
	InstanceCompleted = "completed"
	InstanceCancelled = "cancelled"
)

// WorkflowInstance binds a project to one exact published version.
type WorkflowInstance struct {
	ID                string
	ProjectID         string
	WorkflowVersionID string
	State             string
	CreatedAt         time.Time
	CompletedAt       *time.Time
}

// StageRun is one attempt of one stage inside an instance.
type StageRun struct {
	ID                 string
	InstanceID         string
	StageDefinitionID  string
	AttemptNo          int64
	State              string
	LockVersion        int64
	StartedAt          *time.Time
	CompletedAt        *time.Time
	CreatedAt          time.Time
}

// InputSnapshot is one append-only canonical capture of stage inputs.
type InputSnapshot struct {
	ID         string
	StageRunID string
	InputsJSON string // canonical (sorted keys, no whitespace)
	Digest     string // sha256 over InputsJSON
	CapturedAt time.Time
}

// ArtifactVersion states.
const (
	ArtifactActive     = "active"
	ArtifactSuperseded = "superseded"
)

// Artifact kinds (artifact_versions.kind CHECK set).
const (
	KindDocument   = "document"
	KindPatch      = "patch"
	KindTestReport = "test_report"
	KindScanReport = "scan_report"
	KindPackage    = "package"
	KindSBOM       = "sbom"
	KindOther      = "other"
)

// ArtifactVersion is one immutable content-addressed artifact record.
// UPDATE/DELETE are rejected by trigger M7-ART-001; superseding is a new
// row (version_no+1), never an edit.
type ArtifactVersion struct {
	ID         string
	ArtifactID string
	VersionNo  int64
	Kind       string
	ScopeType  string // project | stage_run | dev_task | release | m6_root
	ScopeID    string
	ContentRef string
	SHA256     string
	Size       int64
	MediaType  string
	State      string
	CreatedBy  string
	CreatedAt  time.Time
}

// SnapshotInput is one entry of a snapshot's inputs array. M6→M7 adaptation:
// sourceRef carries rootId, digest carries the final-tree digest.
type SnapshotInput struct {
	SourceRef string `json:"sourceRef"`
	Digest    string `json:"digest"`
	Kind      string `json:"kind,omitempty"`
}

// NormalizeInputs canonicalizes the inputs object (sorted keys, no
// whitespace) and returns the JSON text plus its sha-256 digest. The same
// logical input always yields the same digest — snapshots are comparable.
func NormalizeInputs(inputs map[string]any) (string, string, error) {
	if inputs == nil {
		inputs = map[string]any{}
	}
	b, err := json.Marshal(inputs)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	return string(b), hex.EncodeToString(sum[:]), nil
}
