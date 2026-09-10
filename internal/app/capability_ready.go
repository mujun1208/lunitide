package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/ocrapp"
)

type CapabilityReadiness struct {
	ID           string
	Availability string
	Detail       string
	Code         string
}

func (e *Engine) capabilityReadiness(ctx context.Context, id string) CapabilityReadiness {
	switch strings.TrimSpace(id) {
	case "chat":
		if e == nil || e.providers == nil {
			return CapabilityReadiness{ID: "chat", Availability: "missing_dependency", Detail: "模型供应商服务未装配", Code: "DEPENDENCY_MISSING"}
		}
		rows, err := e.providers.List(ctx, provider.Filter{})
		if err != nil {
			return CapabilityReadiness{ID: "chat", Availability: "unavailable", Detail: "供应商目录暂时不可用", Code: "STORAGE_UNAVAILABLE"}
		}
		for _, p := range rows {
			if p.Status == provider.StatusEnabled && p.CredentialState == provider.CredentialConfigured && len(p.Models) > 0 {
				return CapabilityReadiness{ID: "chat", Availability: "ready", Detail: "已配置可用模型"}
			}
		}
		return CapabilityReadiness{ID: "chat", Availability: "needs_config", Detail: "请先配置并启用供应商、凭据和模型", Code: "CAPABILITY_NOT_READY"}
	case "ocr":
		if e == nil || e.ocr == nil {
			return CapabilityReadiness{ID: "ocr", Availability: "missing_dependency", Detail: "OCR 入口未装配", Code: "DEPENDENCY_MISSING"}
		}
		routing, err := e.ocr.Routing()
		if err != nil {
			return CapabilityReadiness{ID: "ocr", Availability: "unavailable", Detail: "OCR 路由暂时不可用", Code: "STORAGE_UNAVAILABLE"}
		}
		return ocrCapabilityFrom(routing, e.ocr.HealthSnapshot().Local)
	case "files":
		if e == nil || (e.fileOps == nil && e.tools == nil) {
			return CapabilityReadiness{ID: "files", Availability: "missing_dependency", Detail: "文件批处理未装配", Code: "DEPENDENCY_MISSING"}
		}
		return CapabilityReadiness{ID: "files", Availability: "ready", Detail: "计划/执行/撤销可用"}
	case "desktop", "computer":
		if e == nil || e.ccctrl == nil {
			return CapabilityReadiness{ID: "desktop", Availability: "missing_dependency", Detail: "电脑控制服务未装配", Code: "DEPENDENCY_MISSING"}
		}
		cfg, err := e.ccctrl.GetConfig(ctx)
		if err != nil {
			return CapabilityReadiness{ID: "desktop", Availability: "unavailable", Detail: "电脑控制配置暂时不可用", Code: "STORAGE_UNAVAILABLE"}
		}
		if cfg.EmergencyStopped {
			return CapabilityReadiness{ID: "desktop", Availability: "unavailable", Detail: "急停锁存中，恢复后需重新明确启用", Code: "SCOPE_DENIED"}
		}
		if !cfg.Enabled {
			return CapabilityReadiness{ID: "desktop", Availability: "needs_config", Detail: "请先在设置中启用电脑控制", Code: "CAPABILITY_NOT_READY"}
		}
		return CapabilityReadiness{ID: "desktop", Availability: "ready", Detail: "电脑控制已启用"}
	case "gui":
		if e == nil || e.providers == nil {
			return CapabilityReadiness{ID: "gui", Availability: "missing_dependency", Detail: "模型供应商服务未装配", Code: "DEPENDENCY_MISSING"}
		}
		items, err := e.providers.List(ctx, provider.Filter{})
		if err != nil {
			return CapabilityReadiness{ID: "gui", Availability: "unavailable", Detail: "供应商目录暂时不可用", Code: "STORAGE_UNAVAILABLE"}
		}
		gui := e.preferBoundCatalog(ctx, "gui", provider.CatalogForKind(items, provider.KindGUI))
		vision := e.preferBoundCatalog(ctx, "vision", provider.VisionDescribeCatalog(items, ""))
		if len(gui)+len(vision) == 0 {
			return CapabilityReadiness{ID: "gui", Availability: "needs_config", Detail: "请先配置 GUI 或视觉模型", Code: "CAPABILITY_NOT_READY"}
		}
		return CapabilityReadiness{ID: "gui", Availability: "ready", Detail: "已配置 GUI 或视觉模型"}
	default:
		return CapabilityReadiness{ID: id, Availability: "unverified", Detail: "未检查该能力", Code: "CAPABILITY_NOT_READY"}
	}
}

func ocrCapabilityFrom(routing ocrapp.Routing, local ocrapp.LocalReady) CapabilityReadiness {
	localOK := local.PDF || local.Image
	if !localOK && !routing.Bound() {
		return CapabilityReadiness{ID: "ocr", Availability: "needs_config", Detail: "请先配置 OCR 供应商或安装本机识别", Code: "CAPABILITY_NOT_READY"}
	}
	if routing.Bound() && !localOK {
		return CapabilityReadiness{ID: "ocr", Availability: "ready", Detail: "仅供应商，本机不可用"}
	}
	if !routing.Bound() {
		return CapabilityReadiness{ID: "ocr", Availability: "ready", Detail: "本机识别可用"}
	}
	if routing.PreferProvider {
		return CapabilityReadiness{ID: "ocr", Availability: "ready", Detail: "供应商优先，不可用时本地兜底"}
	}
	return CapabilityReadiness{ID: "ocr", Availability: "ready", Detail: "本机优先，供应商可用"}
}

func isComputerCapabilityTool(name string) bool {
	return name == "computer.act" || name == "desktop.type" || strings.HasPrefix(name, "cc.")
}

func (e *Engine) desktopCapabilityGuard(ctx context.Context, name string) error {
	if !isComputerCapabilityTool(name) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ready := e.capabilityReadiness(ctx, "desktop")
	if ready.Availability == "ready" {
		return nil
	}
	code := strings.TrimSpace(ready.Code)
	if code == "" {
		code = "CAPABILITY_NOT_READY"
	}
	detail := strings.TrimSpace(ready.Detail)
	if detail == "" {
		detail = "电脑控制未就绪"
	}
	return fmt.Errorf("%s: %s", code, detail)
}

func capabilityDeniedOutput(out string) bool {
	upper := strings.ToUpper(out)
	return strings.Contains(upper, "CAPABILITY_NOT_READY") ||
		strings.Contains(upper, "DEPENDENCY_MISSING") ||
		strings.Contains(upper, "SCOPE_DENIED")
}
