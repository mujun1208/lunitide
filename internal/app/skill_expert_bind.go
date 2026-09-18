package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m8app"
)

func (e *Engine) attachDeclaredBindKeys(ctx context.Context, keys ...string) {
	if e == nil || e.m8expert == nil {
		return
	}
	_ = e.m8expert.AttachDeclaredKeys(ctx, keys)
}

func (e *Engine) detachDeclaredBindKeys(ctx context.Context, keys ...string) {
	if e == nil || e.m8expert == nil {
		return
	}
	_ = e.m8expert.DetachBoundKeys(ctx, keys)
}

func (e *Engine) bindKeyPresent(ctx context.Context, key string) bool {
	key = strings.TrimSpace(key)
	if rest, ok := strings.CutPrefix(key, m8app.BoundMcpPrefix); ok {
		return e.mcpPresetInstalled(ctx, rest)
	}
	if strings.HasPrefix(key, m8app.BoundBrainPrefix) {
		return true
	}
	return e.skillBindKeyPresent(ctx, key)
}

func (e *Engine) skillBindKeyPresent(ctx context.Context, key string) bool {
	if !skillServiceAvailable(e.skills) {
		return false
	}
	listed, err := e.skills.List(ctx, skill.SkillStatusPublished)
	if err != nil {
		return false
	}
	want := []string{m8app.CanonicalDeclaredKey(key)}
	for _, item := range listed {
		if m8app.SkillMatchesPreferred(item.Name, item.EntryPoint, want) {
			return true
		}
	}
	return false
}

func (e *Engine) mcpPresetInstalled(ctx context.Context, presetID string) bool {
	if e == nil || e.m7mcp == nil || strings.TrimSpace(presetID) == "" {
		return false
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil {
		return false
	}
	for _, ep := range eps {
		if ep.State == m7flow.McpStateRevoked || ep.State == m7flow.McpStateQuarantined {
			continue
		}
		if e.endpointPresetID(ep.EndpointID) == presetID {
			return true
		}
		args := parseMcpArgsJSON(ep.ArgsJSON)
		if presetIDFromTarget(ep.Command, args, ep.URL) == presetID {
			return true
		}
	}
	return false
}
