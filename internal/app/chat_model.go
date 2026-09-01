package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

type preferredChatModel struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

func resolveChatModel(items []provider.Provider, preferProviderID, preferModelID string) (provider.CatalogEntry, bool) {
	catalog := provider.CatalogForKind(items, provider.KindLLM)
	if len(catalog) == 0 {
		return provider.CatalogEntry{}, false
	}
	preferProviderID = strings.TrimSpace(preferProviderID)
	preferModelID = strings.TrimSpace(preferModelID)
	if preferProviderID != "" && preferModelID != "" {
		for _, entry := range catalog {
			if entry.Provider.ID == preferProviderID && entry.Model.ModelID == preferModelID {
				return entry, true
			}
		}
	}
	if preferModelID != "" {
		for _, entry := range catalog {
			if entry.Model.ModelID == preferModelID {
				return entry, true
			}
		}
	}
	return catalog[0], true
}

func (e *Engine) preferredChatPath() string {
	if e == nil || e.tools == nil {
		return ""
	}
	root := strings.TrimSpace(e.tools.WorkspaceRoot())
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".turns", "preferred-chat.json")
}

func (e *Engine) loadPreferredChatModel() preferredChatModel {
	if e == nil {
		return preferredChatModel{}
	}
	if stored, ok := e.preferredChat.Load().(preferredChatModel); ok {
		if strings.TrimSpace(stored.ProviderID) != "" && strings.TrimSpace(stored.ModelID) != "" {
			return stored
		}
	}
	path := e.preferredChatPath()
	if path == "" {
		return preferredChatModel{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return preferredChatModel{}
	}
	var pref preferredChatModel
	if json.Unmarshal(raw, &pref) != nil {
		return preferredChatModel{}
	}
	pref.ProviderID = strings.TrimSpace(pref.ProviderID)
	pref.ModelID = strings.TrimSpace(pref.ModelID)
	if pref.ProviderID == "" || pref.ModelID == "" {
		return preferredChatModel{}
	}
	e.preferredChat.Store(pref)
	return pref
}

func (e *Engine) persistPreferredChatModel(pref preferredChatModel) {
	path := e.preferredChatPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	raw, err := json.Marshal(pref)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func (e *Engine) resolvePreferredChatModel(items []provider.Provider) (provider.CatalogEntry, bool) {
	var pref preferredChatModel
	if e != nil {
		pref = e.loadPreferredChatModel()
	}
	return resolveChatModel(items, pref.ProviderID, pref.ModelID)
}

func handleChatPrefer(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	_ = ctx
	var p struct {
		ProviderID string `json:"providerId"`
		ModelID    string `json:"modelId"`
	}
	if decodePayload(request.Payload, &p) != nil || !ulidValid(p.ProviderID) || len(p.ModelID) < 1 || len(p.ModelID) > 128 {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "chat.prefer 参数无效", false)
	}
	e.rememberChatModel(p.ProviderID, p.ModelID)
	return request.Ok(map[string]any{"providerId": p.ProviderID, "modelId": p.ModelID})
}

func (e *Engine) rememberChatModel(providerID, modelID string) {
	if e == nil {
		return
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if providerID == "" || modelID == "" {
		return
	}
	pref := preferredChatModel{ProviderID: providerID, ModelID: modelID}
	e.preferredChat.Store(pref)
	e.persistPreferredChatModel(pref)
}
