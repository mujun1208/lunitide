package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
)

func handleTemplateOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.open 参数无效", false)
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
	name := filepath.Base(strings.ReplaceAll(tpl.FileName, `\`, "/"))
	if name != tpl.FileName || !filepath.IsLocal(name) || strings.ContainsAny(name, ":\x00") || asset.ValidateTemplateFile(tpl.TemplateType, name) != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "附件文件名无效", false)
	}
	content, err := e.templateFiles.ReadFile(ctx, tpl.FilePath)
	if err != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "上传的附件不存在或无法读取", false)
	}
	// The application opens an independent copy, never the managed asset original.
	dir, err := os.MkdirTemp("", "lunitide-asset-view-")
	if err != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "无法创建查看副本", true)
	}
	target := filepath.Join(dir, name)
	opened := false
	defer func() {
		if !opened {
			_ = os.Remove(target)
			_ = os.Remove(dir)
		}
	}()
	if err = os.WriteFile(target, content, 0600); err != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "无法写入查看副本", true)
	}
	if err = openArtifactTarget(target, true, false); err != nil {
		return r.Fail("TEMPLATE_OPEN_FAILED", "无法打开附件，请确认已安装对应应用", false)
	}
	opened = true
	return r.Ok(map[string]any{"opened": true})
}
