package app

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/oklog/ulid/v2"
)

type AssetTemplateStore interface {
	CreateAssetTemplate(context.Context, asset.AssetTemplate) (asset.AssetTemplate, error)
	GetAssetTemplate(context.Context, string) (asset.AssetTemplate, error)
	ListAssetTemplates(context.Context, asset.Filter) ([]asset.AssetTemplate, error)
	UpdateAssetTemplateStatus(context.Context, string, int64, asset.Status) (asset.AssetTemplate, error)
	DeleteAssetTemplate(context.Context, string, int64) error
}

func assetStoreAvailable(store AssetTemplateStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

type assetTemplateDTO struct {
	ID           string             `json:"id"`
	TemplateCode string             `json:"templateCode"`
	Name         string             `json:"name"`
	TemplateType asset.TemplateType `json:"templateType"`
	DocumentType asset.DocumentType `json:"documentType"`
	Description  string             `json:"description"`
	Client       string             `json:"client"`
	MimeType     string             `json:"mimeType"`
	FileName     string             `json:"fileName"`
	FilePath     string             `json:"filePath"`
	Status       asset.Status       `json:"status"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
	Version      int64              `json:"version"`
}

func newAssetTemplateDTO(t asset.AssetTemplate) assetTemplateDTO {
	return assetTemplateDTO{
		ID: t.ID, TemplateCode: t.TemplateCode, Name: t.Name, TemplateType: t.TemplateType,
		DocumentType: t.DocumentType, Description: t.Description, Client: t.Client,
		MimeType: t.MimeType, FileName: t.FileName, FilePath: t.FilePath, Status: t.Status,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Version: t.Version,
	}
}

func handleTemplateList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Status       string `json:"status"`
		TemplateType string `json:"templateType"`
		DocumentType string `json:"documentType"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	if p.Status != "" && !asset.ValidStatus(asset.Status(p.Status)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if p.TemplateType != "" && !asset.ValidTemplateType(asset.TemplateType(p.TemplateType)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if p.DocumentType != "" && !asset.ValidDocumentType(asset.DocumentType(p.DocumentType)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	items, err := e.assets.ListAssetTemplates(ctx, asset.Filter{
		Status: asset.Status(p.Status), TemplateType: asset.TemplateType(p.TemplateType),
		DocumentType: asset.DocumentType(p.DocumentType),
	})
	if err != nil {
		return assetFailure(r, err)
	}
	dtos := make([]assetTemplateDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, newAssetTemplateDTO(items[i]))
	}
	return bridge.Success(r.ID, map[string]any{"items": dtos})
}

func handleTemplateCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name           string `json:"name"`
		TemplateType   string `json:"templateType"`
		DocumentType   string `json:"documentType"`
		Description    string `json:"description"`
		Client         string `json:"client"`
		MimeType       string `json:"mimeType"`
		FileName       string `json:"fileName"`
		FilePath       string `json:"filePath"`
		ContentBase64  string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	if e.templateFiles == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板文件存储暂时不可用", true)
	}
	name, err := asset.NormalizeName(p.Name)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	tplType := asset.TemplateType(p.TemplateType)
	if !asset.ValidTemplateType(tplType) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	docType := asset.DocumentType(p.DocumentType)
	if tplType == asset.TemplateTypeDocument {
		if docType == "" || !asset.ValidDocumentType(docType) {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
		}
	} else {
		docType = ""
	}
	desc := clampText(p.Description, 2000)
	if desc == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	fileName := strings.TrimSpace(p.FileName)
	if fileName == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 需要上传附件", false)
	}
	if err := asset.ValidateTemplateFile(tplType, fileName); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	if p.ContentBase64 == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create 需要上传附件", false)
	}
	if len(p.ContentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxFileSize) {
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_FILE_TOO_LARGE", "模板附件超过 10 MiB 限制", false)
	}
	content, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil || len(content) == 0 || len(content) > attachmentapp.MaxFileSize {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.create contentBase64 无效", false)
	}
	mimeType := asset.DetectMimeType(fileName)
	fileRef := ulid.Make().String()
	if err := e.templateFiles.WriteFile(ctx, fileRef, content); err != nil {
		return assetFailure(r, err)
	}
	created, err := e.assets.CreateAssetTemplate(ctx, asset.AssetTemplate{
		Name: name, TemplateType: tplType, DocumentType: docType,
		Description: desc, Client: clampText(p.Client, 200),
		MimeType: mimeType, FileName: clampText(fileName, 260),
		FilePath: fileRef, Status: asset.StatusDraft,
	})
	if err != nil {
		_ = e.templateFiles.DeleteFile(ctx, fileRef)
		return assetFailure(r, err)
	}
	return bridge.Success(r.ID, newAssetTemplateDTO(created))
}

func handleTemplateEnable(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.enable 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusEnabled)
	if err != nil {
		return assetFailure(r, err)
	}
	return bridge.Success(r.ID, newAssetTemplateDTO(updated))
}

func handleTemplateVoid(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.void 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusVoid)
	if err != nil {
		return assetFailure(r, err)
	}
	return bridge.Success(r.ID, newAssetTemplateDTO(updated))
}

func handleTemplateRestore(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.restore 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusDraft)
	if err != nil {
		return assetFailure(r, err)
	}
	return bridge.Success(r.ID, newAssetTemplateDTO(updated))
}

func handleTemplateDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.delete 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	cur, err := e.assets.GetAssetTemplate(ctx, p.ID)
	if err != nil {
		return assetFailure(r, err)
	}
	if err := e.assets.DeleteAssetTemplate(ctx, p.ID, p.ExpectedVersion); err != nil {
		return assetFailure(r, err)
	}
	if e.templateFiles != nil && strings.TrimSpace(cur.FilePath) != "" {
		_ = e.templateFiles.DeleteFile(ctx, cur.FilePath)
	}
	return bridge.Success(r.ID, map[string]any{"deleted": true})
}

func assetFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, asset.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_NOT_FOUND", "模板不存在", false)
	case errors.Is(err, asset.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_INVALID_TRANSITION", "模板状态门禁不允许该操作", false)
	case errors.Is(err, asset.ErrTemplateReferenced):
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_REFERENCED", "模板已被引用，不能删除", false)
	case errors.Is(err, asset.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_VERSION_CONFLICT", "模板已被其他操作修改，请刷新后重试", false)
	default:
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "模板数据暂时不可用"
		}
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", msg, true)
	}
}
