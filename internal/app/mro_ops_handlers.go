package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mroapp"
)

// auditMRO best-effort records an ops write onto the workbench audit ledger.
func (e *Engine) auditMRO(ctx context.Context, action, resourceType, resourceID string) {
	if e == nil || e.m8kb == nil {
		return
	}
	_ = e.m8kb.AppendMroAudit(ctx, action, resourceType, resourceID)
}

func mroDuePayload(items []mroapp.DueView) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"id": item.ID, "kind": item.Kind, "scope": item.ScopeID,
			"state": item.Status.State, "label": item.Status.Label,
			"usedMissing": item.UsedMissing, "limitValue": item.LimitValue,
			"remaining": item.Status.Remaining,
		}
		if !item.UsedMissing {
			row["usedValue"] = item.UsedValue
		}
		if item.DueAt != "" {
			row["dueAt"] = item.DueAt
		}
		out = append(out, row)
	}
	return out
}

func handleMRODueList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.due.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListDue(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	return r.Ok(map[string]any{"items": mroDuePayload(items)})
}

func handleMROToolList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.tool.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListToolViews(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"id": item.ID, "toolNo": item.ToolNo, "sn": item.SN, "location": item.Location,
			"holder": item.Holder, "calibDue": item.CalibDue, "status": item.Status,
		}
		if item.CheckoutBlocked != "" {
			row["checkoutBlocked"] = item.CheckoutBlocked
		}
		out = append(out, row)
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROToolCheckout(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ToolID string `json:"toolId"`
		Holder string `json:"holder"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ToolID) == "" || strings.TrimSpace(p.Holder) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.tool.checkout 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.CheckoutTool(ctx, p.ToolID, p.Holder); err != nil {
		return mroFailure(r, err)
	}
	return r.Ok(map[string]any{"ok": true})
}

func handleMROLotTrace(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LotID string `json:"lotId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.lot.trace 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListLotViews(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	want := strings.TrimSpace(p.LotID)
	for _, item := range items {
		if want != "" && item.ID != want && item.LotNo != want {
			continue
		}
		row := map[string]any{"id": item.ID, "lotNo": item.LotNo, "qty": item.Qty, "expires": item.Expires, "tails": item.Tails}
		if item.ParentLotID != "" {
			row["parentLotId"] = item.ParentLotID
		}
		if item.Tails == nil {
			row["tails"] = []string{}
		}
		out = append(out, row)
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROKitStaging(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.kit.staging 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListKitViews(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		missing := item.Missing
		if missing == nil {
			missing = []string{}
		}
		out = append(out, map[string]any{"id": item.ID, "name": item.Name, "missing": missing})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROPartsStockList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Config string `json:"config"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.parts.stock.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	stock, alts, err := e.mro.ListParts(ctx, p.Config)
	if err != nil {
		return mroFailure(r, err)
	}
	items := make([]map[string]any, 0, len(stock))
	for _, row := range stock {
		items = append(items, map[string]any{"pn": row.PN, "qty": row.Qty, "source": row.Source})
	}
	altOut := make([]map[string]any, 0, len(alts))
	for _, row := range alts {
		altOut = append(altOut, map[string]any{
			"pnFrom": row.PNFrom, "pnTo": row.PNTo, "certOk": row.CertOK,
			"effectivity": row.Effectivity, "qty": row.Qty, "accepted": row.Accepted,
		})
	}
	return r.Ok(map[string]any{"items": items, "alternates": altOut})
}

func handleMROPlanList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.plan.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListWorkPackages(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		sources := item.Sources
		if sources == nil {
			sources = []string{}
		}
		out = append(out, map[string]any{"id": item.ID, "title": item.Title, "sources": sources, "hours": item.Hours})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROPlanConstraintCheck(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.plan.constraint.check 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.CheckConstraints(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"code": item.Code, "detail": item.Detail})
	}
	return r.Ok(map[string]any{"violations": out})
}

func handleMROPlanPublish(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackageID string `json:"packageId"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.PackageID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.plan.publish 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	todos, err := e.mro.PublishSchedule(ctx, p.PackageID)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(todos))
	for _, item := range todos {
		out = append(out, map[string]any{"id": item.ID, "kind": item.Kind, "ref": item.Ref, "status": item.Status, "detail": item.Detail})
	}
	return r.Ok(map[string]any{"todos": out})
}

func handleMROOpsTodoList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.ops.todo.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListOpsTodos(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "kind": item.Kind, "ref": item.Ref, "status": item.Status, "detail": item.Detail})
	}
	return r.Ok(map[string]any{"items": out})
}

// --- P1 write handlers ------------------------------------------------------

func handleMROToolUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ToolNo   string `json:"toolNo"`
		SN       string `json:"sn"`
		Location string `json:"location"`
		CalibDue string `json:"calibDue"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ToolNo) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.tool.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertTool(ctx, mroapp.Tool{ToolNo: p.ToolNo, SN: p.SN, Location: p.Location, CalibDue: p.CalibDue}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.tool.upsert", "mro_tool", p.ToolNo)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROToolReturn(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ToolID string `json:"toolId"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ToolID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.tool.return 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.ReturnTool(ctx, p.ToolID); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.tool.return", "mro_tool", p.ToolID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMRODueUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID         string  `json:"id"`
		ScopeID    string  `json:"scopeId"`
		Kind       string  `json:"kind"`
		LimitValue float64 `json:"limitValue"`
		DueAt      string  `json:"dueAt"`
		Source     string  `json:"source"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ScopeID) == "" || strings.TrimSpace(p.Kind) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.due.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	row := mroapp.DueItem{ID: p.ID, ScopeID: p.ScopeID, Kind: p.Kind, LimitValue: p.LimitValue, DueAt: p.DueAt, Source: p.Source, UsedMissing: true}
	if err := e.mro.UpsertDueItem(ctx, row); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.due.upsert", "mro_due", p.ScopeID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROUtilRecord(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeID       string  `json:"scopeId"`
		Hours         float64 `json:"hours"`
		Cycles        float64 `json:"cycles"`
		BatteryCycles float64 `json:"batteryCycles"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ScopeID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.util.record 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	items, err := e.mro.RecordUtilization(ctx, p.ScopeID, p.Hours, p.Cycles, p.BatteryCycles)
	if err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.util.record", "mro_due", p.ScopeID)
	return r.Ok(map[string]any{"items": mroDuePayload(items)})
}

func handleMRODueRecompute(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.due.recompute 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.RecomputeDue(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	return r.Ok(map[string]any{"items": mroDuePayload(items)})
}

func handleMROLotUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LotNo       string  `json:"lotNo"`
		ParentLotID string  `json:"parentLotId"`
		Qty         float64 `json:"qty"`
		Expires     string  `json:"expires"`
		SDSDoc      string  `json:"sdsDoc"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.LotNo) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.lot.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertChemLot(ctx, mroapp.ChemLot{LotNo: p.LotNo, ParentLotID: p.ParentLotID, Qty: p.Qty, Expires: p.Expires, SDSDoc: p.SDSDoc}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.lot.upsert", "mro_chem_lot", p.LotNo)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROLotUse(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LotID  string `json:"lotId"`
		TailNo string `json:"tailNo"`
		WO     string `json:"wo"`
		Tech   string `json:"tech"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.LotID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.lot.use 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.InsertChemUse(ctx, mroapp.ChemUse{LotID: p.LotID, TailNo: p.TailNo, WO: p.WO, Tech: p.Tech}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.lot.use", "mro_chem_use", p.LotID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROKitUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name  string `json:"name"`
		Items []struct {
			PN       string  `json:"pn"`
			Required float64 `json:"required"`
			OnHand   float64 `json:"onHand"`
		} `json:"items"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Name) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.kit.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	items := make([]mroapp.KitItem, 0, len(p.Items))
	for _, it := range p.Items {
		if strings.TrimSpace(it.PN) == "" {
			continue
		}
		items = append(items, mroapp.KitItem{PN: it.PN, Required: it.Required, OnHand: it.OnHand})
	}
	if err := e.mro.UpsertKit(ctx, mroapp.Kit{Name: p.Name}, items); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.kit.upsert", "mro_kit", p.Name)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROPartsStockUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PN     string  `json:"pn"`
		Qty    float64 `json:"qty"`
		Source string  `json:"source"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.PN) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.parts.stock.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertPartsStock(ctx, mroapp.PartsStock{PN: p.PN, Qty: p.Qty, Source: p.Source}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.parts.stock.upsert", "mro_parts_stock", p.PN)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROAlternateUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PNFrom      string `json:"pnFrom"`
		PNTo        string `json:"pnTo"`
		CertOK      bool   `json:"certOk"`
		Effectivity string `json:"effectivity"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.PNFrom) == "" || strings.TrimSpace(p.PNTo) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.alternate.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertAlternate(ctx, mroapp.Alternate{PNFrom: p.PNFrom, PNTo: p.PNTo, CertOK: p.CertOK, Effectivity: p.Effectivity}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.alternate.upsert", "mro_alternate", p.PNFrom)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROWorkpackageBuild(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Title string   `json:"title"`
		Cards []string `json:"cards"`
		ADs   []string `json:"ads"`
		MELs  []string `json:"mels"`
		Open  []string `json:"open"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.workpackage.build 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	pkg, err := e.mro.BuildWorkPackage(ctx, p.Title, p.Cards, p.ADs, p.MELs, p.Open)
	if err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.workpackage.build", "mro_work_package", pkg.ID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROIntervalUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskKey       string  `json:"taskKey"`
		IntervalValue float64 `json:"intervalValue"`
		Unit          string  `json:"unit"`
		SourceCite    string  `json:"sourceCite"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TaskKey) == "" || strings.TrimSpace(p.Unit) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.interval.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertIntervalRule(ctx, mroapp.IntervalRule{TaskKey: p.TaskKey, IntervalValue: p.IntervalValue, Unit: p.Unit, SourceCite: p.SourceCite}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.interval.upsert", "mro_interval_rule", p.TaskKey)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROIntervalPropose(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskKey   string `json:"taskKey"`
		MPDCite   string `json:"mpdCite"`
		FleetCite string `json:"fleetCite"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TaskKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.interval.propose 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.ProposeIntervalChangeDraft(ctx, p.TaskKey, p.MPDCite, p.FleetCite); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.interval.propose", "mro_interval_change", p.TaskKey)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROScheduleUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TailNo    string  `json:"tailNo"`
		CheckName string  `json:"checkName"`
		StartOn   string  `json:"startOn"`
		EndOn     string  `json:"endOn"`
		Hours     float64 `json:"hours"`
		Skill     string  `json:"skill"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TailNo) == "" || strings.TrimSpace(p.CheckName) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.schedule.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertScheduleAssignment(ctx, mroapp.ScheduleAssignment{TailNo: p.TailNo, CheckName: p.CheckName, Start: p.StartOn, End: p.EndOn, Hours: p.Hours, Skill: p.Skill}); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.schedule.upsert", "mro_schedule", p.TailNo)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROCapacityUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Skill string  `json:"skill"`
		Hours float64 `json:"hours"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Skill) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.capacity.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.UpsertCapacitySlot(ctx, p.Skill, p.Hours); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.capacity.upsert", "mro_capacity", p.Skill)
	return r.Ok(map[string]any{"ok": true})
}

// --- P2 low-altitude / AOG / PO handlers -----------------------------------

func handleMROComponentUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SN   string  `json:"sn"`
		PN   string  `json:"pn"`
		Life float64 `json:"life"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.SN) == "" || strings.TrimSpace(p.PN) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.component.upsert 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if _, err := e.mro.UpsertComponent(ctx, p.SN, p.PN, p.Life); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.component.upsert", "mro_component", p.SN)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROComponentList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.component.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	views, err := e.mro.ListGenealogies(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(views))
	for _, v := range views {
		events := make([]map[string]any, 0, len(v.Events))
		for _, ev := range v.Events {
			events = append(events, map[string]any{"kind": ev.Kind, "occurredAt": ev.OccurredAt, "note": ev.Note})
		}
		out = append(out, map[string]any{"id": v.ID, "sn": v.SN, "pn": v.PN, "lifeCount": v.LifeCount, "installed": v.Installed, "tailNo": v.TailNo, "events": events})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROLifeEvent(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ComponentID string `json:"componentId"`
		Kind        string `json:"kind"`
		OccurredAt  string `json:"occurredAt"`
		Note        string `json:"note"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ComponentID) == "" || strings.TrimSpace(p.Kind) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.life.event 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.RecordLifeEvent(ctx, p.ComponentID, p.Kind, p.OccurredAt, p.Note); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.life.event", "mro_component", p.ComponentID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROPirepDraft(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TailNo string `json:"tailNo"`
		Body   string `json:"body"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.TailNo) == "" || strings.TrimSpace(p.Body) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.pirep.draft 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	body, _ := json.Marshal(map[string]string{"note": p.Body})
	if _, err := e.mro.DraftPirep(ctx, p.TailNo, string(body)); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.pirep.draft", "mro_pirep", p.TailNo)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROPirepList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.pirep.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListPireps(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "tailNo": item.TailNo, "body": item.BodyJSON, "state": item.State, "createdAt": item.CreatedAt})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROAogIntake(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Text string `json:"text"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.Text) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.aog.intake 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	row, err := e.mro.IntakeAOG(ctx, p.Text)
	if err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.aog.intake", "mro_aog", row.TailNo)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROAogList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.aog.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListAOG(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "tailNo": item.TailNo, "pn": item.PN, "qty": item.Qty, "note": item.Note, "state": item.State})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROPoDraft(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PN    string `json:"pn"`
		Qty   string `json:"qty"`
		Price string `json:"price"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.PN) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.po.draft 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if _, err := e.mro.DraftPO(ctx, p.PN, p.Qty, p.Price); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.po.draft", "mro_po", p.PN)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROPoList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.po.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListPO(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "pn": item.PN, "qty": item.Qty, "price": item.Price, "state": item.State})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROTriggerList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.trigger.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.Triggers(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"scopeId": item.ScopeID, "kind": item.Kind, "state": item.State, "action": item.Action, "category": item.Category})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROIntervalList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if decodePayload(r.Payload, &struct{}{}) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.interval.list 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	items, err := e.mro.ListIntervalRules(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"taskKey": item.TaskKey, "intervalValue": item.IntervalValue, "unit": item.Unit, "version": item.Version, "sourceCite": item.SourceCite})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleMROChemIssue(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LotID  string  `json:"lotId"`
		Qty    float64 `json:"qty"`
		TailNo string  `json:"tailNo"`
		WO     string  `json:"wo"`
		Tech   string  `json:"tech"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.LotID) == "" || p.Qty <= 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.chem.issue 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if _, err := e.mro.IssueChemical(ctx, p.LotID, p.Qty, p.TailNo, p.WO, p.Tech); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.chem.issue", "mro_chem_lot", p.LotID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROOpsTodoAdd(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		KitID  string `json:"kitId"`
		Detail string `json:"detail"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.KitID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.ops.todo.add 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	row, err := e.mro.AddPartsTodo(ctx, p.KitID, p.Detail)
	if err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.ops.todo.add", "mro_ops_todo", row.Ref)
	return r.Ok(map[string]any{"ok": true, "id": row.ID})
}

func handleMROPirepConfirm(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.pirep.confirm 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.ConfirmPirep(ctx, p.ID, p.State); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.pirep.confirm", "mro_pirep", p.ID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROAogConfirm(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.aog.confirm 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.ConfirmAOG(ctx, p.ID, p.State); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.aog.confirm", "mro_aog", p.ID)
	return r.Ok(map[string]any{"ok": true})
}

func handleMROPoConfirm(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "mro.po.confirm 参数无效", false)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if err := e.mro.ConfirmPO(ctx, p.ID, p.State); err != nil {
		return mroFailure(r, err)
	}
	e.auditMRO(ctx, "mro.po.confirm", "mro_po", p.ID)
	return r.Ok(map[string]any{"ok": true})
}
