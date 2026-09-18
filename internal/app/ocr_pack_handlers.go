package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

const ocrPackIDPaddleVL = "paddleocr-vl-1.6"

func (e *Engine) ocrSQLite() *sqlite.Store {
	if e == nil {
		return nil
	}
	return e.sqlStore
}

func handleOCRPackGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackID string `json:"packId"`
	}
	if decodePayload(r.Payload, &p) != nil || p.PackID != ocrPackIDPaddleVL {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.pack.get 参数无效", false)
	}
	reason := "NO_VERIFIED_RUNTIME_PROFILE"
	pack := map[string]any{
		"packId":          ocrPackIDPaddleVL,
		"availability":    "not_installed",
		"currentVersion":  nil,
		"previousVersion": nil,
		"manifestDigest":  nil,
		"engineVersion":   nil,
		"deviceKind":      nil,
		"lastHealthAt":    nil,
		"lastErrorCode":   nil,
		"revision":        1,
	}
	gate := map[string]any{
		"installAllowed":               false,
		"autoRouteAllowed":             false,
		"reasonCode":                   reason,
		"verifiedRuntimeProfileDigest": nil,
		"revision":                     1,
	}
	if store := e.ocrSQLite(); store != nil {
		if row, err := store.OCRPackGate(ctx, ocrPackIDPaddleVL); err == nil {
			gate["revision"] = row.Revision
		}
		if st, err := store.OCRPackState(ctx, ocrPackIDPaddleVL); err == nil {
			pack["availability"] = st.Availability
			pack["revision"] = st.Revision
			pack["currentVersion"] = nullSQLString(st.CurrentVersion)
			pack["previousVersion"] = nullSQLString(st.PreviousVersion)
			pack["lastHealthAt"] = nullSQLString(st.LastHealthAt)
			if st.LastErrorCode == "" {
				pack["lastErrorCode"] = nil
			} else {
				pack["lastErrorCode"] = st.LastErrorCode
			}
		}
	}
	return r.Ok(map[string]any{
		"pack":      pack,
		"gate":      gate,
		"operation": nil,
		"release":   nil,
	})
}

func nullSQLString(v sql.NullString) any {
	if !v.Valid || v.String == "" {
		return nil
	}
	return v.String
}

func handleOCRPackInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return rejectOCRPackMutation(e, ctx, r, "ocr.pack.install")
}

func handleOCRPackCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return rejectOCRPackMutation(e, ctx, r, "ocr.pack.cancel")
}

func handleOCRPackUninstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return rejectOCRPackMutation(e, ctx, r, "ocr.pack.uninstall")
}

func rejectOCRPackMutation(e *Engine, ctx context.Context, r bridge.Request, method string) bridge.Response {
	_ = e
	_ = ctx
	var raw map[string]any
	if decodePayload(r.Payload, &raw) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", method+" 参数无效", false)
	}
	return r.Fail("NO_VERIFIED_RUNTIME_PROFILE", "尚无经验证的 Windows 运行包，不能安装或变更 OCR 模型包", false)
}

func handleOCRPackNoticeList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackID string `json:"packId"`
		Limit  int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || p.PackID != ocrPackIDPaddleVL {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.pack.notice.list 参数无效", false)
	}
	items := []any{}
	if store := e.ocrSQLite(); store != nil {
		rows, err := store.OCRNoticeList(ctx, ocrPackIDPaddleVL)
		if err != nil {
			return r.Fail("STORAGE_UNAVAILABLE", "OCR 许可文本暂时不可用", true)
		}
		limit := p.Limit
		if limit <= 0 || limit > 50 {
			limit = 50
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		for _, row := range rows {
			items = append(items, map[string]any{
				"packId":         ocrPackIDPaddleVL,
				"manifestDigest": row.ManifestDigest,
				"version":        row.Version,
				"noticeDigest":   row.NoticeDigest,
				"noticeBytes":    len(row.NoticeBytes),
				"verifiedAt":     row.VerifiedAt,
				"installedAt":    nullSQLString(row.InstalledAt),
				"uninstalledAt":  nullSQLString(row.UninstalledAt),
			})
		}
	}
	return r.Ok(map[string]any{
		"items":      items,
		"nextCursor": nil,
	})
}

func handleOCRPackNoticeRead(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackID         string `json:"packId"`
		ManifestDigest string `json:"manifestDigest"`
		Offset         int    `json:"offset"`
		Limit          int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || p.PackID != ocrPackIDPaddleVL || p.Offset < 0 || p.Limit < 1 || p.Limit > 65536 || len(p.ManifestDigest) != 64 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.pack.notice.read 参数无效", false)
	}
	store := e.ocrSQLite()
	if store == nil {
		return r.Fail("OCR_PACK_NOTICE_UNAVAILABLE", "本机没有已验证的 OCR 许可文本", false)
	}
	row, err := store.OCRNoticeRead(ctx, ocrPackIDPaddleVL, p.ManifestDigest)
	if err != nil {
		if errors.Is(err, sqlite.ErrOCRNoticeUnavailable) {
			return r.Fail("OCR_PACK_NOTICE_UNAVAILABLE", "本机没有已验证的 OCR 许可文本", false)
		}
		return r.Fail("STORAGE_UNAVAILABLE", "OCR 许可文本暂时不可用", true)
	}
	return r.Ok(boundedBytesResult(row.NoticeBytes, p.Offset, p.Limit, row.NoticeDigest))
}
