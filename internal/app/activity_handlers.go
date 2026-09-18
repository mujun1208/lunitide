package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/media"
	"github.com/lunitide/lunitide/internal/modelfit"
)

func handleActivityList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, scopeID, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "activity.list 参数无效", false)
	}
	var p struct {
		ScopeKind string   `json:"scopeKind"`
		ScopeID   string   `json:"scopeId"`
		Domains   []string `json:"domains"`
		Cursor    string   `json:"cursor"`
		Limit     int      `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "activity.list 参数无效", false)
	}
	want := map[string]bool{"tool": true, "ocr": true, "media": true}
	if len(p.Domains) > 0 {
		want = map[string]bool{}
		for _, domain := range p.Domains {
			want[domain] = true
		}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	fetch := limit + 1
	if fetch < 51 {
		fetch = 51
	}
	items := make([]map[string]any, 0)
	owner := e.memorySubjectID()
	if want["media"] && e != nil && e.media != nil {
		ops, err := e.media.ListOperations(ctx, owner, kind, scopeID, fetch)
		if err == nil {
			for _, op := range ops {
				items = append(items, activityFromMedia(op, kind, scopeID))
			}
		}
	}
	if want["ocr"] {
		if store := e.ocrSQLite(); store != nil {
			ops, err := store.ListOCRPackOperationsForSubject(ctx, owner, fetch)
			if err == nil {
				for _, op := range ops {
					items = append(items, activityFromOCR(op.OperationID, op.PackID, op.Phase, op.ErrorCode, op.CreatedAt, op.UpdatedAt, kind, scopeID))
				}
			}
		}
	}
	if want["tool"] && e != nil && e.toolOps != nil {
		ops, err := e.toolOps.ListToolOperations(ctx, owner, fetch)
		if err == nil {
			for _, op := range ops {
				items = append(items, activityFromTool(op, kind, scopeID))
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		ui, uj := fmt.Sprint(items[i]["updatedAt"]), fmt.Sprint(items[j]["updatedAt"])
		if ui != uj {
			return ui > uj
		}
		return fmt.Sprint(items[i]["activityId"]) > fmt.Sprint(items[j]["activityId"])
	})
	start := 0
	if p.Cursor != "" {
		for i, item := range items {
			if activityCursor(item) == p.Cursor {
				start = i + 1
				break
			}
		}
	}
	page := items[start:]
	var next any
	hasMore := false
	if len(page) > limit {
		page = page[:limit]
		hasMore = true
		next = activityCursor(page[len(page)-1])
	}
	return r.Ok(map[string]any{
		"items":      page,
		"nextCursor": next,
		"snapshotAt": time.Now().UTC().Format(time.RFC3339),
		"hasMore":    hasMore,
	})
}

func activityCursor(item map[string]any) string {
	return fmt.Sprint(item["updatedAt"]) + "|" + fmt.Sprint(item["activityId"])
}

func activityTerminal(phase string) bool {
	return phase == "succeeded" || phase == "uncertain" || phase == "failed" || phase == "cancelled"
}

func activityDTO(id, domain, kind, phase, title, verifyStatus, verifySource, recovery, scopeKind, scopeID, createdAt, updatedAt, rootID, errorCode string, retryable bool) map[string]any {
	if phase == "" {
		phase = "uncertain"
	}
	created := any(nil)
	if createdAt != "" {
		created = createdAt
	}
	updated := any(nil)
	if updatedAt != "" {
		updated = updatedAt
	}
	root := any(nil)
	if len(rootID) == 26 {
		root = rootID
	}
	return map[string]any{
		"activityId":         id,
		"domain":             domain,
		"kind":               kind,
		"phase":              phase,
		"terminal":           activityTerminal(phase),
		"verificationStatus": verifyStatus,
		"verificationSource": verifySource,
		"title":              title,
		"completedUnits":     nil,
		"totalUnits":         nil,
		"errorCode":          nullIfBlank(errorCode),
		"retryable":          retryable,
		"recoveryAction":     recovery,
		"scopeKind":          scopeKind,
		"scopeId":            publicScopeID(scopeKind, scopeID),
		"createdAt":          created,
		"updatedAt":          updated,
		"rootOperationId":    root,
	}
}

func activityFromMedia(op media.Operation, scopeKind, scopeID string) map[string]any {
	phase := string(op.Phase)
	switch phase {
	case "requested":
		phase = "queued"
	case "awaiting_approval":
		phase = "awaiting_approval"
	case "dispatching":
		phase = "running"
	case "verifying":
		phase = "verifying"
	case "succeeded", "uncertain", "failed", "cancelled":
	default:
		phase = "uncertain"
	}
	recovery := "none"
	if phase == "failed" || phase == "uncertain" {
		recovery = "open_player"
	}
	title := op.Action
	if title == "" {
		title = "media"
	}
	return activityDTO(op.OperationID, "media", op.Action, phase, title, op.VerificationStatus, op.VerificationSource, recovery, scopeKind, scopeID, op.CreatedAt, op.UpdatedAt, op.RootOperationID, op.ErrorCode, phase == "failed")
}

func activityFromOCR(id, packID, packPhase, errorCode, createdAt, updatedAt, scopeKind, scopeID string) map[string]any {
	phase := "uncertain"
	switch packPhase {
	case "requested":
		phase = "queued"
	case "preflighting", "downloading", "installing":
		phase = "running"
	case "verifying", "self_testing":
		phase = "verifying"
	case "succeeded", "failed", "cancelled":
		phase = packPhase
	}
	title := packID
	if title == "" {
		title = "ocr"
	}
	recovery := "none"
	if phase == "failed" || phase == "uncertain" {
		recovery = "open_settings"
	}
	verify := "not_applicable"
	if phase == "uncertain" {
		verify = "unconfirmed"
	}
	return activityDTO(id, "ocr", "pack", phase, title, verify, "none", recovery, scopeKind, scopeID, createdAt, updatedAt, id, errorCode, false)
}

func activityFromTool(op modelfit.ToolOperation, scopeKind, scopeID string) map[string]any {
	phase := "uncertain"
	switch op.State {
	case modelfit.OpPending:
		phase = "queued"
	case modelfit.OpRunning:
		phase = "running"
	case modelfit.OpSucceeded:
		phase = "succeeded"
	case modelfit.OpFailed:
		phase = "failed"
	case modelfit.OpCancelled:
		phase = "cancelled"
	case modelfit.OpUnknown, modelfit.OpPartial:
		phase = "uncertain"
	}
	title := op.ToolName
	if title == "" {
		title = "tool"
	}
	recovery := "none"
	if phase == "failed" || phase == "uncertain" {
		recovery = "retry"
	}
	verify := "not_applicable"
	if phase == "uncertain" {
		verify = "unconfirmed"
	}
	created := ""
	if !op.CreatedAt.IsZero() {
		created = op.CreatedAt.UTC().Format(time.RFC3339)
	}
	updated := ""
	if !op.UpdatedAt.IsZero() {
		updated = op.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return activityDTO(op.ID, "tool", op.ToolName, phase, title, verify, "none", recovery, scopeKind, scopeID, created, updated, op.ID, op.ErrorKind, phase == "failed")
}
