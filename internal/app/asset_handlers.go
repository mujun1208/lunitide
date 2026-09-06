package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

type assetTemplateCreationStore interface {
	ReplayAssetTemplateCreation(context.Context, string, string) (asset.AssetTemplate, bool, error)
	CreateAssetTemplateIdempotent(context.Context, string, string, asset.AssetTemplate) (asset.AssetTemplate, error)
}

func assetStoreAvailable(store AssetTemplateStore) bool {
	if store == nil {
		return false
	}
	v := reflect.ValueOf(store)
	return v.Kind() != reflect.Pointer || !v.IsNil()
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
	OrgID        string             `json:"orgId,omitempty"`
	Status       asset.Status       `json:"status"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
	Version      int64              `json:"version"`
}

func newAssetTemplateDTO(t asset.AssetTemplate) assetTemplateDTO {
	return assetTemplateDTO{
		ID: t.ID, TemplateCode: t.TemplateCode, Name: t.Name, TemplateType: t.TemplateType,
		OrgID:        t.OrgID,
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
		Cursor       string `json:"cursor"`
		Query        string `json:"query"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	if p.Status != "" && !asset.ValidStatus(asset.Status(p.Status)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if p.TemplateType != "" && !asset.ValidTemplateType(asset.TemplateType(p.TemplateType)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	if p.DocumentType != "" && !asset.ValidDocumentType(asset.DocumentType(p.DocumentType)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.list 参数无效", false)
	}
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	if len([]rune(p.Query)) > 200 || len(p.Cursor) > 1024 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "分页或搜索参数无效", false)
	}
	querySum := sha256.Sum256([]byte(orgID + "\x00" + p.Status + "\x00" + p.TemplateType + "\x00" + p.DocumentType + "\x00" + p.Query))
	queryKey := hex.EncodeToString(querySum[:])
	var cursor struct {
		CreatedAt string `json:"createdAt"`
		ID        string `json:"id"`
		QueryKey  string `json:"queryKey"`
	}
	if p.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		if err != nil || decodePayload(data, &cursor) != nil || cursor.QueryKey != queryKey || !validCanonicalULID(cursor.ID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "分页游标已失效，请重新加载", false)
		}
		if _, err = time.Parse(time.RFC3339Nano, cursor.CreatedAt); err != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "分页游标无效", false)
		}
	}
	items, err := e.assets.ListAssetTemplates(ctx, asset.Filter{
		Status: asset.Status(p.Status), TemplateType: asset.TemplateType(p.TemplateType),
		DocumentType: asset.DocumentType(p.DocumentType),
		OrgID:        orgID, Scoped: true, Query: p.Query, Limit: 101, BeforeCreatedAt: cursor.CreatedAt, BeforeID: cursor.ID,
	})
	if err != nil {
		return assetFailure(r, err)
	}
	nextCursor := ""
	if len(items) > 100 {
		items = items[:100]
		last := items[len(items)-1]
		cursor.CreatedAt, cursor.ID, cursor.QueryKey = last.CreatedAt.Format(time.RFC3339Nano), last.ID, queryKey
		raw, _ := json.Marshal(cursor)
		nextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	dtos := make([]assetTemplateDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, newAssetTemplateDTO(items[i]))
	}
	return r.Ok(map[string]any{"items": dtos, "nextCursor": nextCursor})
}

func handleTemplateCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Name          string `json:"name"`
		TemplateType  string `json:"templateType"`
		DocumentType  string `json:"documentType"`
		Description   string `json:"description"`
		Client        string `json:"client"`
		MimeType      string `json:"mimeType"`
		FileName      string `json:"fileName"`
		FilePath      string `json:"filePath"`
		UploadID      string `json:"uploadId"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	if e.templateFiles == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "模板文件存储暂时不可用", true)
	}
	name, err := asset.NormalizeName(p.Name)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	tplType := asset.TemplateType(p.TemplateType)
	if !asset.ValidTemplateType(tplType) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	docType := asset.DocumentType(p.DocumentType)
	if tplType == asset.TemplateTypeDocument {
		if docType == "" || !asset.ValidDocumentType(docType) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
		}
	} else {
		docType = ""
	}
	desc := clampText(p.Description, 2000)
	if desc == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 参数无效", false)
	}
	fileName := strings.TrimSpace(p.FileName)
	if fileName == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 需要上传附件", false)
	}
	if err := asset.ValidateTemplateFile(tplType, fileName); err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	if strings.TrimSpace(p.UploadID) == "" && strings.TrimSpace(p.ContentBase64) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 需要上传附件", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	creationStore, ok := e.assets.(assetTemplateCreationStore)
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "模板创建事务暂时不可用", true)
	}
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	requestJSON, err := json.Marshal(struct {
		OrgID   string
		Payload any
	}{orgID, p})
	if err != nil {
		return assetFailure(r, err)
	}
	requestHash := sha256.Sum256(requestJSON)
	digest := hex.EncodeToString(requestHash[:])
	e.templateCreateMu.Lock()
	defer e.templateCreateMu.Unlock()
	if ctx.Err() != nil {
		return assetFailure(r, ctx.Err())
	}
	if replay, found, err := creationStore.ReplayAssetTemplateCreation(ctx, r.IdempotencyKey, digest); err != nil {
		return assetFailure(r, err)
	} else if found {
		return r.Ok(newAssetTemplateDTO(replay))
	}
	var content []byte
	switch {
	case strings.TrimSpace(p.UploadID) != "":
		if strings.TrimSpace(p.ContentBase64) != "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 不能同时提供 uploadId 与 contentBase64", false)
		}
		state := e.templateStage()
		state.mu.Lock()
		upload := state.uploads[strings.TrimSpace(p.UploadID)]
		matching := upload != nil && upload.orgID == orgID
		state.mu.Unlock()
		if !matching {
			return r.Fail("DATA_SCOPE_DENIED", "当前组织无法使用该上传", false)
		}
		content, err = e.consumeTemplateStage(strings.TrimSpace(p.UploadID))
		if err != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 分片上传未完成或已过期", false)
		}
	case strings.TrimSpace(p.ContentBase64) != "":
		if len(p.ContentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxFileSize) {
			return r.Fail("TEMPLATE_FILE_TOO_LARGE", "模板附件超过 10 MiB 限制", false)
		}
		content, err = base64.StdEncoding.DecodeString(p.ContentBase64)
		if err != nil || len(content) == 0 || len(content) > attachmentapp.MaxFileSize {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create contentBase64 无效", false)
		}
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.create 需要上传附件", false)
	}
	mimeType := asset.DetectMimeType(fileName)
	fileRef := ulid.Make().String()
	if err := e.templateFiles.WriteFile(ctx, fileRef, content); err != nil {
		return assetFailure(r, err)
	}
	created, err := creationStore.CreateAssetTemplateIdempotent(ctx, r.IdempotencyKey, digest, asset.AssetTemplate{
		OrgID: orgID,
		Name:  name, TemplateType: tplType, DocumentType: docType,
		Description: desc, Client: clampText(p.Client, 200),
		MimeType: mimeType, FileName: clampText(fileName, 260),
		FilePath: fileRef, Status: asset.StatusDraft,
	})
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancelCleanup()
	if err != nil {
		// Commit may have succeeded before its acknowledgement was lost. Only
		// delete our file after a successful lookup proves it is unreferenced.
		replay, found, lookupErr := creationStore.ReplayAssetTemplateCreation(cleanupCtx, r.IdempotencyKey, digest)
		if lookupErr != nil {
			return assetFailure(r, fmt.Errorf("模板创建结果待核对，已保留文件以供恢复: %w", errors.Join(err, lookupErr)))
		}
		if !found {
			cleanupErr := e.templateFiles.DeleteFile(cleanupCtx, fileRef)
			return assetFailure(r, errors.Join(err, cleanupErr))
		}
		created = replay
	}
	if created.FilePath != fileRef {
		if err := e.templateFiles.DeleteFile(cleanupCtx, fileRef); err != nil {
			return assetFailure(r, fmt.Errorf("模板已创建，但重复暂存文件清理失败，请刷新列表核对: %w", err))
		}
	}
	if strings.TrimSpace(p.UploadID) != "" {
		e.finishTemplateStage(strings.TrimSpace(p.UploadID))
	}
	return r.Ok(newAssetTemplateDTO(created))
}

func handleTemplateEnable(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.enable 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusEnabled)
	if err != nil {
		return assetFailure(r, err)
	}
	return r.Ok(newAssetTemplateDTO(updated))
}

func handleTemplateVoid(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.void 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusVoid)
	if err != nil {
		return assetFailure(r, err)
	}
	return r.Ok(newAssetTemplateDTO(updated))
}

func handleTemplateRestore(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.restore 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	updated, err := e.assets.UpdateAssetTemplateStatus(ctx, p.ID, p.ExpectedVersion, asset.StatusDraft)
	if err != nil {
		return assetFailure(r, err)
	}
	return r.Ok(newAssetTemplateDTO(updated))
}

func handleTemplateDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID              string `json:"id"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.ExpectedVersion < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.delete 参数无效", false)
	}
	if !assetStoreAvailable(e.assets) {
		return r.Fail("STORAGE_UNAVAILABLE", "模板数据暂时不可用", true)
	}
	e.templateCreateMu.Lock()
	defer e.templateCreateMu.Unlock()
	cur, err := e.assets.GetAssetTemplate(ctx, p.ID)
	if err != nil {
		return assetFailure(r, err)
	}
	if err := e.assets.DeleteAssetTemplate(ctx, p.ID, p.ExpectedVersion); err != nil {
		return assetFailure(r, err)
	}
	cleanupPending := false
	if strings.TrimSpace(cur.FilePath) != "" {
		if e.templateFiles == nil {
			cleanupPending = true
		} else if err := e.templateFiles.DeleteFile(ctx, cur.FilePath); err != nil {
			cleanupPending = true
			log.Printf("template deleted; file cleanup pending: %v", err)
		}
	}
	return r.Ok(map[string]any{"deleted": true, "cleanupPending": cleanupPending})
}

func assetFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, asset.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同模板创建请求", false)
	case errors.Is(err, asset.ErrCreationExpired):
		return r.Fail("IDEMPOTENCY_CONFLICT", "创建请求的回放已过期，请刷新资产列表核对；需要另一份模板时重新发起创建", false)
	case errors.Is(err, asset.ErrNotFound):
		return r.Fail("TEMPLATE_NOT_FOUND", "模板不存在", false)
	case errors.Is(err, asset.ErrInvalidTransition):
		return r.Fail("TEMPLATE_INVALID_TRANSITION", "模板状态门禁不允许该操作", false)
	case errors.Is(err, asset.ErrTemplateReferenced):
		return r.Fail("TEMPLATE_REFERENCED", "模板已被引用，不能删除", false)
	case errors.Is(err, asset.ErrVersionConflict):
		return r.Fail("TEMPLATE_VERSION_CONFLICT", "模板已被其他操作修改，请刷新后重试", false)
	default:
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "模板数据暂时不可用"
		}
		return r.Fail("STORAGE_UNAVAILABLE", msg, true)
	}
}
