package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/capabilitypack"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/oklog/ulid/v2"
)

func (e *Engine) SetCapabilityPackStore(store capabilitypack.Store) {
	e.capabilityPacks = capabilitypack.New(store, enginePackExecutor{e})
}

type enginePackExecutor struct{ e *Engine }

func (x enginePackExecutor) preset(key string) (mcp6.Preset, []string, error) {
	p, ok := mcp6.PresetByID(key)
	if !ok {
		return p, nil, fmt.Errorf("unknown MCP preset %s", key)
	}
	args := append([]string(nil), p.Args...)
	if p.NeedsArgs {
		arg := strings.TrimSpace(p.ArgDefault)
		if arg == "" && p.ArgPlaceholder == "{{dir}}" {
			arg = mcp6.PrepareSandbox(p.ID)
		}
		if arg == "" {
			return p, nil, fmt.Errorf("MCP %s needs %s", key, p.ArgHint)
		}
		args = p.ResolveArgs(arg)
	}
	return p, args, nil
}
func (x enginePackExecutor) Describe(ctx context.Context, kind, key string) (capabilitypack.Resource, error) {
	r := capabilitypack.Resource{Kind: kind, Key: key}
	switch kind {
	case "gate":
		if x.e.m8plugin == nil {
			return r, capabilitypack.ErrUnavailable
		}
		listed, err := x.e.m8plugin.List(ctx, "", "")
		if err != nil {
			return r, err
		}
		for _, p := range listed.Plugins {
			if p.PluginID == key {
				if p.Origin != "builtin" && p.Origin != "local" {
					return r, fmt.Errorf("gate %s is not built in", key)
				}
				r.TargetID = p.InstallID
				return r, nil
			}
		}
		return r, fmt.Errorf("gate %s is not installed", key)
	case "skill":
		if !skillServiceAvailable(x.e.skills) {
			return r, capabilitypack.ErrUnavailable
		}
		if _, ok := x.e.skills.(interface {
			EnsureCatalogPublished(context.Context, string) (skill.Skill, error)
		}); !ok {
			return r, capabilitypack.ErrUnavailable
		}
		for _, tpl := range skillapp.Catalog() {
			if tpl.ID == key {
				r.TargetID = "catalog:" + key
				return r, nil
			}
		}
		return r, skillapp.ErrTemplateUnknown
	case "mcp":
		if x.e.m7mcp == nil || x.e.mcp6Registry == nil {
			return r, capabilitypack.ErrUnavailable
		}
		p, args, err := x.preset(key)
		if err != nil {
			return r, err
		}
		raw, _ := json.Marshal(args)
		listed, err := x.e.m7mcp.List(ctx, p.Transport)
		if err != nil {
			return r, err
		}
		for _, ep := range listed {
			if ep.Command == p.Command && ep.ArgsJSON == string(raw) && ep.State != "revoked" {
				r.TargetID = ep.EndpointID
				return r, nil
			}
		}
		r.TargetID = "mcp-" + ulid.Make().String()
		return r, nil
	default:
		return r, capabilitypack.ErrConflict
	}
}
func (x enginePackExecutor) Ensure(ctx context.Context, r capabilitypack.Resource) (string, error) {
	switch r.Kind {
	case "gate":
		out, err := x.e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: r.TargetID, Enabled: true, Actor: "capability-pack"})
		return out.InstallID, err
	case "skill":
		svc, ok := x.e.skills.(interface {
			EnsureCatalogPublished(context.Context, string) (skill.Skill, error)
		})
		if !ok {
			return "", capabilitypack.ErrUnavailable
		}
		sk, err := svc.EnsureCatalogPublished(ctx, r.Key)
		return sk.ID, err
	case "mcp":
		p, args, err := x.preset(r.Key)
		if err != nil {
			return "", err
		}
		added, err := x.e.m7mcp.Add(ctx, m7app.McpAddInput{EndpointID: r.TargetID, Origin: "manual", Transport: p.Transport, Command: p.Command, Args: args, RiskConfirmed: true, Actor: "capability-pack", IdempotencyKey: r.TargetID})
		if err != nil {
			return "", err
		}
		ep, err := x.e.m7mcp.Toggle(ctx, added.EndpointID, true, "capability-pack")
		if err != nil {
			return "", err
		}
		if err = x.e.admitSettingsMcp(ctx, ep); err != nil {
			return "", err
		}
		x.e.rememberMcpPreset(ep.EndpointID, p.ID)
		return ep.EndpointID, nil
	default:
		return "", capabilitypack.ErrConflict
	}
}
func (x enginePackExecutor) Release(ctx context.Context, r capabilitypack.Resource) error {
	switch r.Kind {
	case "gate":
		_, err := x.e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: r.TargetID, Enabled: false, Actor: "capability-pack"})
		return err
	case "mcp":
		_, err := x.e.m7mcp.Toggle(ctx, r.TargetID, false, "capability-pack")
		if errors.Is(err, m7app.ErrMcpNotFound) {
			return nil
		}
		return err
	default:
		return capabilitypack.ErrConflict
	}
}
func (x enginePackExecutor) Mount(ctx context.Context, s capabilitypack.Spec) error {
	_, err := x.e.m8plugin.CreateAndMount(ctx, m8app.DevCreateInput{WorkspaceID: "capability-packs", Entrypoint: "pack://manifest", Manifest: map[string]any{"id": s.ID, "kind": "workflow", "publisher": "lunitide", "semver": "1.0.0", "name": s.Name, "description": s.Description, "skills": s.Skills, "mcpPresetIds": s.McpPresetIDs, "toolGates": s.ToolGates}})
	return err
}
func (x enginePackExecutor) Unmount(ctx context.Context, id string) error {
	listed, err := x.e.m8plugin.List(ctx, "", "")
	if err != nil {
		return err
	}
	for _, p := range listed.Plugins {
		if p.PluginID != id || p.State == "uninstalled" {
			continue
		}
		token, _, err := x.e.pluginConfirm.issue(p.InstallID, time.Now())
		if err != nil {
			return err
		}
		if !x.e.pluginConfirm.consume(token, p.InstallID, time.Now()) {
			return capabilitypack.ErrConflict
		}
		_, err = x.e.m8plugin.Uninstall(ctx, m8app.UninstallInput{InstallID: p.InstallID, ConfirmToken: token, Actor: "capability-pack"})
		return err
	}
	return nil
}
