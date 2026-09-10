package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/connectorapp"
	"github.com/lunitide/lunitide/internal/maintenance"
	"github.com/lunitide/lunitide/internal/widgetapp"
)

func handleBackupProbe(_ *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Directory string `json:"directory"`
	}
	if decodePayload(request.Payload, &p) != nil || strings.TrimSpace(p.Directory) == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "backup.probe 参数无效", false)
	}
	got, err := maintenance.Probe(p.Directory)
	if err != nil {
		return request.Ok(map[string]any{"ok": false, "format": got.Format, "files": got.Files, "reason": err.Error()})
	}
	return request.Ok(map[string]any{"ok": got.OK, "format": got.Format, "files": got.Files})
}

func handleConnectorRecipeList(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	if len(request.Payload) > 0 && string(request.Payload) != "{}" && string(request.Payload) != "null" {
		var empty map[string]any
		if decodePayload(request.Payload, &empty) != nil || len(empty) > 0 {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "connector.recipe.list 参数无效", false)
		}
	}
	var store *connectorapp.FileStore
	if e != nil {
		store = e.connectors
	}
	items := make([]map[string]any, 0)
	for _, r := range store.ListOrCatalog() {
		item := map[string]any{"id": r.ID, "scope": r.Scope, "revision": r.Revision, "status": r.Status, "paused": r.Paused || r.Status != "ready", "credentialBound": r.CredentialRef != ""}
		if r.RateLimit != "" {
			item["rateLimit"] = r.RateLimit
		}
		if r.Health != "" {
			item["health"] = r.Health
		}
		items = append(items, item)
	}
	return request.Ok(map[string]any{"items": items})
}

func handleConnectorRecipeRevoke(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(request.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "connector.recipe.revoke 参数无效", false)
	}
	if e == nil || e.connectors == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "连接配方未装配", true)
	}
	got, err := e.RevokeConnectorCredential(ctx, p.ID, "")
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "连接配方撤销失败", true)
	}
	return request.Ok(map[string]any{"id": got.ID, "status": got.Status, "paused": got.Paused || got.Status != "ready"})
}

func specFromPayload(p struct {
	Owner   string             `json:"owner"`
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Layout  string             `json:"layout"`
	Widgets []widgetapp.Widget `json:"widgets"`
	Actions []widgetapp.Action `json:"actions"`
}) widgetapp.Spec {
	return widgetapp.Spec{ID: p.ID, Owner: p.Owner, Title: p.Title, Layout: p.Layout, Widgets: p.Widgets, Actions: p.Actions}
}

func widgetStateDTO(st widgetapp.WidgetState) map[string]any {
	if st == (widgetapp.WidgetState{}) {
		return nil
	}
	out := map[string]any{}
	if st.Seconds != 0 || st.Running || st.UpdatedAt != "" {
		out["seconds"] = st.Seconds
	}
	if st.Running {
		out["running"] = true
	}
	if st.UpdatedAt != "" {
		out["updatedAt"] = st.UpdatedAt
	}
	if st.Value != "" {
		out["value"] = st.Value
	}
	if st.Unit != "" {
		out["unit"] = st.Unit
	}
	if st.Percent != 0 {
		out["percent"] = st.Percent
	}
	if st.Rows != "" {
		out["rows"] = st.Rows
	}
	if st.Checked != "" {
		out["checked"] = st.Checked
	}
	if st.Items != "" {
		out["items"] = st.Items
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func widgetDTO(spec widgetapp.Spec) map[string]any {
	return map[string]any{"id": spec.ID, "revision": spec.Revision, "owner": spec.Owner, "title": spec.Title, "status": spec.Status}
}

func widgetQueryDTO(spec widgetapp.Spec) map[string]any {
	out := widgetDTO(spec)
	if len(spec.Widgets) == 0 {
		return out
	}
	widgets := make([]map[string]any, 0, len(spec.Widgets))
	for _, w := range spec.Widgets {
		item := map[string]any{"id": w.ID, "kind": w.Kind}
		if st := widgetStateDTO(w.State); st != nil {
			item["state"] = st
		}
		widgets = append(widgets, item)
	}
	out["widgets"] = widgets
	return out
}

func handleWidgetCreate(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Owner   string             `json:"owner"`
		ID      string             `json:"id"`
		Title   string             `json:"title"`
		Layout  string             `json:"layout"`
		Widgets []widgetapp.Widget `json:"widgets"`
		Actions []widgetapp.Action `json:"actions"`
	}
	if decodePayload(request.Payload, &p) != nil || p.Owner == "" || p.ID == "" || p.Title == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "widget.create 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 存储未装配", true)
	}
	got, err := e.widgets.Create(specFromPayload(p))
	if errors.Is(err, widgetapp.ErrInvalidSpec) || errors.Is(err, widgetapp.ErrForbidden) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 写入失败", true)
	}
	return request.Ok(widgetDTO(got))
}

func handleWidgetUpdate(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Owner    string             `json:"owner"`
		ID       string             `json:"id"`
		Revision int                `json:"revision"`
		Title    string             `json:"title"`
		Layout   string             `json:"layout"`
		Widgets  []widgetapp.Widget `json:"widgets"`
		Actions  []widgetapp.Action `json:"actions"`
	}
	if decodePayload(request.Payload, &p) != nil || p.Owner == "" || p.ID == "" || p.Revision < 1 || p.Title == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "widget.update 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 存储未装配", true)
	}
	spec := specFromPayload(struct {
		Owner   string             `json:"owner"`
		ID      string             `json:"id"`
		Title   string             `json:"title"`
		Layout  string             `json:"layout"`
		Widgets []widgetapp.Widget `json:"widgets"`
		Actions []widgetapp.Action `json:"actions"`
	}{p.Owner, p.ID, p.Title, p.Layout, p.Widgets, p.Actions})
	spec.Revision = p.Revision
	got, err := e.widgets.Update(p.Owner, spec)
	if errors.Is(err, widgetapp.ErrInvalidSpec) || errors.Is(err, widgetapp.ErrForbidden) || errors.Is(err, widgetapp.ErrConflict) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 写入失败", true)
	}
	return request.Ok(widgetDTO(got))
}

func handleWidgetQuery(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Owner string `json:"owner"`
		ID    string `json:"id"`
	}
	if decodePayload(request.Payload, &p) != nil || p.Owner == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "widget.query 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Ok(map[string]any{"items": []any{}})
	}
	listed, err := e.widgets.Query(p.Owner, p.ID)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 读取失败", true)
	}
	items := make([]map[string]any, 0, len(listed))
	for _, spec := range listed {
		items = append(items, widgetQueryDTO(spec))
	}
	return request.Ok(map[string]any{"items": items})
}

func handleWidgetArchive(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Owner    string `json:"owner"`
		ID       string `json:"id"`
		Revision int    `json:"revision"`
	}
	if decodePayload(request.Payload, &p) != nil || p.Owner == "" || p.ID == "" || p.Revision < 1 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "widget.archive 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "widget 存储未装配", true)
	}
	got, err := e.widgets.Archive(p.Owner, p.ID, p.Revision)
	if err != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	return request.Ok(widgetDTO(got))
}

func itemDTO(item widgetapp.Item) map[string]any {
	out := map[string]any{"id": item.ID, "type": item.Type, "title": item.Title, "status": item.Status}
	if item.DueAt != "" {
		out["dueAt"] = item.DueAt
	}
	if item.Timezone != "" {
		out["timezone"] = item.Timezone
	}
	if item.RepeatRule != "" {
		out["repeatRule"] = item.RepeatRule
	}
	if item.Amount != "" {
		out["amount"] = item.Amount
	}
	if item.Currency != "" {
		out["currency"] = item.Currency
	}
	if item.SourceRef != "" {
		out["sourceRef"] = item.SourceRef
	}
	if item.UpdatedAt != "" {
		out["updatedAt"] = item.UpdatedAt
	}
	return out
}

func handleItemList(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	if len(request.Payload) > 0 && string(request.Payload) != "{}" && string(request.Payload) != "null" {
		var empty map[string]any
		if decodePayload(request.Payload, &empty) != nil || len(empty) > 0 {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "item.list 参数无效", false)
		}
	}
	if e == nil || e.widgets == nil {
		return request.Ok(map[string]any{"items": []any{}})
	}
	listed := e.widgets.ListItems()
	items := make([]map[string]any, 0, len(listed))
	for _, item := range listed {
		items = append(items, itemDTO(item))
	}
	return request.Ok(map[string]any{"items": items})
}

func handleItemUpsert(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p widgetapp.Item
	if decodePayload(request.Payload, &p) != nil || p.ID == "" || p.Type == "" || p.Title == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "item.upsert 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "事项存储未装配", true)
	}
	got, err := e.widgets.UpsertItem(p)
	if err != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	return request.Ok(itemDTO(got))
}

func handleItemArchive(e *Engine, _ context.Context, request bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(request.Payload, &p) != nil || p.ID == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "item.archive 参数无效", false)
	}
	if e == nil || e.widgets == nil {
		return request.Fail("STORAGE_UNAVAILABLE", "事项存储未装配", true)
	}
	got, err := e.widgets.ArchiveItem(p.ID)
	if err != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	return request.Ok(itemDTO(got))
}
