package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

// MigrationService provides Electron/prototype data migration operations.
type MigrationService interface {
	// InspectDiscovery checks discovered Electron sources and returns aggregate status.
	InspectDiscovery(ctx context.Context) (MigrationInspectResult, error)
	// RunDiscovery executes discovered Electron migrations.
	RunDiscovery(ctx context.Context, dryRun bool) (MigrationStatus, error)
	// StatusDiscovery returns the current migration status across all discovered sources.
	StatusDiscovery(ctx context.Context) (MigrationStatus, error)
}

// MigrationInspectResult is the aggregate inspection result.
type MigrationInspectResult struct {
	Required      bool   `json:"required"`
	Items         int    `json:"items"`
	SourceVersion int    `json:"sourceVersion"`
	TargetVersion int    `json:"targetVersion"`
}

// MigrationStatus is the aggregate migration status.
type MigrationStatus struct {
	State    string `json:"state"`
	Processed int   `json:"processed"`
	Total    int    `json:"total"`
}

func handleMigrationInspect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.migration == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移服务暂时不可用", true)
	}
	result, err := e.migration.InspectDiscovery(ctx)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移检查失败", true)
	}
	return bridge.Success(r.ID, result)
}

func handleMigrationRun(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DryRun bool `json:"dryRun"`
	}
	_ = decodePayload(r.Payload, &p)
	if e.migration == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移服务暂时不可用", true)
	}
	if p.DryRun {
		result, err := e.migration.InspectDiscovery(ctx)
		if err != nil {
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移检查失败", true)
		}
		return bridge.Success(r.ID, MigrationStatus{State: "idle", Processed: 0, Total: result.Items})
	}
	status, err := e.migration.RunDiscovery(ctx, false)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移执行失败", true)
	}
	return bridge.Success(r.ID, status)
}

func handleMigrationStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.migration == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移服务暂时不可用", true)
	}
	status, err := e.migration.StatusDiscovery(ctx)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "迁移状态查询失败", true)
	}
	return bridge.Success(r.ID, status)
}
