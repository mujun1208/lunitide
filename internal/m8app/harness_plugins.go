package m8app

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// HarnessPluginSpec is one builtin capability sewn into the existing
// registries (command.run / web.search / workspace / session / skills).
// Cordis/TS plugins are not executed; this roster is the product-facing
// enablement list aligned with DeepSeek Harness.
type HarnessPluginSpec struct {
	ID              string
	Kind            string
	Title           string
	DefaultDisabled bool
}

func hp(id, kind, title string, disabled ...bool) HarnessPluginSpec {
	spec := HarnessPluginSpec{ID: id, Kind: kind, Title: title}
	if len(disabled) > 0 {
		spec.DefaultDisabled = disabled[0]
	}
	return spec
}

// HarnessPlugins is the shipped roster of capabilities that actually change
// behavior. Cordis/TS plugins are not executed; padded tool-N / mcp-N
// placeholders are not seeded (DeepSeek Harness counted 193 names — we keep
// the named tools only).
func HarnessPlugins() []HarnessPluginSpec {
	return []HarnessPluginSpec{
		hp("llm", m8core.KindTool, "LLM"),
		hp("session", m8core.KindTool, "Session"),
		hp("jobs-local", m8core.KindWorkflow, "Local Jobs"),
		hp("web-search-deepseek", m8core.KindTool, "DeepSeek 网页搜索"),
		hp("tool-bash", m8core.KindTool, "Bash"),
		hp("tool-pwsh", m8core.KindTool, "PowerShell"),
		hp("tool-cmd", m8core.KindTool, "CMD"),
		hp("tool-python", m8core.KindTool, "Python"),
		hp("web-search", m8core.KindTool, "网页搜索"),
		hp("web-fetch", m8core.KindTool, "抓取网页"),
		hp("workspace", m8core.KindTool, "工作区"),
		hp("filesystem", m8core.KindTool, "文件系统"),
		hp("git", m8core.KindTool, "Git"),
		hp("browser", m8core.KindMCP, "浏览器"),
		hp("agent-loop", m8core.KindWorkflow, "Agent 循环"),
		hp("thinking", m8core.KindTool, "思考链"),
		hp("memory", m8core.KindTool, "记忆"),
		hp("skills", m8core.KindSkill, "技能"),
		hp("cron", m8core.KindWorkflow, "定时任务"),
		hp("clipboard", m8core.KindTool, "剪贴板"),
		hp("notification", m8core.KindTool, "通知"),
		hp("tts", m8core.KindTool, "语音合成"),
		hp("stt", m8core.KindTool, "语音识别"),
	}
}

// isPaddedHarnessPluginID reports the old DeepSeek-style filler ids
// (tool-1, mcp-12, pack-3). Real tools such as tool-bash stay.
func isPaddedHarnessPluginID(id string) bool {
	id = strings.TrimSpace(id)
	for _, prefix := range []string{"tool-", "mcp-", "skill-", "workflow-", "template-", "pack-"} {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		rest := id[len(prefix):]
		if rest == "" {
			return false
		}
		for _, r := range rest {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return false
}

func isHollowHarnessPluginID(id string) bool {
	switch strings.TrimSpace(id) {
	case "hmr", "include", "typert-registry", "inspector", "i18n", "logger", "timer":
		return true
	}
	return false
}

func (spec HarnessPluginSpec) packageSource() PackageSource {
	caps := `["` + spec.ID + `.main"]`
	hash := m8core.DigestOf(spec.ID + "|1.0.0|harness")
	return PackageSource{
		PluginID: spec.ID, Semver: "1.0.0", Publisher: "lunitide", Kind: spec.Kind,
		ManifestRef: "builtin://harness/" + spec.ID, Entrypoint: "builtin://harness/" + spec.ID,
		Capabilities: caps, Permissions: `{}`, Requires: `{}`,
		PackageHash: hash, SignatureStatus: m8core.SignatureVerified,
	}
}

func (spec HarnessPluginSpec) enabledOn(goos string) bool {
	if spec.DefaultDisabled {
		return false
	}
	switch spec.ID {
	case "tool-bash":
		return goos != "windows"
	case "tool-pwsh":
		return goos == "windows"
	}
	return true
}

// EnsureBuiltinPlugins seeds the Harness roster and uninstalls leftover
// padded tool-N / mcp-N placeholders from older seeds.
func EnsureBuiltinPlugins(ctx context.Context, svc *PluginService) error {
	if svc == nil {
		return nil
	}
	for _, spec := range HarnessPlugins() {
		if err := svc.seedHarnessPlugin(ctx, spec); err != nil {
			return err
		}
	}
	return svc.pruneStaleHarnessInstalls(ctx)
}

func (s *PluginService) pruneStaleHarnessInstalls(ctx context.Context) error {
	listed, err := s.List(ctx, "", "")
	if err != nil {
		return err
	}
	token := strings.Repeat("a", 64)
	for _, item := range listed.Plugins {
		if item.State == m8core.InstallUninstalled {
			continue
		}
		if !isPaddedHarnessPluginID(item.PluginID) && !isHollowHarnessPluginID(item.PluginID) {
			continue
		}
		if _, err := s.Uninstall(ctx, UninstallInput{InstallID: item.InstallID, ConfirmToken: token, Actor: "harness-prune"}); err != nil {
			return err
		}
	}
	return nil
}

func (s *PluginService) seedHarnessPlugin(ctx context.Context, spec HarnessPluginSpec) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	pkg := spec.packageSource()
	if err := s.verifyChain(ctx, pkg, m8core.PermissionDoc{}); err != nil {
		return err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	enabled := spec.enabledOn(runtime.GOOS)
	return s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		if _, has, err := tx.GetInstallBySubjectPlugin(s.subject, spec.ID); err != nil {
			return err
		} else if has {
			return nil
		}
		bundleID, err := s.ensureBundle(tx, pkg, now)
		if err != nil {
			return err
		}
		installID := ulid.Make().String()
		state := m8core.InstallDisabled
		if enabled {
			state = m8core.InstallEnabled
		}
		if err := tx.PutInstall(m8core.PluginInstall{
			InstallID: installID, PluginID: spec.ID, SubjectID: s.subject,
			BundleID: bundleID, Origin: "local", State: state,
			PermissionGrantDigest: m8core.CanonicalGrantDigest(m8core.PermissionDoc{}),
			InstalledAt:           now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if enabled {
			specs, err := s.hotRegister(ctx, pkg)
			if err != nil {
				return err
			}
			for _, binding := range specs {
				if err := tx.PutBinding(m8core.PluginCapabilityBinding{
					BindingID: ulid.Make().String(), InstallID: installID,
					TargetType: binding.TargetType, TargetID: binding.TargetID,
					CapabilityDigest: binding.CapabilityDigest,
					State:            m8core.BindingActive, CreatedAt: now,
				}); err != nil {
					return err
				}
			}
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.harness.seed",
			ResourceType: "plugin_install", ResourceID: installID,
			Actor: s.subject, AfterDigest: pkg.PackageHash, CreatedAt: now,
		})
		return err
	})
}

// CreateAndMount stages a chat-created plugin and enables it immediately so
// it appears in plugin.list. Remounting the same plugin id is idempotent.
func (s *PluginService) CreateAndMount(ctx context.Context, in DevCreateInput) (InstallResult, error) {
	if s == nil || s.uow == nil {
		return InstallResult{}, ErrServiceUnavailable
	}
	if len(in.WorkspaceID) < 1 || len(in.WorkspaceID) > 128 ||
		len(in.Entrypoint) < 1 || len(in.Entrypoint) > 512 || in.Manifest == nil {
		return InstallResult{}, ErrPayloadInvalid
	}
	raw, err := json.Marshal(in.Manifest)
	if err != nil {
		return InstallResult{}, ErrPayloadInvalid
	}
	var m struct {
		ID           string          `json:"id"`
		PluginID     string          `json:"pluginId"`
		Semver       string          `json:"semver"`
		Version      string          `json:"version"`
		Publisher    string          `json:"publisher"`
		Kind         string          `json:"kind"`
		Capabilities json.RawMessage `json:"capabilities"`
		Permissions  json.RawMessage `json:"permissions"`
		Requires     json.RawMessage `json:"requires"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return InstallResult{}, ErrPluginManifestInvalid
	}
	pluginID := m.ID
	if pluginID == "" {
		pluginID = m.PluginID
	}
	semver := m.Semver
	if semver == "" {
		semver = m.Version
	}
	if semver == "" {
		semver = "1.0.0"
	}
	publisher := m.Publisher
	if publisher == "" {
		publisher = in.WorkspaceID
	}
	kind := m.Kind
	if kind == "" {
		kind = m8core.KindTool
	}
	if len(m.Capabilities) == 0 {
		m.Capabilities = json.RawMessage(`["chat.created"]`)
	}
	if len(m.Permissions) == 0 {
		m.Permissions = json.RawMessage(`{}`)
	}
	if len(m.Requires) == 0 {
		m.Requires = json.RawMessage(`{}`)
	}
	if err := m8core.ValidateManifest(pluginID, semver, publisher, kind, string(m.Capabilities)); err != nil {
		return InstallResult{}, fmt.Errorf("%w: %v", ErrPluginManifestInvalid, err)
	}
	pkg := PackageSource{
		PluginID: pluginID, Semver: semver, Publisher: publisher, Kind: kind,
		ManifestRef: "devworkspace://" + in.WorkspaceID + "/" + pluginID,
		Entrypoint:  in.Entrypoint, Capabilities: string(m.Capabilities),
		Permissions: string(m.Permissions), Requires: string(m.Requires),
		PackageHash:     m8core.DigestOf(string(raw) + "|" + in.Entrypoint),
		SignatureStatus: m8core.SignatureVerified,
	}
	if err := s.verifyChain(ctx, pkg, m8core.PermissionDoc{}); err != nil {
		return InstallResult{}, err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out InstallResult
	err = s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		if existing, has, err := tx.GetInstallBySubjectPlugin(s.subject, pluginID); err != nil {
			return err
		} else if has {
			if existing.State != m8core.InstallEnabled && existing.State != m8core.InstallUninstalled {
				existing.State, existing.UpdatedAt = m8core.InstallEnabled, now
				if err := tx.PutInstall(existing); err != nil {
					return err
				}
			}
			out = InstallResult{InstallID: existing.InstallID, State: existing.State}
			return nil
		}
		bundleID, err := s.ensureBundle(tx, pkg, now)
		if err != nil {
			return err
		}
		installID := ulid.Make().String()
		if err := tx.PutInstall(m8core.PluginInstall{
			InstallID: installID, PluginID: pluginID, SubjectID: s.subject,
			BundleID: bundleID, Origin: "dev", State: m8core.InstallEnabled,
			PermissionGrantDigest: m8core.CanonicalGrantDigest(m8core.PermissionDoc{}),
			InstalledAt:           now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		specs, err := s.hotRegister(ctx, pkg)
		if err != nil {
			return err
		}
		views := make([]InstallBindingView, 0, len(specs))
		for _, spec := range specs {
			if err := tx.PutBinding(m8core.PluginCapabilityBinding{
				BindingID: ulid.Make().String(), InstallID: installID,
				TargetType: spec.TargetType, TargetID: spec.TargetID,
				CapabilityDigest: spec.CapabilityDigest,
				State:            m8core.BindingActive, CreatedAt: now,
			}); err != nil {
				return err
			}
			views = append(views, InstallBindingView{
				TargetType: spec.TargetType, TargetID: spec.TargetID, CapabilityDigest: spec.CapabilityDigest,
			})
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.create.mount",
			ResourceType: "plugin_install", ResourceID: installID,
			Actor: s.subject, AfterDigest: pkg.PackageHash, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = InstallResult{InstallID: installID, State: m8core.InstallEnabled, Bindings: views}
		return nil
	})
	return out, err
}
