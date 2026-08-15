package systemsettings

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/bridge"
)

type Handler struct {
	OpenMicrophone func() error
}

func (h *Handler) HandleHost(_ context.Context, request bridge.Request) bridge.Response {
	var payload struct {
		Page string `json:"page"`
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) == nil || payload.Page != "privacy-microphone" {
		return bridge.Failure(request.ID, request.TraceID, "INVALID_SYSTEM_SETTINGS_PAGE", "系统设置页面无效", false)
	}
	if h.OpenMicrophone == nil || h.OpenMicrophone() != nil {
		return bridge.Failure(request.ID, request.TraceID, "SYSTEM_SETTINGS_OPEN_FAILED", "无法打开 Windows 麦克风设置", true)
	}
	return bridge.Success(request.ID, struct {
		Opened bool `json:"opened"`
	}{Opened: true})
}
