package attachmenthost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
)

type EngineCaller interface {
	Call(context.Context, bridge.Request) (bridge.Response, error)
}

type Handler struct {
	Allow  func(path string) bool
	Engine EngineCaller
}

func (h *Handler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string   `json:"projectId"`
		SessionID string   `json:"sessionId"`
		Paths     []string `json:"paths"`
	}
	if json.Unmarshal(r.Payload, &p) != nil || !canonicalULID(p.ProjectID) || (p.SessionID != "" && !canonicalULID(p.SessionID)) || len(p.Paths) < 1 || len(p.Paths) > 40 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "附件路径无效", false)
	}
	items := make([]any, 0, len(p.Paths))
	failed := make([]map[string]string, 0)
	for _, path := range p.Paths {
		name := filepath.Base(path)
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "附件"
		}
		if strings.ContainsRune(path, 0) || h.Allow == nil || !h.Allow(path) {
			failed = append(failed, map[string]string{"name": name, "path": path, "error": "不能读取未选择的文件"})
			continue
		}
		body, _ := json.Marshal(map[string]string{
			"projectId": p.ProjectID, "sessionId": p.SessionID, "path": path, "originalName": name,
		})
		resp, err := engineCall(ctx, h.Engine, "internal.attachment.importPath", body, 120_000)
		if err != nil || !resp.OK {
			message := "附件暂时无法读取"
			if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
				message = resp.Error.Message
			}
			failed = append(failed, map[string]string{"name": name, "path": path, "error": message})
			continue
		}
		raw, err := json.Marshal(resp.Payload)
		var item map[string]any
		if err != nil || json.Unmarshal(raw, &item) != nil || item["attachmentId"] == nil {
			failed = append(failed, map[string]string{"name": name, "path": path, "error": "附件暂时无法读取"})
			continue
		}
		items = append(items, item)
	}
	return bridge.Success(r.ID, map[string]any{"items": items, "failed": failed})
}

func canonicalULID(v string) bool {
	parsed, err := ulid.ParseStrict(v)
	return err == nil && parsed.String() == v && v[0] <= '7'
}
