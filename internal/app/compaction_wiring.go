package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/handoffapp"
)

// CompactionStore combines the storage interfaces needed by the compaction
// trigger, executor, and summary reader. A single Store implementation
// typically satisfies all of these.
type CompactionStore interface {
	compactionapp.ExecutorStore
	compactionapp.SourceReader
	compactionapp.CheckpointStore
	CompactionSummaryReader
}

// compactionProviderLookup adapts the Engine's provider service to the
// compactionapp.ProviderLookup interface.
type compactionProviderLookup struct {
	e *Engine
}

func (l *compactionProviderLookup) GetProvider(ctx context.Context, id string) (provider.Provider, error) {
	return l.e.providers.Get(ctx, id)
}

// compactionAdapterFactory adapts the Engine's adapter() method to the
// compactionapp.AdapterFactory interface.
type compactionAdapterFactory struct {
	e *Engine
}

func (f *compactionAdapterFactory) Adapter(ctx context.Context, p provider.Provider) (gateway.Adapter, error) {
	return f.e.adapter(ctx, p)
}

// SetupCompactionServices wires the compaction trigger, executor, and summary
// reader into the engine using the engine's own provider, lease, and adapter
// infrastructure. This should be called during engine initialization after
// the store, tokenRepo, and lease client are available.
//
// The store must satisfy CompactionStore (executor + source + checkpoint +
// summary reader). The messageReader must be a compactionapp.MessageReader
// (typically store.CompactionMessageReader()).
func (e *Engine) SetupCompactionServices(store CompactionStore, messageReader compactionapp.MessageReader) {
	if store == nil || messageReader == nil {
		return
	}
	if e.tokenRepo == nil || e.leases == nil {
		return
	}

	// LeaseClient satisfies compactionapp.LeaseAcquirer structurally.
	summarizer := compactionapp.NewGatewaySummarizer(
		&compactionProviderLookup{e: e},
		e.leases,
		&compactionAdapterFactory{e: e},
		compactionapp.DefaultGatewaySummarizerConfig(""),
	)

	executor := compactionapp.NewExecutor(store, store, summarizer)
	trigger := compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), e.tokenRepo, store, messageReader)

	e.SetCompactionServices(trigger, executor, store)
}

// HandoffStore combines the storage interfaces needed by the handoff capsule
// service: checkpoint reading (for digest binding) and capsule persistence.
// A single Store implementation typically satisfies both.
type HandoffStore interface {
	handoffapp.CheckpointReader
	handoffapp.CapsuleStore
	handoffapp.TombstoneChecker
}

// SetupHandoffService wires the handoff capsule service into the engine.
// The store must satisfy HandoffStore (checkpoint reader + capsule store +
// tombstone checker). When the store is nil, the method is a no-op
// (ADR-005 §5).
func (e *Engine) SetupHandoffService(store HandoffStore) {
	if store == nil {
		return
	}
	e.SetHandoffService(handoffapp.NewService(store, store).WithTombstoneChecker(store))
}
