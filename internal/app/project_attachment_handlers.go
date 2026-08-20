package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/oklog/ulid/v2"
)

type ProjectAttachmentStore interface {
	ListProjectAttachments(context.Context, projectattachment.Filter) ([]projectattachment.Attachment, error)
	CreateProjectAttachment(context.Context, projectattachment.Attachment) (projectattachment.Attachment, error)
	GetProjectAttachment(context.Context, string) (projectattachment.Attachment, error)
}

func projectAttachmentStoreAvailable(store ProjectAttachmentStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

type projectAttachmentDTO struct {
	AttachmentID string    `json:"attachmentId"`
	ProjectID    string    `json:"projectId"`
	Phase        int       `json:"phase"`
	Category     string    `json:"category"`
	FileName     string    `json:"fileName"`
	MimeType     string    `json:"mimeType"`
	Size         int64     `json:"size"`
	Digest       string    `json:"digest"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Version      int64     `json:"version"`
}

func newProjectAttachmentDTO(a projectattachment.Attachment, size int64) projectAttachmentDTO {
	return projectAttachmentDTO{
		AttachmentID: a.ID, ProjectID: a.ProjectID, Phase: a.Phase, Category: a.Category,
		FileName: a.FileName, MimeType: a.MimeType, Size: size, Digest: a.Digest,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, Version: a.Version,
	}
}

func handleProjectAttachmentList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Phase     int    `json:"phase"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "projectAttachment.list 参数无效", false)
	}
	if p.Phase != 0 && !projectattachment.ValidPhase(p.Phase) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "projectAttachment.list 参数无效", false)
	}
	if !projectAttachmentStoreAvailable(e.projectAttachments) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目附件数据暂时不可用", true)
	}
	items, err := e.projectAttachments.ListProjectAttachments(ctx, projectattachment.Filter{
		ProjectID: p.ProjectID, Phase: p.Phase,
	})
	if err != nil {
		return projectAttachmentFailure(r, err)
	}
	dtos := make([]projectAttachmentDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, newProjectAttachmentDTO(items[i], 0))
	}
	return bridge.Success(r.ID, map[string]any{"items": dtos})
}

func handleProjectAttachmentIngest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID     string `json:"projectId"`
		Phase         int    `json:"phase"`
		Category      string `json:"category"`
		FileName      string `json:"fileName"`
		MimeType      string `json:"mimeType"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		!validCanonicalULID(p.ProjectID) ||
		!projectattachment.ValidPhase(p.Phase) ||
		strings.TrimSpace(p.FileName) == "" ||
		strings.TrimSpace(p.MimeType) == "" ||
		p.ContentBase64 == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "projectAttachment.ingest 参数无效", false)
	}
	if len(p.ContentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxFileSize) {
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_ATTACHMENT_FILE_TOO_LARGE", "项目附件超过 10 MiB 限制", false)
	}
	decodedLen := base64.StdEncoding.DecodedLen(len(p.ContentBase64))
	if strings.HasSuffix(p.ContentBase64, "==") {
		decodedLen -= 2
	} else if strings.HasSuffix(p.ContentBase64, "=") {
		decodedLen--
	}
	if decodedLen > attachmentapp.MaxFileSize {
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_ATTACHMENT_FILE_TOO_LARGE", "项目附件超过 10 MiB 限制", false)
	}
	content, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "projectAttachment.ingest contentBase64 解码失败", false)
	}
	fileName, err := projectattachment.NormalizeFileName(p.FileName)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "projectAttachment.ingest 参数无效", false)
	}
	if !projectAttachmentStoreAvailable(e.projectAttachments) || e.projectAttachmentFiles == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "项目附件数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	digest := sha256.Sum256(content)
	id := ulid.Make().String()
	att := projectattachment.Attachment{
		ID:        id,
		ProjectID: p.ProjectID,
		Phase:     p.Phase,
		Category:  projectattachment.NormalizeCategory(p.Category),
		FileName:  fileName,
		MimeType:  strings.TrimSpace(p.MimeType),
		FilePath:  id,
		Digest:    hex.EncodeToString(digest[:]),
	}
	saved, err := e.ingestProjectAttachment(ctx, att, content)
	if err != nil {
		return projectAttachmentFailure(r, err)
	}
	dto := newProjectAttachmentDTO(saved, int64(len(content)))
	return bridge.Success(r.ID, dto)
}

func (e *Engine) ingestProjectAttachment(ctx context.Context, att projectattachment.Attachment, content []byte) (projectattachment.Attachment, error) {
	if len(content) > projectattachment.MaxFileSize {
		return projectattachment.Attachment{}, projectattachment.ErrFileTooLarge
	}
	if err := e.projectAttachmentFiles.WriteFile(ctx, att.FilePath, content); err != nil {
		return projectattachment.Attachment{}, err
	}
	saved, err := e.projectAttachments.CreateProjectAttachment(ctx, att)
	if err != nil {
		_ = e.projectAttachmentFiles.DeleteFile(ctx, att.FilePath)
		return projectattachment.Attachment{}, err
	}
	return saved, nil
}

func projectAttachmentFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, projectattachment.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_ATTACHMENT_NOT_FOUND", "项目附件不存在", false)
	case errors.Is(err, projectattachment.ErrFileTooLarge):
		return bridge.Failure(r.ID, r.TraceID, "PROJECT_ATTACHMENT_FILE_TOO_LARGE", "项目附件超过 10 MiB 限制", false)
	default:
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "项目附件操作暂时不可用"
		}
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", msg, true)
	}
}
