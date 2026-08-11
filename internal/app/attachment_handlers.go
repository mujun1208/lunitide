package app

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/attachment"
)

// attachmentDTO is the JSON-serializable view of an attachment.
// The parsed_text field is only included in attachment.get responses;
// list responses expose parsed_text_bytes for budget estimation without
// transferring the full text.
type attachmentDTO struct {
	AttachmentID    string    `json:"attachmentId"`
	ProjectID       string    `json:"projectId"`
	SessionID       string    `json:"sessionId,omitempty"`
	OriginalName    string    `json:"originalName"`
	MIME            string    `json:"mime"`
	Size            int64     `json:"size"`
	SHA256          string    `json:"sha256"`
	ParseStatus     string    `json:"parseStatus"`
	ParseErrorCode  string    `json:"parseErrorCode"`
	ParsedTextBytes int64     `json:"parsedTextBytes"`
	CreatedAt       time.Time `json:"createdAt"`
}

func newAttachmentDTO(a attachment.Attachment) attachmentDTO {
	return attachmentDTO{
		AttachmentID:    a.ID,
		ProjectID:       a.ProjectID,
		SessionID:       a.SessionID,
		OriginalName:    a.OriginalName,
		MIME:            a.MIME,
		Size:            a.Size,
		SHA256:          a.SHA256,
		ParseStatus:     string(a.ParseStatus),
		ParseErrorCode:  a.ParseErrorCode,
		ParsedTextBytes: a.ParsedTextBytes,
		CreatedAt:       a.CreatedAt,
	}
}

// handleAttachmentIngest ingests a user-supplied file: validates content,
// writes to the controlled data directory, creates the metadata record, and
// parses text (ADR-005 §7: attachment isolation). The file content is sent
// as base64 within the bridge payload.
func handleAttachmentIngest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID     string `json:"projectId"`
		SessionID     string `json:"sessionId"`
		OriginalName  string `json:"originalName"`
		MIME          string `json:"mime"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		!validCanonicalULID(p.ProjectID) ||
		strings.TrimSpace(p.OriginalName) == "" ||
		strings.TrimSpace(p.MIME) == "" ||
		p.ContentBase64 == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.ingest 参数无效", false)
	}
	if p.SessionID != "" && !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.ingest sessionId 无效", false)
	}
	content, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.ingest contentBase64 解码失败", false)
	}
	att, err := e.IngestAttachment(ctx, attachmentapp.IngestFileRequest{
		ProjectID:    p.ProjectID,
		SessionID:    p.SessionID,
		OriginalName: p.OriginalName,
		MIME:         p.MIME,
		Content:      content,
	})
	if err != nil {
		return attachmentFailure(r, err)
	}
	dto := newAttachmentDTO(att)
	return bridge.Success(r.ID, map[string]any{
		"attachmentId":    dto.AttachmentID,
		"projectId":       dto.ProjectID,
		"sessionId":       dto.SessionID,
		"originalName":    dto.OriginalName,
		"mime":            dto.MIME,
		"size":            dto.Size,
		"sha256":          dto.SHA256,
		"parseStatus":     dto.ParseStatus,
		"parseErrorCode":  dto.ParseErrorCode,
		"parsedTextBytes": dto.ParsedTextBytes,
		"createdAt":       dto.CreatedAt,
	})
}

// handleAttachmentGet returns an attachment by ID, including its parsed text
// for display (ADR-005 §7).
func handleAttachmentGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AttachmentID string `json:"attachmentId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AttachmentID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.get 参数无效", false)
	}
	att, err := e.GetAttachment(ctx, p.AttachmentID)
	if err != nil {
		return attachmentFailure(r, err)
	}
	dto := newAttachmentDTO(*att)
	return bridge.Success(r.ID, map[string]any{
		"attachmentId":    dto.AttachmentID,
		"projectId":       dto.ProjectID,
		"sessionId":       dto.SessionID,
		"originalName":    dto.OriginalName,
		"mime":            dto.MIME,
		"size":            dto.Size,
		"sha256":          dto.SHA256,
		"parseStatus":     dto.ParseStatus,
		"parseErrorCode":  dto.ParseErrorCode,
		"parsedText":      att.ParsedText,
		"parsedTextBytes": dto.ParsedTextBytes,
		"createdAt":       dto.CreatedAt,
	})
}

// handleAttachmentList returns attachments for a project, ordered by creation
// time descending (ADR-005 §7). Soft-deleted attachments are excluded.
func handleAttachmentList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.list 参数无效", false)
	}
	atts, err := e.ListAttachmentsByProject(ctx, p.ProjectID, p.Limit)
	if err != nil {
		return attachmentFailure(r, err)
	}
	items := make([]attachmentDTO, 0, len(atts))
	for _, a := range atts {
		items = append(items, newAttachmentDTO(a))
	}
	return bridge.Success(r.ID, map[string]any{"items": items})
}

// handleAttachmentDelete soft-deletes an attachment and removes the underlying
// file from the data directory (ADR-005 §7). Idempotent.
func handleAttachmentDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AttachmentID string `json:"attachmentId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AttachmentID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "attachment.delete 参数无效", false)
	}
	if err := e.DeleteAttachment(ctx, p.AttachmentID); err != nil {
		return attachmentFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{
		"attachmentId": p.AttachmentID,
		"deleted":      true,
	})
}

// attachmentFailure maps attachment service errors to stable Bridge error codes.
func attachmentFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, attachmentapp.ErrAttachmentNotFound):
		return bridge.Failure(r.ID, r.TraceID, "ATTACHMENT_NOT_FOUND", "附件不存在", false)
	case errors.Is(err, attachmentapp.ErrFileTooLarge):
		return bridge.Failure(r.ID, r.TraceID, "ATTACHMENT_FILE_TOO_LARGE", "附件文件超过 10 MiB 限制", false)
	case errors.Is(err, attachmentapp.ErrUnsupportedMIME):
		return bridge.Failure(r.ID, r.TraceID, "ATTACHMENT_UNSUPPORTED_MIME", "不支持的附件 MIME 类型", false)
	case errors.Is(err, attachmentapp.ErrInvalidContent):
		return bridge.Failure(r.ID, r.TraceID, "ATTACHMENT_INVALID_CONTENT", "附件内容无效", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "ATTACHMENT_OPERATION_FAILED", err.Error(), true)
	}
}
