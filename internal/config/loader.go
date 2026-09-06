package config

// F-03 unified configuration loader. This is a purely additive, read-only
// aggregation view over the global sidecar JSON files that already live under
// the engine data root. It does NOT replace any service's independent loading
// path; each service keeps reading its own file as before. The Loader offers a
// single parsed snapshot plus a change-notification abstraction for consumers
// that want a consolidated view.
//
// Import discipline: this file must stay free of any dependency on
// internal/app (and any other higher layer) to avoid an import cycle.
// Structures owned by internal/app (inbound-routes, mcp-presets,
// preferred-chat) are read as json.RawMessage / plain maps rather than by
// importing the app types.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Global sidecar file names. Per-session files (.turns/<sid>, .todos,
// .companion) are intentionally NOT aggregated here.
const (
	fileWorkspaceRoot     = "workspace-root.json"
	fileConversationsRoot = "conversations-root.json"
	fileDatasourceSecrets = "datasource-secrets.json"
	fileInboundRoutes     = "inbound-routes.json"
	fileMCPPresets        = "mcp-presets.json"

	// org/binding.json lives under the "org" subdirectory (see
	// cmd/engine/main.go:353-359).
	subdirOrg   = "org"
	fileBinding = "binding.json"

	// command-policy.json / hooks-policy.json live under the
	// "tool-workspaces" subdirectory (see cmd/engine/main.go:415-419 and
	// internal/toolruntime/runtime.go:121-122).
	subdirToolWorkspaces = "tool-workspaces"
	fileCommandPolicy    = "command-policy.json"
	fileHooksPolicy      = "hooks-policy.json"

	// .turns/preferred-chat.json (see internal/app/chat_model.go:51).
	subdirTurns        = ".turns"
	filePreferredChat  = "preferred-chat.json"
)

// AppConfig is a parsed, read-only snapshot of the global sidecar files that
// existed at Load time. Missing files leave their field at the zero value.
type AppConfig struct {
	// WorkspaceRoot is the resolved path from workspace-root.json {"path"}.
	WorkspaceRoot string
	// ConversationsRoot is the resolved path from conversations-root.json
	// {"path"}.
	ConversationsRoot string
	// DatasourceSecrets maps a datasource ref -> DSN (datasource-secrets.json).
	DatasourceSecrets map[string]string
	// OrgBindingID is the bound org id from org/binding.json {"orgId"}.
	OrgBindingID string
	// CommandPolicyRaw is the raw tool-workspaces/command-policy.json document.
	CommandPolicyRaw json.RawMessage
	// HooksPolicyRaw is the raw tool-workspaces/hooks-policy.json document.
	HooksPolicyRaw json.RawMessage
	// InboundRoutes maps a session id -> raw route object (inbound-routes.json).
	// Kept as json.RawMessage to avoid importing internal/app route types.
	InboundRoutes map[string]json.RawMessage
	// MCPPresets maps an endpoint id -> preset id (mcp-presets.json).
	MCPPresets map[string]string
	// PreferredChatProviderID / PreferredChatModel come from
	// .turns/preferred-chat.json {"providerId","modelId"}.
	PreferredChatProviderID string
	PreferredChatModel      string
}

// clone returns a deep-enough copy so callers cannot mutate the Loader's
// retained snapshot through the returned pointer.
func (c *AppConfig) clone() *AppConfig {
	if c == nil {
		return nil
	}
	out := &AppConfig{
		WorkspaceRoot:           c.WorkspaceRoot,
		ConversationsRoot:       c.ConversationsRoot,
		OrgBindingID:            c.OrgBindingID,
		PreferredChatProviderID: c.PreferredChatProviderID,
		PreferredChatModel:      c.PreferredChatModel,
	}
	if len(c.CommandPolicyRaw) > 0 {
		out.CommandPolicyRaw = append(json.RawMessage(nil), c.CommandPolicyRaw...)
	}
	if len(c.HooksPolicyRaw) > 0 {
		out.HooksPolicyRaw = append(json.RawMessage(nil), c.HooksPolicyRaw...)
	}
	if c.DatasourceSecrets != nil {
		out.DatasourceSecrets = make(map[string]string, len(c.DatasourceSecrets))
		for k, v := range c.DatasourceSecrets {
			out.DatasourceSecrets[k] = v
		}
	}
	if c.MCPPresets != nil {
		out.MCPPresets = make(map[string]string, len(c.MCPPresets))
		for k, v := range c.MCPPresets {
			out.MCPPresets[k] = v
		}
	}
	if c.InboundRoutes != nil {
		out.InboundRoutes = make(map[string]json.RawMessage, len(c.InboundRoutes))
		for k, v := range c.InboundRoutes {
			out.InboundRoutes[k] = append(json.RawMessage(nil), v...)
		}
	}
	return out
}

// Loader aggregates the global sidecars under a single data root. It is safe
// for concurrent use. It never writes any file.
type Loader struct {
	dataRoot string
	mu       sync.RWMutex
	snapshot *AppConfig
	subs     []chan struct{}
}

// NewLoader builds a Loader rooted at the engine data root (the same dataRoot
// created in cmd/engine/main.go). It does not touch disk until Load/Snapshot.
func NewLoader(dataRoot string) *Loader {
	return &Loader{dataRoot: dataRoot}
}

// Load reads and parses every global sidecar that exists. A missing file is
// treated as a zero value (no error). Only a JSON parse failure returns an
// error, and the error names the offending file. On success the internal
// snapshot is atomically replaced and a copy is returned.
func (l *Loader) Load() (*AppConfig, error) {
	cfg := &AppConfig{}

	// workspace-root.json {"path"}
	if root, ok, err := l.readPathField(l.join(fileWorkspaceRoot), fileWorkspaceRoot); err != nil {
		return nil, err
	} else if ok {
		cfg.WorkspaceRoot = root
	}

	// conversations-root.json {"path"}
	if root, ok, err := l.readPathField(l.join(fileConversationsRoot), fileConversationsRoot); err != nil {
		return nil, err
	} else if ok {
		cfg.ConversationsRoot = root
	}

	// datasource-secrets.json -> map[string]string
	if m, ok, err := l.readStringMap(l.join(fileDatasourceSecrets), fileDatasourceSecrets); err != nil {
		return nil, err
	} else if ok {
		cfg.DatasourceSecrets = m
	}

	// org/binding.json {"orgId"}
	if id, ok, err := l.readOrgID(l.join(subdirOrg, fileBinding), fileBinding); err != nil {
		return nil, err
	} else if ok {
		cfg.OrgBindingID = id
	}

	// tool-workspaces/command-policy.json (raw)
	if raw, ok, err := l.readRaw(l.join(subdirToolWorkspaces, fileCommandPolicy), fileCommandPolicy); err != nil {
		return nil, err
	} else if ok {
		cfg.CommandPolicyRaw = raw
	}

	// tool-workspaces/hooks-policy.json (raw)
	if raw, ok, err := l.readRaw(l.join(subdirToolWorkspaces, fileHooksPolicy), fileHooksPolicy); err != nil {
		return nil, err
	} else if ok {
		cfg.HooksPolicyRaw = raw
	}

	// inbound-routes.json -> map[string]json.RawMessage
	if m, ok, err := l.readRawMap(l.join(fileInboundRoutes), fileInboundRoutes); err != nil {
		return nil, err
	} else if ok {
		cfg.InboundRoutes = m
	}

	// mcp-presets.json -> map[string]string
	if m, ok, err := l.readStringMap(l.join(fileMCPPresets), fileMCPPresets); err != nil {
		return nil, err
	} else if ok {
		cfg.MCPPresets = m
	}

	// .turns/preferred-chat.json {"providerId","modelId"}
	if pid, mid, ok, err := l.readPreferredChat(l.join(subdirTurns, filePreferredChat), filePreferredChat); err != nil {
		return nil, err
	} else if ok {
		cfg.PreferredChatProviderID = pid
		cfg.PreferredChatModel = mid
	}

	l.mu.Lock()
	l.snapshot = cfg
	l.mu.Unlock()
	return cfg.clone(), nil
}

// Snapshot returns the most recent Load result. If nothing has been loaded yet
// it triggers a single Load; if that Load fails it returns an empty config so
// callers always get a non-nil pointer.
func (l *Loader) Snapshot() *AppConfig {
	l.mu.RLock()
	snap := l.snapshot
	l.mu.RUnlock()
	if snap != nil {
		return snap.clone()
	}
	if cfg, err := l.Load(); err == nil {
		return cfg
	}
	return &AppConfig{}
}

// Reload re-reads all sidecars and, on success, broadcasts a change
// notification (non-blocking) to every subscriber.
func (l *Loader) Reload() (*AppConfig, error) {
	cfg, err := l.Load()
	if err != nil {
		return nil, err
	}
	l.broadcast()
	return cfg, nil
}

// Subscribe returns a buffered-of-1 notification channel. Reload performs a
// non-blocking send; if a pending signal is already queued the new one is
// dropped (coalesced), so a slow consumer never blocks Reload.
func (l *Loader) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	l.subs = append(l.subs, ch)
	l.mu.Unlock()
	return ch
}

func (l *Loader) broadcast() {
	l.mu.RLock()
	subs := make([]chan struct{}, len(l.subs))
	copy(subs, l.subs)
	l.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// --- path helpers ---

func (l *Loader) join(parts ...string) string {
	return filepath.Join(append([]string{l.dataRoot}, parts...)...)
}

// readBytes reads a sidecar. ok=false with nil error means the file is absent.
func readBytes(path string) (data []byte, ok bool, err error) {
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, false, nil
		}
		return nil, false, rerr
	}
	return raw, true, nil
}

func (l *Loader) readPathField(path, name string) (string, bool, error) {
	raw, ok, err := readBytes(path)
	if err != nil || !ok {
		return "", false, err
	}
	var doc struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false, fmt.Errorf("%s: %w", name, err)
	}
	return doc.Path, true, nil
}

func (l *Loader) readOrgID(path, name string) (string, bool, error) {
	raw, ok, err := readBytes(path)
	if err != nil || !ok {
		return "", false, err
	}
	var doc struct {
		OrgID string `json:"orgId"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", false, fmt.Errorf("%s: %w", name, err)
	}
	return doc.OrgID, true, nil
}

func (l *Loader) readStringMap(path, name string) (map[string]string, bool, error) {
	raw, ok, err := readBytes(path)
	if err != nil || !ok {
		return nil, false, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	return m, true, nil
}

func (l *Loader) readRawMap(path, name string) (map[string]json.RawMessage, bool, error) {
	raw, ok, err := readBytes(path)
	if err != nil || !ok {
		return nil, false, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	return m, true, nil
}

func (l *Loader) readRaw(path, name string) (json.RawMessage, bool, error) {
	raw, ok, err := readBytes(path)
	if err != nil || !ok {
		return nil, false, err
	}
	// Validate the bytes are well-formed JSON so a corrupt policy surfaces as
	// a named parse error rather than silently propagating garbage.
	var probe json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	return probe, true, nil
}

func (l *Loader) readPreferredChat(path, name string) (providerID, modelID string, ok bool, err error) {
	raw, present, rerr := readBytes(path)
	if rerr != nil || !present {
		return "", "", false, rerr
	}
	var doc struct {
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
	}
	if uerr := json.Unmarshal(raw, &doc); uerr != nil {
		return "", "", false, fmt.Errorf("%s: %w", name, uerr)
	}
	return doc.ProviderID, doc.ModelID, true, nil
}