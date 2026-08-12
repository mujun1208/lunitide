//go:build !windows

package workspaceapp

import (
	"context"
	"github.com/lunitide/lunitide/internal/bridge"
)

type Handler struct{}

func New(string) *Handler { return &Handler{} }
func (h *Handler) HandleHost(context.Context, bridge.Request) bridge.Response {
	panic("workspace host is Windows-only")
}
