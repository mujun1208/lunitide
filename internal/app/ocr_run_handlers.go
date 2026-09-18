package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func handleOCRRunList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, _, ok := parseOCRPublicScope(r.Payload)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.run.list 参数无效", false)
	}
	if kind == "project" {
		return r.Fail("OCR_SCOPE_FORBIDDEN", "当前范围没有可读取的 OCR 运行记录", false)
	}
	items := []any{}
	if store := e.ocrSQLite(); store != nil {
		owner := e.memorySubjectID()
		rows, err := store.OCRListRuns(ctx, owner, "user", owner, 20)
		if err != nil {
			return r.Fail("STORAGE_UNAVAILABLE", "OCR 运行记录暂时不可用", true)
		}
		for _, row := range rows {
			item := map[string]any{
				"runId":     row.RunID,
				"createdAt": row.CreatedAt,
				"status":    row.State,
			}
			if row.ArtifactID != "" {
				item["artifactId"] = row.ArtifactID
				item["previewBytes"] = row.PreviewBytes
			}
			items = append(items, item)
		}
	}
	return r.Ok(map[string]any{
		"items":      items,
		"nextCursor": nil,
	})
}

func handleOCRRunGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
		Limit int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.run.get 参数无效", false)
	}
	store := e.ocrSQLite()
	if store == nil {
		return r.Fail("OCR_RUN_NOT_FOUND", "OCR 运行记录不存在", false)
	}
	row, err := store.OCRGetRunRecord(ctx, e.memorySubjectID(), p.RunID)
	if err != nil {
		return failOCRRun(r, err)
	}
	pages, err := store.OCRListRunPages(ctx, row.RunID, p.Limit)
	if err != nil {
		return failOCRRun(r, err)
	}
	out := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		out = append(out, map[string]any{
			"page":      page.Page,
			"complete":  page.Complete,
			"uncertain": page.Uncertain,
		})
	}
	return r.Ok(map[string]any{
		"runId":      row.RunID,
		"pages":      out,
		"nextCursor": nil,
	})
}

func handleOCRArtifactRead(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ArtifactID string `json:"artifactId"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ArtifactID) || p.Offset < 0 || p.Limit < 1 || p.Limit > 65536 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.artifact.read 参数无效", false)
	}
	store := e.ocrSQLite()
	if store == nil {
		return r.Fail("OCR_ARTIFACT_MISSING", "OCR 结果已过期或不存在", false)
	}
	rec, err := store.OCRGetArtifact(ctx, e.memorySubjectID(), p.ArtifactID)
	if err != nil {
		if errors.Is(err, sqlite.ErrOCRScopeDenied) {
			return r.Fail("OCR_SCOPE_FORBIDDEN", "当前范围没有可读取的 OCR 运行记录", false)
		}
		if errors.Is(err, sqlite.ErrOCRRunNotFound) {
			return r.Fail("OCR_ARTIFACT_MISSING", "OCR 结果已过期或不存在", false)
		}
		return r.Fail("STORAGE_UNAVAILABLE", "OCR 结果暂时不可用", true)
	}
	raw, err := store.OCRReadArtifactBytes(rec)
	if err != nil {
		return r.Fail("OCR_ARTIFACT_MISSING", "OCR 结果已过期或不存在", false)
	}
	digest := rec.SHA256
	if digest == "" {
		sum := sha256.Sum256(raw)
		digest = hex.EncodeToString(sum[:])
	}
	return r.Ok(boundedBytesResult(raw, p.Offset, p.Limit, digest))
}

func failOCRRun(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, sqlite.ErrOCRScopeDenied):
		return r.Fail("OCR_SCOPE_FORBIDDEN", "当前范围没有可读取的 OCR 运行记录", false)
	case errors.Is(err, sqlite.ErrOCRRunNotFound):
		return r.Fail("OCR_RUN_NOT_FOUND", "OCR 运行记录不存在", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "OCR 运行记录暂时不可用", true)
	}
}

func boundedBytesResult(raw []byte, offset, limit int, digest string) map[string]any {
	total := len(raw)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	chunk := raw[offset:end]
	return map[string]any{
		"base64":     base64.StdEncoding.EncodeToString(chunk),
		"nextOffset": end,
		"eof":        end >= total,
		"sha256":     digest,
		"totalBytes": total,
	}
}
