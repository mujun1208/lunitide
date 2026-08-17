// M10 memory-operations domain (wave 2, migration 0075): fact flags,
// growth-box observation entries and per-subject memory settings. These are
// sidecars over the 0061 immutable fact chain — no fact version is ever
// rewritten from here.
package m8core

import "errors"

// Memory-ops sentinel errors shared by storage and service layers so the
// service can map them onto M10-MO codes without importing sqlite.
var (
	// ErrFactNotFound: fact id unknown (M10-MO-001).
	ErrFactNotFound = errors.New("memory fact not found")
	// ErrGrowthNotObserving: growth entry missing or already decided (M10-MO-003).
	ErrGrowthNotObserving = errors.New("growth entry not observing")
)

// Fact flags (memory_fact_flags CHECK).
const (
	FlagPinned = "pinned"
	FlagHidden = "hidden"
)

// Growth-box statuses (memory_growth_box CHECK).
const (
	GrowthObserving = "observing"
	GrowthPromoted  = "promoted"
	GrowthDropped   = "dropped"
)

// Settings bounds (memory_settings CHECKs); MaxSubjectID lives in memory.go.
const (
	MinGrowthDays = 1
	MaxGrowthDays = 90
	MaxFlagNote   = 1000
)

// FactFlag is one (fact_id, flag) marker row with an optional note.
type FactFlag struct {
	FactID    string
	Flag      string
	Note      string
	CreatedAt string
	UpdatedAt string
}

// GrowthEntry is one growth-box row: a freshly promoted fact under
// observation until it is promoted to a durable fact or dropped.
type GrowthEntry struct {
	FactID           string
	ScopeID          string
	Status           string
	ReferenceCount   int64
	LastReferencedAt string
	ReviewAt         string
	DecidedAt        string
	CreatedAt        string
	UpdatedAt        string
}

// MemorySettings is the per-subject operations profile.
type MemorySettings struct {
	SubjectID     string
	MemoryEnabled bool
	AutoNominate  bool
	GrowthDays    int
	CreatedAt     string
	UpdatedAt     string
}

// ValidFactFlag reports whether flag is a legal marker kind.
func ValidFactFlag(flag string) bool {
	return flag == FlagPinned || flag == FlagHidden
}

// ValidGrowthStatus reports whether status is a legal growth-box state.
func ValidGrowthStatus(status string) bool {
	return status == GrowthObserving || status == GrowthPromoted || status == GrowthDropped
}

// GrowthTerminal reports whether a growth entry has been decided.
func GrowthTerminal(status string) bool {
	return status == GrowthPromoted || status == GrowthDropped
}

// DefaultMemorySettings returns the implicit profile applied when no row
// exists yet: memory on, auto-nomination off, 14-day observation window.
func DefaultMemorySettings(subjectID string) MemorySettings {
	return MemorySettings{SubjectID: subjectID, MemoryEnabled: true, AutoNominate: false, GrowthDays: 14}
}

// SettingsValidate checks subject/profile invariants.
func SettingsValidate(s MemorySettings) bool {
	if len(s.SubjectID) < 1 || len(s.SubjectID) > MaxSubjectID {
		return false
	}
	return s.GrowthDays >= MinGrowthDays && s.GrowthDays <= MaxGrowthDays
}

// FactRow is the paged fact projection: latest version per fact_id.
type FactRow struct {
	FactID      string
	ScopeID     string
	Version     int64
	Sensitivity string
	State       string
	CreatedAt   string
}

// TraceRow is one recall-trace row; payload JSON passes through verbatim.
type TraceRow struct {
	ID                   string
	QueryDigest          string
	HitsJSON             string
	ReasonsJSON          string
	PolicyRedactionsJSON string
	CreatedAt            string
}

// GroupCount is one (label, count) aggregate bucket.
type GroupCount struct {
	Label string
	Count int
}

// MemoryOpsCounts is the purge result payload.
type MemoryOpsCounts struct {
	FactsTombstoned int64
	Candidates      int64
	GrowthRows      int64
	Flags           int64
	Traces          int64
	Memories        int64
}

// ExportFactVersion is one immutable fact version row (all versions kept).
type ExportFactVersion struct {
	FactID       string
	ScopeID      string
	Version      int64
	Sensitivity  string
	State        string
	SupersededBy string
	DeletedAt    string
	CreatedAt    string
}

// ExportBundle carries every memory surface for the export Bridge.
type ExportBundle struct {
	Facts      []ExportFactVersion
	Leaves     []SourceLeaf
	Candidates []MemoryCandidate
	Traces     []TraceRow
	Growth     []GrowthEntry
	Flags      []FactFlag
	Settings   []MemorySettings
}
