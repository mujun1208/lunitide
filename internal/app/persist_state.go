package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/imapp"
)

const (
	persistInboundFile = "inbound-routes.json"
	persistMCPFile     = "mcp-presets.json"
)

type persistedInboundRoute struct {
	Kind           string `json:"kind"`
	Sender         string `json:"sender"`
	ConversationID string `json:"conversationId"`
}

// SetPersistDir loads I4 reply routes and MCP preset ids from the engine
// data root so they survive process restart.
func (e *Engine) SetPersistDir(dir string) {
	if e == nil {
		return
	}
	e.persistDir = strings.TrimSpace(dir)
	e.loadPersistedState()
}

func (e *Engine) persistPath(name string) string {
	if e == nil || e.persistDir == "" || name == "" {
		return ""
	}
	return filepath.Join(e.persistDir, name)
}

func (e *Engine) loadPersistedState() {
	if e == nil || e.persistDir == "" {
		return
	}
	e.loadInboundRoutes()
	e.loadMcpPresets()
}

func (e *Engine) loadInboundRoutes() {
	path := e.persistPath(persistInboundFile)
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var rows map[string]persistedInboundRoute
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	for sessionID, row := range rows {
		sessionID = strings.TrimSpace(sessionID)
		kind, err := imapp.ParseKind(row.Kind)
		if err != nil || sessionID == "" || row.Sender == "" {
			continue
		}
		e.inboundRoutes.Store(sessionID, inboundRoute{
			Kind:           kind,
			Sender:         strings.TrimSpace(row.Sender),
			ConversationID: strings.TrimSpace(row.ConversationID),
		})
	}
}

func (e *Engine) saveInboundRoutes() {
	if e == nil || e.persistDir == "" {
		return
	}
	e.persistMu.Lock()
	defer e.persistMu.Unlock()
	rows := map[string]persistedInboundRoute{}
	e.inboundRoutes.Range(func(key, value any) bool {
		sessionID, _ := key.(string)
		route, ok := value.(inboundRoute)
		if !ok || sessionID == "" || route.Sender == "" {
			return true
		}
		rows[sessionID] = persistedInboundRoute{
			Kind:           string(route.Kind),
			Sender:         route.Sender,
			ConversationID: route.ConversationID,
		}
		return true
	})
	_ = writePersistJSON(e.persistPath(persistInboundFile), rows)
}

func (e *Engine) loadMcpPresets() {
	path := e.persistPath(persistMCPFile)
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var rows map[string]string
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	for endpointID, presetID := range rows {
		endpointID = strings.TrimSpace(endpointID)
		presetID = strings.TrimSpace(presetID)
		if endpointID == "" || presetID == "" {
			continue
		}
		e.mcpPresetByEP.Store(endpointID, presetID)
	}
}

func (e *Engine) saveMcpPresets() {
	if e == nil || e.persistDir == "" {
		return
	}
	e.persistMu.Lock()
	defer e.persistMu.Unlock()
	rows := map[string]string{}
	e.mcpPresetByEP.Range(func(key, value any) bool {
		endpointID, _ := key.(string)
		presetID, _ := value.(string)
		if endpointID == "" || presetID == "" {
			return true
		}
		rows[endpointID] = presetID
		return true
	})
	_ = writePersistJSON(e.persistPath(persistMCPFile), rows)
}

func writePersistJSON(path string, value any) error {
	if path == "" {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
