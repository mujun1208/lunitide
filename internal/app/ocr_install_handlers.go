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
	if len(r.Payload) > 0 && string(r.Payload) != "{}" && string(r.Payload) != "null" {
		var empty map[string]any
		if decodePayload(r.Payload, &empty) != nil || len(empty) > 0 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "ocr.install 参数无效", false)
		}
	}
	e.ocr.BeginInstall()
	return r.Ok(e.ocr.InstallSnapshot())
}
