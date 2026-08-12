package uitheme

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/bridge"
	"sync"
)

type Handler struct {
	mu    sync.RWMutex
	apply func(bool) bool
}

func (h *Handler) Bind(apply func(bool) bool) { h.mu.Lock(); h.apply = apply; h.mu.Unlock() }
func (h *Handler) HandleHost(_ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Theme string `json:"theme"`
	}
	decoder := json.NewDecoder(bytes.NewReader(r.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(&struct{}{}) == nil || (p.Theme != "dark" && p.Theme != "light") {
		return bridge.Failure(r.ID, r.TraceID, "INVALID_THEME", "主题值无效", false)
	}
	h.mu.RLock()
	apply := h.apply
	h.mu.RUnlock()
	applied := apply != nil && apply(p.Theme == "dark")
	return bridge.Success(r.ID, struct {
		Applied bool `json:"applied"`
	}{applied})
}
