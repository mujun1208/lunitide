package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/producthub"
)

func handleProductHub(e *Engine, ctx context.Context, r bridge.Request) (resp bridge.Response) {
	defer func() {
		if rec := recover(); rec != nil {
			resp = r.Fail("ENGINE_INTERNAL_ERROR", "productHub 内部错误", true)
		}
	}()
	if e == nil || e.productHub == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "产品知识中枢暂不可用", true)
	}
	method := string(r.Method)
	if method != "productHub.auth.status" && method != "productHub.auth.unlock" {
		token := payloadString(r.Payload, "sessionToken")
		if !e.productHub.Check(token) {
			return r.Fail("PH_012", "未解锁，无法使用产品知识中枢", false)
		}
		// 解锁后的每次中枢调用前镜像供应商/模型配置（与 Bridge 注入同模式）：
		// 诊断按当前快照审计模型融合，模型升级或换默认模型后下次调用即重算。
		syncModelSlots(e, ctx)
	}
	switch method {
	case "productHub.auth.status":
		return r.Ok(e.productHub.Status(ctx, payloadString(r.Payload, "sessionToken")))
	case "productHub.auth.unlock":
		token, err := e.productHub.Unlock(ctx, payloadString(r.Payload, "username"), payloadString(r.Payload, "password"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"sessionToken": token, "username": producthub.AdminUsername, "unlocked": true})
	case "productHub.auth.changePassword":
		err := e.productHub.ChangePassword(ctx, payloadString(r.Payload, "sessionToken"), payloadString(r.Payload, "currentPassword"), payloadString(r.Payload, "newPassword"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"ok": true})
	case "productHub.status", "productHub.overview":
		ov, err := e.productHub.Overview(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(ov)
	case "productHub.refresh":
		ctx = producthub.WithLandscape(ctx, landscapeNotes(r.Payload))
		ed, err := e.productHub.Generate(ctx, "manual")
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{
			"editionId": ed.EditionID, "generatedAt": ed.GeneratedAt,
			"cardCount": ed.CardCount, "healthScore": ed.HealthScore,
			"added": ed.Added, "updated": ed.Updated, "removed": ed.Removed,
			"reportMarkdown": ed.ReportMarkdown, "reportHtml": ed.ReportHTML,
		})
	case "productHub.featureCard":
		card, ok, err := e.productHub.FeatureCard(ctx, payloadString(r.Payload, "stableKey"))
		if err != nil {
			return failProductHub(r, err)
		}
		if !ok {
			return r.Fail("PH_018", "没有这张功能卡", false)
		}
		return r.Ok(map[string]any{"card": card})
	case "productHub.graph":
		g, err := e.productHub.Graph(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(g)
	case "productHub.node":
		n, ok, err := e.productHub.Node(ctx, payloadString(r.Payload, "id"))
		if err != nil {
			return failProductHub(r, err)
		}
		if !ok {
			return r.Fail("PH_018", "没有这个节点", false)
		}
		return r.Ok(n)
	case "productHub.changelog":
		ch, err := e.productHub.Changelog(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"changes": ch})
	case "productHub.diagnostics":
		ctx = producthub.WithLandscape(ctx, landscapeNotes(r.Payload))
		findings, md, html, err := e.productHub.Diagnostics(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"findings": findings, "reportMarkdown": md, "reportHtml": html})
	case "productHub.tags":
		tags, err := e.productHub.Tags(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"tags": tags})
	case "productHub.tagSet":
		err := e.productHub.TagSet(ctx, payloadString(r.Payload, "stableKey"), payloadString(r.Payload, "vocab"), payloadString(r.Payload, "value"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"ok": true})
	case "productHub.export":
		format := payloadString(r.Payload, "format")
		content, mime, err := e.productHub.Export(ctx, format)
		if err != nil {
			return failProductHub(r, err)
		}
		out := map[string]any{"content": content, "mime": mime}
		if path, saveErr := saveProductHubExport(format, content); saveErr == nil {
			out["path"] = path
		}
		return r.Ok(out)
	case "productHub.apply":
		res, err := e.productHub.Apply(ctx, payloadString(r.Payload, "errorCode"), payloadString(r.Payload, "stableKey"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(res)
	case "productHub.landscape.collect":
		result, err := e.productHub.CollectLandscape(ctx, payloadStrings(r.Payload, "names"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(result)
	case "productHub.landscape.drafts":
		drafts, err := e.productHub.LandscapeDrafts(ctx)
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"drafts": drafts})
	case "productHub.landscape.confirm":
		draft, err := e.productHub.ConfirmLandscapeDraft(ctx, payloadString(r.Payload, "id"))
		if err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"draft": draft})
	case "productHub.landscape.discard":
		if err := e.productHub.DiscardLandscapeDraft(ctx, payloadString(r.Payload, "id")); err != nil {
			return failProductHub(r, err)
		}
		return r.Ok(map[string]any{"ok": true})
	default:
		return r.Fail("BAD_REQUEST", "未知 productHub 方法", false)
	}
}

// syncModelSlots mirrors the engine's provider configuration into the product
// hub. The hub audits model fusion from this snapshot without importing the
// provider domain or the engine.
func syncModelSlots(e *Engine, ctx context.Context) {
	if e == nil || e.providers == nil {
		return
	}
	providers, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return
	}
	var slots []producthub.ModelSlot
	for _, p := range providers {
		for _, m := range p.Models {
			slots = append(slots, producthub.ModelSlot{
				ProviderID:      p.ID,
				ProviderName:    p.Name,
				Protocol:        string(p.Protocol),
				ModelID:         m.ModelID,
				DisplayName:     m.DisplayName,
				Kind:            string(m.EffectiveKind()),
				IsDefault:       m.IsDefault,
				KindDefault:     m.KindDefault,
				ContextWindow:   m.ContextWindow,
				SupportsVision:  m.SupportsVision,
				Status:          string(p.Status),
				CredentialState: string(p.CredentialState),
			})
		}
	}
	producthub.SetModelSlots(slots)
}

func failProductHub(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, producthub.ErrAuthDenied):
		return r.Fail("PH_012", "未解锁，无法使用产品知识中枢", false)
	case errors.Is(err, producthub.ErrBadCredentials):
		return r.Fail("PH_013", "用户名或密码不正确", false)
	case errors.Is(err, producthub.ErrRateLimited):
		return r.Fail("PH_005", "生成过于频繁，请稍后再试", true)
	case errors.Is(err, producthub.ErrNotImplemented):
		return r.Fail("PH_006", "该导出格式尚未实现", false)
	case errors.Is(err, producthub.ErrNotFound):
		return r.Fail("PH_018", "没有这条诊断或功能卡", false)
	case errors.Is(err, producthub.ErrNoSource):
		return r.Fail("PH_023", "本机没有产品源码，不能自净化修复升级，请联系管理员", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "产品知识中枢暂不可用", true)
	}
}

func landscapeNotes(raw json.RawMessage) []producthub.LandscapeNote {
	var body struct {
		Landscape []producthub.LandscapeNote `json:"landscape"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	return body.Landscape
}

func payloadString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func payloadStrings(raw json.RawMessage, key string) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	item, ok := m[key]
	if !ok {
		return nil
	}
	var list []string
	if json.Unmarshal(item, &list) != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func saveProductHubExport(format, content string) (string, error) {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	desktop := filepath.Join(dir, "Desktop")
	if info, statErr := os.Stat(desktop); statErr != nil || !info.IsDir() {
		desktop = dir
	}
	name := "lunitide-诊断报告-" + time.Now().Format("20060102-1504") + ".md"
	if format == "html" {
		name = "lunitide-产品手册-" + time.Now().Format("20060102-1504") + ".html"
	}
	path := filepath.Join(desktop, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
