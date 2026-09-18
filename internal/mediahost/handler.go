package mediahost

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/desktopfiles"
)

type EngineCaller interface {
	Call(context.Context, bridge.Request) (bridge.Response, error)
}

type Handler struct {
	Pick   func(folder, multiple bool) ([]desktopfiles.Item, []string, error)
	Engine EngineCaller
	Player *Player
}

func (h *Handler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	switch r.Method {
	case "media.asset.pick":
		return h.handlePick(ctx, r)
	case "media.element.report":
		return h.handleElementReport(ctx, r)
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "未知的媒体方法", false)
	}
}

func (h *Handler) handleElementReport(ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		MediaSessionID string `json:"mediaSessionId"`
		Event          string `json:"event"`
		PositionMs     int64  `json:"positionMs"`
		DurationMs     int64  `json:"durationMs"`
	}
	if json.Unmarshal(r.Payload, &p) != nil || len(p.MediaSessionID) != 26 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.element.report 参数无效", false)
	}
	switch p.Event {
	case "playing", "pause", "ended", "stalled", "error", "position":
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.element.report 参数无效", false)
	}
	if h.Player == nil {
		return bridge.Success(r.ID, map[string]any{"accepted": false})
	}
	accepted, err := h.Player.Observe(ctx, p.MediaSessionID, p.Event, p.PositionMs, p.DurationMs)
	if err != nil {
		if strings.Contains(err.Error(), "invalid") {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.element.report 参数无效", false)
		}
		return bridge.Failure(r.ID, r.TraceID, "ENGINE_UNAVAILABLE", "核心引擎暂时不可用", true)
	}
	return bridge.Success(r.ID, map[string]any{"accepted": accepted})
}

func (h *Handler) handlePick(ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind string `json:"scopeKind"`
		ScopeID   string `json:"scopeId"`
		Multiple  bool   `json:"multiple"`
	}
	if json.Unmarshal(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.asset.pick 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.asset.pick 参数无效", false)
	}
	if p.ScopeKind == "project" && strings.TrimSpace(p.ScopeID) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "media.asset.pick 参数无效", false)
	}
	if h.Pick == nil {
		return bridge.Failure(r.ID, r.TraceID, "DESKTOP_PICK_UNAVAILABLE", "系统没打开文件框，请再试一次。", false)
	}
	items, _, err := h.Pick(false, p.Multiple)
	if errors.Is(err, desktopfiles.ErrCanceled) {
		return bridge.Success(r.ID, map[string]any{"canceled": true, "assets": []any{}})
	}
	if errors.Is(err, desktopfiles.ErrUnavailable) {
		return bridge.Failure(r.ID, r.TraceID, "DESKTOP_PICK_UNAVAILABLE", "系统没打开文件框，请再试一次。", false)
	}
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "DESKTOP_PICK_FAILED", "系统没打开文件框，请再试一次。", false)
	}
	if h.Engine == nil {
		return bridge.Failure(r.ID, r.TraceID, "ENGINE_UNAVAILABLE", "核心引擎暂时不可用", true)
	}
	assets := make([]any, 0, len(items))
	for _, item := range items {
		kind, mime := classifyMedia(item.Path, item.MIME)
		if kind == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"scopeKind":  p.ScopeKind,
			"sourceKind": "user_selected",
			"path":       item.Path,
			"mime":       mime,
			"kind":       kind,
			"title":      item.FileName,
			"size":       item.Size,
		})
		if p.ScopeKind == "project" {
			payload, _ = json.Marshal(map[string]any{
				"scopeKind":  p.ScopeKind,
				"scopeId":    p.ScopeID,
				"sourceKind": "user_selected",
				"path":       item.Path,
				"mime":       mime,
				"kind":       kind,
				"title":      item.FileName,
				"size":       item.Size,
			})
		}
		resp, callErr := h.Engine.Call(ctx, bridge.Request{
			Version: bridge.Version, Kind: "request", ID: r.ID, TraceID: r.TraceID,
			Method: "internal.media.asset.register", Payload: payload, DeadlineMS: r.DeadlineMS,
		})
		if callErr != nil {
			return bridge.Failure(r.ID, r.TraceID, "ENGINE_UNAVAILABLE", "核心引擎暂时不可用", true)
		}
		if !resp.OK {
			return resp
		}
		assets = append(assets, resp.Payload)
		if len(assets) >= 50 {
			break
		}
	}
	return bridge.Success(r.ID, map[string]any{"canceled": false, "assets": assets})
}

func classifyMedia(path, mime string) (kind, outMIME string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg", ".oga":
		if mime == "" {
			mime = audioMIME(ext)
		}
		return "audio", mime
	case ".mp4", ".webm", ".mkv", ".mov", ".avi", ".m4v":
		if mime == "" {
			mime = videoMIME(ext)
		}
		return "video", mime
	}
	lower := strings.ToLower(mime)
	if strings.HasPrefix(lower, "audio/") {
		return "audio", mime
	}
	if strings.HasPrefix(lower, "video/") {
		return "video", mime
	}
	return "", ""
}

func audioMIME(ext string) string {
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	default:
		return "audio/ogg"
	}
}

func videoMIME(ext string) string {
	switch ext {
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	default:
		return "video/mp4"
	}
}
