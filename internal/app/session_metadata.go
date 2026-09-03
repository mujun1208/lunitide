package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
)

type sessionMetaAPI interface {
	GetMetadata(context.Context, string) (map[string]any, error)
	MergeMetadata(context.Context, string, map[string]any) (map[string]any, error)
}

type mroSessionContext struct {
	TailNo    string
	AsOf      string
	ManualIDs []string
	Pack      string
	Scenario  string
}

var mroAsOf = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func parseMROContext(raw any) (mroSessionContext, bool) {
	row, ok := raw.(map[string]any)
	if !ok {
		return mroSessionContext{}, false
	}
	tail := strings.TrimSpace(asString(row["tailNo"]))
	asOf := strings.TrimSpace(asString(row["asOf"]))
	if tail == "" || len(tail) > 32 || !mroAsOf.MatchString(asOf) {
		return mroSessionContext{}, false
	}
	ids := []string{}
	if rawIDs, ok := row["manualIds"].([]any); ok {
		for _, item := range rawIDs {
			id := strings.TrimSpace(asString(item))
			if len(id) == 26 {
				ids = append(ids, id)
			}
		}
	}
	scenario := asString(row["scenario"])
	if scenario != "fault" && scenario != "checklist" {
		scenario = "manual"
	}
	return mroSessionContext{TailNo: tail, AsOf: asOf, ManualIDs: ids, Pack: "mro.v1", Scenario: scenario}, true
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func (e *Engine) sessionMROContext(ctx context.Context, sessionID string) (tailNo, asOf string) {
	if e == nil || strings.TrimSpace(sessionID) == "" {
		return "", ""
	}
	api, ok := e.sessions.(sessionMetaAPI)
	if !ok {
		return "", ""
	}
	bag, err := api.GetMetadata(ctx, sessionID)
	if err != nil {
		return "", ""
	}
	parsed, ok := parseMROContext(bag["mroContext"])
	if !ok {
		return "", ""
	}
	return parsed.TailNo, parsed.AsOf
}

func handleSessionMetadataGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.metadata.get 参数无效", false)
	}
	api, ok := e.sessions.(sessionMetaAPI)
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "会话元数据暂时不可用", true)
	}
	bag, err := api.GetMetadata(ctx, p.SessionID)
	if err != nil {
		return sessionFailure(r, err)
	}
	out := map[string]any{}
	if parsed, ok := parseMROContext(bag["mroContext"]); ok {
		out["mroContext"] = map[string]any{
			"tailNo": parsed.TailNo, "asOf": parsed.AsOf, "manualIds": parsed.ManualIDs,
			"pack": parsed.Pack, "scenario": parsed.Scenario,
		}
	}
	return r.Ok(out)
}

func handleSessionMetadataSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID  string `json:"sessionId"`
		MROContext any    `json:"mroContext"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.metadata.set 参数无效", false)
	}
	parsed, ok := parseMROContext(p.MROContext)
	if !ok {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.metadata.set 参数无效", false)
	}
	api, ok := e.sessions.(sessionMetaAPI)
	if !ok {
		return r.Fail("STORAGE_UNAVAILABLE", "会话元数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	ctxMap := map[string]any{
		"tailNo": parsed.TailNo, "asOf": parsed.AsOf, "manualIds": parsed.ManualIDs,
		"pack": parsed.Pack, "scenario": parsed.Scenario,
	}
	if _, err := api.MergeMetadata(ctx, p.SessionID, map[string]any{"mroContext": ctxMap}); err != nil {
		return sessionFailure(r, err)
	}
	return r.Ok(map[string]any{"mroContext": ctxMap})
}
