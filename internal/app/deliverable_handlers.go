package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/projectapp"
)

type DeliverableStore interface {
	ListProjectDeliverables(context.Context, deliverable.Filter) ([]deliverable.ProjectDeliverable, error)
	UpsertProjectDeliverable(context.Context, deliverable.ProjectDeliverable) (deliverable.ProjectDeliverable, error)
	ConfirmDeliverableGate(context.Context, string, string, int64) (deliverable.ProjectDeliverable, error)
}

func deliverableStoreAvailable(store DeliverableStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func rejectIfProjectReadOnly(e *Engine, ctx context.Context, r bridge.Request, projectID string) *bridge.Response {
	if !projectServiceAvailable(e.projects) {
		resp := bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
		return &resp
	}
	proj, err := e.projects.Get(ctx, projectID)
	if err != nil {
		resp := projectFailure(r, err)
		return &resp
	}
	if !proj.CanEditMutableFields() {
		resp := projectFailure(r, projectapp.ErrInvalidTransition)
		return &resp
	}
	return nil
}

type deliverableDTO struct {
	ID                string             `json:"id"`
	ProjectID         string             `json:"projectId"`
	Phase             int                `json:"phase"`
	DocumentType      string             `json:"documentType"`
	Title             string             `json:"title"`
	TemplateID        string             `json:"templateId,omitempty"`
	AttachmentID      string             `json:"attachmentId,omitempty"`
	Status            deliverable.Status `json:"status"`
	GateConfirmations int                `json:"gateConfirmations"`
	Digest            string             `json:"digest"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	Version           int64              `json:"version"`
}

func newDeliverableDTO(d deliverable.ProjectDeliverable) deliverableDTO {
	return deliverableDTO{
		ID: d.ID, ProjectID: d.ProjectID, Phase: d.Phase, DocumentType: d.DocumentType,
		Title: d.Title, TemplateID: d.TemplateID, AttachmentID: d.AttachmentID,
		Status: d.Status, GateConfirmations: d.GateConfirmations, Digest: d.Digest,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Version: d.Version,
	}
}

func handleDeliverableList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Phase     int    `json:"phase"`
		Status    string `json:"status"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.list 参数无效", false)
	}
	if p.Phase != 0 && !deliverable.ValidPhase(p.Phase) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.list 参数无效", false)
	}
	if p.Status != "" && !deliverable.ValidStatus(deliverable.Status(p.Status)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.list 参数无效", false)
	}
	if !deliverableStoreAvailable(e.deliverables) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "交付物数据暂时不可用", true)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{
		ProjectID: p.ProjectID, Phase: p.Phase, Status: deliverable.Status(p.Status),
	})
	if err != nil {
		return deliverableFailure(r, err)
	}
	dtos := make([]deliverableDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, newDeliverableDTO(items[i]))
	}
	return bridge.Success(r.ID, map[string]any{"items": dtos})
}

func handleDeliverableUpsert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID    string `json:"projectId"`
		Phase        int    `json:"phase"`
		DocumentType string `json:"documentType"`
		Title        string `json:"title"`
		TemplateID   string `json:"templateId"`
		AttachmentID string `json:"attachmentId"`
		Status       string `json:"status"`
		Digest       string `json:"digest"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || !deliverable.ValidPhase(p.Phase) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
	}
	if !asset.ValidDocumentType(asset.DocumentType(p.DocumentType)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
	}
	title, err := deliverable.NormalizeTitle(p.Title)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
	}
	if p.TemplateID != "" && !validCanonicalULID(p.TemplateID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
	}
	if p.AttachmentID != "" && !validCanonicalULID(p.AttachmentID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
	}
	status := deliverable.StatusDraft
	if p.Status != "" {
		if !deliverable.ValidStatus(deliverable.Status(p.Status)) {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.upsert 参数无效", false)
		}
		status = deliverable.Status(p.Status)
	}
	if !deliverableStoreAvailable(e.deliverables) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "交付物数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	saved, err := e.deliverables.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{
		ProjectID: p.ProjectID, Phase: p.Phase, DocumentType: p.DocumentType, Title: title,
		TemplateID: p.TemplateID, AttachmentID: p.AttachmentID, Status: status,
		Digest: clampText(p.Digest, 128),
	})
	if err != nil {
		return deliverableFailure(r, err)
	}
	return bridge.Success(r.ID, newDeliverableDTO(saved))
}

func handleDeliverableConfirmGate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID       string `json:"projectId"`
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "deliverable.confirmGate 参数无效", false)
	}
	if !deliverableStoreAvailable(e.deliverables) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "交付物数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	updated, err := e.deliverables.ConfirmDeliverableGate(ctx, p.ProjectID, p.ID, p.ExpectedVersion)
	if err != nil {
		return deliverableFailure(r, err)
	}
	return bridge.Success(r.ID, newDeliverableDTO(updated))
}

func deliverableFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, deliverable.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "DELIVERABLE_NOT_FOUND", "交付物不存在", false)
	case errors.Is(err, deliverable.ErrGateLocked):
		return bridge.Failure(r.ID, r.TraceID, "DELIVERABLE_GATE_LOCKED", "交付物门禁已锁定", false)
	case errors.Is(err, deliverable.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "DELIVERABLE_VERSION_CONFLICT", "交付物已被其他操作修改，请刷新后重试", false)
	default:
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "交付物数据暂时不可用"
		}
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", msg, true)
	}
}
