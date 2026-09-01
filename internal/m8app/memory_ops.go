// M10 memory-operations service (wave 2, migration 0075): stats, fact
// browsing with flags, recall-trace browsing, growth-box decisions,
// settings, export and the one-shot purge. Read paths are sidecar-only:
// the 0061 immutable fact chain is never rewritten; purge tombstones facts
// instead of deleting versions.
package m8app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M10 memory-operations error family (M10-MO-001~005).
var (
	// ErrOpsFactNotFound: fact id unknown (M10-MO-001, 404).
	ErrOpsFactNotFound = errors.New("m8app: memory fact not found")
	// ErrOpsFlagInvalid: flag kind or note bounds (M10-MO-002, 422).
	ErrOpsFlagInvalid = errors.New("m8app: fact flag invalid")
	// ErrOpsGrowthConflict: entry missing or already decided (M10-MO-003, 409).
	ErrOpsGrowthConflict = errors.New("m8app: growth entry conflict")
	// ErrOpsSettingsInvalid: profile bounds (M10-MO-004, 422).
	ErrOpsSettingsInvalid = errors.New("m8app: memory settings invalid")
	// ErrOpsDecisionInvalid: decision not promote/drop (M10-MO-005, 422).
	ErrOpsDecisionInvalid = errors.New("m8app: growth decision invalid")
)

// OpsStore is the persistence surface for memory operations (backed by
// *sqlite.Store via the migration 0075 methods).
type OpsStore interface {
	GetMemorySettings(ctx context.Context, subjectID string) (m8core.MemorySettings, error)
	UpsertMemorySettings(ctx context.Context, settings m8core.MemorySettings) error
	ListFactsPaged(ctx context.Context, state, scope string, limit, offset int) ([]m8core.FactRow, int, error)
	SetFactFlag(ctx context.Context, factID, flag, note string) error
	ClearFactFlag(ctx context.Context, factID, flag string) error
	ListFactFlags(ctx context.Context) ([]m8core.FactFlag, error)
	ListRecallTracesPaged(ctx context.Context, limit, offset int) ([]m8core.TraceRow, int, error)
	UpsertGrowthEntry(ctx context.Context, entry m8core.GrowthEntry) error
	ListGrowthEntries(ctx context.Context, status string, limit, offset int) ([]m8core.GrowthEntry, int, error)
	DecideGrowthEntry(ctx context.Context, factID, decision string) error
	CountFactsBy(ctx context.Context, column string) ([]m8core.GroupCount, error)
	CountCandidatesBy(ctx context.Context) ([]m8core.GroupCount, error)
	CountGrowthBy(ctx context.Context) ([]m8core.GroupCount, error)
	CountTracesSince(ctx context.Context, since time.Time) (int, error)
	CountMemories(ctx context.Context) (int, error)
	PurgeAllMemoryData(ctx context.Context) (m8core.MemoryOpsCounts, error)
	ExportAllMemoryData(ctx context.Context) (m8core.ExportBundle, error)
}

// MemoryOpsStats is the stats-bar payload (mh stats aggregation).
type MemoryOpsStats struct {
	FactsByState       map[string]int
	FactsBySensitivity map[string]int
	CandidatesByState  map[string]int
	GrowthByStatus     map[string]int
	TracesTotal        int
	TracesLast7Days    int
	MemoriesTotal      int
}

// FactView is one fact row merged with its flags.
type FactView struct {
	m8core.FactRow
	Pinned bool
	Hidden bool
	Note   string
}

// MemoryOpsService implements the M10 memory-operations use cases.
type MemoryOpsService struct {
	store OpsStore
}

// NewMemoryOpsService wires the service over the ops store.
func NewMemoryOpsService(store OpsStore) *MemoryOpsService {
	return &MemoryOpsService{store: store}
}

// Stats aggregates every memory surface for the dashboard strip.
func (s *MemoryOpsService) Stats(ctx context.Context) (MemoryOpsStats, error) {
	stats := MemoryOpsStats{
		FactsByState: map[string]int{}, FactsBySensitivity: map[string]int{},
		CandidatesByState: map[string]int{}, GrowthByStatus: map[string]int{},
	}
	if s == nil || s.store == nil {
		return stats, ErrServiceUnavailable
	}
	fill := func(groups []m8core.GroupCount, into map[string]int) {
		for _, g := range groups {
			into[g.Label] = g.Count
		}
	}
	if groups, err := s.store.CountFactsBy(ctx, "state"); err != nil {
		return stats, err
	} else {
		fill(groups, stats.FactsByState)
	}
	if groups, err := s.store.CountFactsBy(ctx, "sensitivity"); err != nil {
		return stats, err
	} else {
		fill(groups, stats.FactsBySensitivity)
	}
	if groups, err := s.store.CountCandidatesBy(ctx); err != nil {
		return stats, err
	} else {
		fill(groups, stats.CandidatesByState)
	}
	if groups, err := s.store.CountGrowthBy(ctx); err != nil {
		return stats, err
	} else {
		fill(groups, stats.GrowthByStatus)
	}
	if traces, err := s.store.CountTracesSince(ctx, time.Now().UTC().AddDate(0, 0, -7)); err != nil {
		return stats, err
	} else {
		stats.TracesLast7Days = traces
	}
	if n, err := s.countTraces(ctx); err != nil {
		return stats, err
	} else {
		stats.TracesTotal = n
	}
	if n, err := s.store.CountMemories(ctx); err != nil {
		return stats, err
	} else {
		stats.MemoriesTotal = n
	}
	return stats, nil
}

func (s *MemoryOpsService) countTraces(ctx context.Context) (int, error) {
	_, total, err := s.store.ListRecallTracesPaged(ctx, 0, 0)
	return total, err
}

// Facts returns the newest version of each fact merged with its flags.
func (s *MemoryOpsService) Facts(ctx context.Context, state, scope string, limit, offset int) ([]FactView, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, ErrServiceUnavailable
	}
	rows, total, err := s.store.ListFactsPaged(ctx, state, scope, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	flags, err := s.store.ListFactFlags(ctx)
	if err != nil {
		return nil, 0, err
	}
	type flagPair struct {
		pinned, hidden bool
		note           string
	}
	flagged := map[string]flagPair{}
	for _, f := range flags {
		pair := flagged[f.FactID]
		switch f.Flag {
		case m8core.FlagPinned:
			pair.pinned, pair.note = true, f.Note
		case m8core.FlagHidden:
			pair.hidden, pair.note = true, f.Note
		}
		flagged[f.FactID] = pair
	}
	views := make([]FactView, 0, len(rows))
	for _, r := range rows {
		pair := flagged[r.FactID]
		views = append(views, FactView{FactRow: r, Pinned: pair.pinned, Hidden: pair.hidden, Note: pair.note})
	}
	return views, total, nil
}

// FlagFact sets or clears one marker on a fact.
func (s *MemoryOpsService) FlagFact(ctx context.Context, factID, flag, note string, on bool) error {
	if s == nil || s.store == nil {
		return ErrServiceUnavailable
	}
	if !m8core.ValidFactFlag(flag) || len(note) > m8core.MaxFlagNote {
		return ErrOpsFlagInvalid
	}
	if on {
		return s.store.SetFactFlag(ctx, factID, flag, note)
	}
	return s.store.ClearFactFlag(ctx, factID, flag)
}

// Traces returns recall traces newest first.
func (s *MemoryOpsService) Traces(ctx context.Context, limit, offset int) ([]m8core.TraceRow, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, ErrServiceUnavailable
	}
	return s.store.ListRecallTracesPaged(ctx, limit, offset)
}

// GrowthList returns growth-box entries by optional status.
func (s *MemoryOpsService) GrowthList(ctx context.Context, status string, limit, offset int) ([]m8core.GrowthEntry, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, ErrServiceUnavailable
	}
	if status != "" && !m8core.ValidGrowthStatus(status) {
		return nil, 0, ErrOpsDecisionInvalid
	}
	return s.store.ListGrowthEntries(ctx, status, limit, offset)
}

// GrowthDecide promotes or drops an observing entry.
func (s *MemoryOpsService) GrowthDecide(ctx context.Context, factID, decision string) error {
	if s == nil || s.store == nil {
		return ErrServiceUnavailable
	}
	if decision != m8core.GrowthPromoted && decision != m8core.GrowthDropped {
		return ErrOpsDecisionInvalid
	}
	err := s.store.DecideGrowthEntry(ctx, factID, decision)
	if errors.Is(err, m8core.ErrGrowthNotObserving) {
		return ErrOpsGrowthConflict
	}
	if errors.Is(err, m8core.ErrFactNotFound) {
		return ErrOpsFactNotFound
	}
	return err
}

// SettingsGet returns the subject profile (implicit defaults when absent).
func (s *MemoryOpsService) SettingsGet(ctx context.Context, subjectID string) (m8core.MemorySettings, error) {
	if s == nil || s.store == nil {
		return m8core.MemorySettings{}, ErrServiceUnavailable
	}
	if len(subjectID) < 1 || len(subjectID) > m8core.MaxSubjectID {
		return m8core.MemorySettings{}, ErrOpsSettingsInvalid
	}
	return s.store.GetMemorySettings(ctx, subjectID)
}

// SettingsUpdate validates and persists the profile.
func (s *MemoryOpsService) SettingsUpdate(ctx context.Context, settings m8core.MemorySettings) error {
	if s == nil || s.store == nil {
		return ErrServiceUnavailable
	}
	if !m8core.SettingsValidate(settings) {
		return ErrOpsSettingsInvalid
	}
	return s.store.UpsertMemorySettings(ctx, settings)
}

// Export returns the full memory bundle for download.
func (s *MemoryOpsService) Export(ctx context.Context) (m8core.ExportBundle, error) {
	if s == nil || s.store == nil {
		return m8core.ExportBundle{}, ErrServiceUnavailable
	}
	return s.store.ExportAllMemoryData(ctx)
}

// Purge clears every memory surface in one audited transaction.
func (s *MemoryOpsService) Purge(ctx context.Context) (m8core.MemoryOpsCounts, error) {
	if s == nil || s.store == nil {
		return m8core.MemoryOpsCounts{}, ErrServiceUnavailable
	}
	return s.store.PurgeAllMemoryData(ctx)
}
