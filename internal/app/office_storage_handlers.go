package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

func handleOfficeStorage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.officeStudio == nil {
		return r.Fail("FEATURE_DISABLED", "办公工作台尚未初始化", false)
	}
	orgID, _, err := e.boundOrgState(ctx)
	if err != nil {
		return officeFailure(r, err)
	}
	ctx = domain.WithScope(ctx, orgID)
	if r.Method == "office.storage.usage" {
		usage, err := e.officeStudio.StorageUsage(ctx)
		if err != nil {
			return officeFailure(r, err)
		}
		return r.Ok(usage)
	}
	var p domain.StorageSweepOptions
	if decodePayload(r.Payload, &p) != nil || p.Limit < 0 || p.Limit > 200 {
		return officeFailure(r, domain.ErrInvalid)
	}
	if p.Limit == 0 {
		p.Limit = 100
	}
	if !p.DryRun {
		if failure := requireIdempotency(r); failure != nil {
			return *failure
		}
		p.RequestKey = r.IdempotencyKey
	}
	// Only the service's expired, unreferenced managed blobs can be removed.
	// Existing files remain readable even when creation has been disabled.
	report, err := e.officeStudio.SweepStorage(ctx, p)
	if err != nil {
		return officeFailure(r, err)
	}
	return r.Ok(report)
}
