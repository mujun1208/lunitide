package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
)

// handleTemplateReadChunk streams one slice of an enabled office template so
// the frontend can assemble files too large for a single bridge frame (see
// templateOpenInlineMax). It enforces the same scope, status, and type rules
// as the office branch of template.open.
func handleTemplateReadChunk(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID     string `json:"id"`
		Offset int64  `json:"offset"`
		Limit  int64  `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.Offset < 0 ||
		p.Limit < 0 || p.Limit > templateReadChunkLimitMax {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.readChunk 参数无效", false)
	}
	limit := p.Limit
	if limit == 0 {
		limit = templateReadChunkLimitDefault
	}
	if !assetStoreAvailable(e.assets) || e.templateFiles == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "资产文件暂时不可用", true)
	}
	tpl, err := e.assets.GetAssetTemplate(ctx, p.ID)
	if err != nil {
		return assetFailure(r, err)
	}
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	if tpl.OrgID != orgID {
		return assetFailure(r, asset.ErrNotFound)
	}
	name := templateFileName(tpl)
	if name == "" {
		return r.Fail("TEMPLATE_OPEN_FAILED", "附件文件名无效", false)
	}
	if tpl.Status != asset.StatusEnabled {
		return r.Fail("TEMPLATE_OPEN_FAILED", "只有已启用的办公模版可以引用", false)
	}
	switch tpl.TemplateType {
	case asset.TemplateTypePPT, asset.TemplateTypeWord, asset.TemplateTypeExcel:
	default:
		return r.Fail("TEMPLATE_OPEN_FAILED", "办公工作台只引用 PPT、Word、Excel 模版", false)
	}
	content, err := e.templateFiles.ReadFile(ctx, tpl.FilePath)
	if err != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "上传的附件不存在或无法读取", false)
	}
	total := int64(len(content))
	if p.Offset >= total {
		return r.Ok(map[string]any{
			"fileName": name, "contentBase64": "", "nextOffset": p.Offset, "total": total, "eof": true,
		})
	}
	end := p.Offset + limit
	if end > total || end < p.Offset {
		end = total
	}
	chunk := content[p.Offset:end]
	return r.Ok(map[string]any{
		"fileName":      name,
		"contentBase64": encodeTemplateBytes(chunk),
		"nextOffset":    end,
		"total":         total,
		"eof":           end >= total,
	})
}

// templateFileName returns the validated local file name of tpl, or "" when
// the stored name is unsafe. It mirrors the office branch of template.open.
func templateFileName(tpl asset.AssetTemplate) string {
	name := filepath.Base(strings.ReplaceAll(tpl.FileName, `\`, "/"))
	if name != tpl.FileName || !filepath.IsLocal(name) || strings.ContainsAny(name, ":\x00") ||
		asset.ValidateTemplateFile(tpl.TemplateType, name) != nil {
		return ""
	}
	return name
}
