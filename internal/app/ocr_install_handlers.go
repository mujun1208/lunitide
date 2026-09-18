package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

func handleOCRInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	_ = ctx
	if e == nil || e.ocr == nil {
		return r.Fail("OCR-002", "本机 OCR 安装不可用", true)
	}
	var p struct {
		Probe bool `json:"probe"`
	}
	if len(r.Payload) > 0 && string(r.Payload) != "{}" && string(r.Payload) != "null" {
		var fields map[string]any
		if decodePayload(r.Payload, &fields) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.install 参数无效", false)
		}
		for key, value := range fields {
			flag, ok := value.(bool)
			if key != "probe" || !ok {
				return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.install 参数无效", false)
			}
			p.Probe = flag
		}
	}
	if p.Probe {
		e.ocr.RefreshInstallState()
	} else {
		e.ocr.BeginInstall()
	}
	return r.Ok(e.ocr.InstallSnapshot())
}
