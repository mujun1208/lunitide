//go:build !windows

package conversationsapp

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

type HostHandler struct{}

func NewHostHandler() *HostHandler { return &HostHandler{} }

func (h *HostHandler) HandleHost(_ context.Context, r bridge.Request) bridge.Response {
	return bridge.Failure(r.ID, r.TraceID, "PLATFORM_UNSUPPORTED", "对话目录选择仅支持 Windows", false)
}
