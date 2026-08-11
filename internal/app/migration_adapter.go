package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// migrationAdapter bridges sqlite.Store to the MigrationService interface.
type migrationAdapter struct {
	store *sqlite.Store
}

// NewMigrationAdapter creates a MigrationService backed by the given store.
func NewMigrationAdapter(store *sqlite.Store) MigrationService {
	if store == nil {
		return nil
	}
	return &migrationAdapter{store: store}
}

func (m *migrationAdapter) InspectDiscovery(ctx context.Context) (MigrationInspectResult, error) {
	sources, err := sqlite.DiscoverElectronSources()
	if err != nil {
		return MigrationInspectResult{TargetVersion: 1}, nil
	}
	totalItems := 0
	sourceVersion := 0
	required := false
	for _, src := range sources {
		totalItems += src.Providers
		if src.Version != "" && sourceVersion == 0 {
			sourceVersion = 1
		}
		required = true
	}
	return MigrationInspectResult{
		Required:      required,
		Items:         totalItems,
		SourceVersion: sourceVersion,
		TargetVersion: 1,
	}, nil
}

func (m *migrationAdapter) RunDiscovery(ctx context.Context, dryRun bool) (MigrationStatus, error) {
	if dryRun {
		inspect, err := m.InspectDiscovery(ctx)
		if err != nil {
			return MigrationStatus{}, err
		}
		return MigrationStatus{State: "idle", Total: inspect.Items}, nil
	}
	statuses, err := m.store.RunDiscoveredElectronProviderMetadata(ctx)
	if err != nil {
		return MigrationStatus{State: "failed"}, nil
	}
	totalProcessed := 0
	totalItems := 0
	state := "completed"
	for _, st := range statuses {
		totalProcessed += st.Processed
		totalItems += st.Total
		if st.State == "running" || st.State == "idle" {
			state = "running"
		}
		if st.State == "failed" {
			state = "failed"
		}
	}
	return MigrationStatus{State: state, Processed: totalProcessed, Total: totalItems}, nil
}

func (m *migrationAdapter) StatusDiscovery(ctx context.Context) (MigrationStatus, error) {
	sources, err := sqlite.DiscoverElectronSources()
	if err != nil || len(sources) == 0 {
		return MigrationStatus{State: "idle"}, nil
	}
	return m.RunDiscovery(ctx, true)
}
