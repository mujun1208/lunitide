package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
)

// SessionArtifact is durable metadata for a chat output card (file bytes
// live in the session folder or Desktop via desktop/ prefix).
type SessionArtifact struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	CallID   string `json:"callId"`
	ToolName string `json:"toolName"`
}

type sessionArtifactsDoc struct {
	Messages map[string][]SessionArtifact `json:"messages"`
}

func (e *Engine) sessionArtifactsPath(sessionID string) string {
	if e == nil || e.tools == nil || sessionID == "" {
		return ""
	}
	dir, err := e.tools.SessionFolder(sessionID)
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, ".message-artifacts.json")
}

func loadSessionArtifactsDoc(path string) sessionArtifactsDoc {
	raw, err := os.ReadFile(path)
	if err != nil {
		return sessionArtifactsDoc{Messages: map[string][]SessionArtifact{}}
	}
	var doc sessionArtifactsDoc
	if json.Unmarshal(raw, &doc) != nil || doc.Messages == nil {
		return sessionArtifactsDoc{Messages: map[string][]SessionArtifact{}}
	}
	return doc
}

func saveSessionArtifactsDoc(path string, doc sessionArtifactsDoc) error {
	if doc.Messages == nil {
		doc.Messages = map[string][]SessionArtifact{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

// chatDeliverableArtifact decides whether a tool output should appear as a
// user-facing chat deliverable card. Intermediate web.search / web.fetch HTML
// (search.html, fetch.html) stays in the workspace browser only.
func chatDeliverableArtifact(toolName, kind, path string) bool {
	switch toolName {
	case "web.search", "web.fetch":
		return false
	case "pptx.gen", "docx.gen", "excel.gen", "pdf.gen", "html.gen":
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	if kind == "html" && (base == "search.html" || base == "fetch.html") {
		return false
	}
	if kind == "image" {
		return true
	}
	if kind == "html" && toolName == "workspace.write" {
		return true
	}
	return false
}

func normalizeSessionArtifact(a SessionArtifact) (SessionArtifact, bool) {
	a.Kind = strings.TrimSpace(a.Kind)
	a.Path = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(strings.TrimSpace(a.Path), "\\", "/")))
	a.CallID = strings.TrimSpace(a.CallID)
	a.ToolName = strings.TrimSpace(a.ToolName)
	if !artifactKindValid(a.Kind) || a.Path == "" || len(a.Path) > 512 ||
		strings.HasPrefix(a.Path, "/") || strings.Contains(a.Path, "..") ||
		a.CallID == "" || len(a.CallID) > 128 || a.ToolName == "" {
		return SessionArtifact{}, false
	}
	return a, true
}

func (e *Engine) loadSessionArtifactsByMessage(sessionID string) map[string][]SessionArtifact {
	path := e.sessionArtifactsPath(sessionID)
	if path == "" {
		return nil
	}
	doc := loadSessionArtifactsDoc(path)
	if len(doc.Messages) == 0 {
		return nil
	}
	out := make(map[string][]SessionArtifact, len(doc.Messages))
	for id, items := range doc.Messages {
		if !message.CanonicalULID(id) || len(items) == 0 {
			continue
		}
		clean := make([]SessionArtifact, 0, len(items))
		for _, item := range items {
			if norm, ok := normalizeSessionArtifact(item); ok && chatDeliverableArtifact(norm.ToolName, norm.Kind, norm.Path) {
				clean = append(clean, norm)
			}
		}
		if len(clean) > 0 {
			out[id] = clean
		}
	}
	return out
}

func (e *Engine) appendMessageArtifacts(sessionID, messageID string, artifacts []SessionArtifact) {
	if sessionID == "" || messageID == "" || len(artifacts) == 0 {
		return
	}
	path := e.sessionArtifactsPath(sessionID)
	if path == "" {
		return
	}
	clean := make([]SessionArtifact, 0, len(artifacts))
	for _, item := range artifacts {
		if norm, ok := normalizeSessionArtifact(item); ok && chatDeliverableArtifact(norm.ToolName, norm.Kind, norm.Path) {
			clean = append(clean, norm)
		}
	}
	if len(clean) == 0 {
		return
	}
	doc := loadSessionArtifactsDoc(path)
	doc.Messages[messageID] = clean
	_ = saveSessionArtifactsDoc(path, doc)
}

func enrichMessageListPage(page any, artifacts map[string][]SessionArtifact) map[string]any {
	raw, err := json.Marshal(page)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{}
	}
	items, ok := out["items"].([]any)
	if !ok || len(artifacts) == 0 {
		return out
	}
	for i, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := row["role"].(string)
		id, _ := row["id"].(string)
		if role != string(message.RoleAssistant) {
			continue
		}
		if arts, ok := artifacts[id]; ok && len(arts) > 0 {
			row["artifacts"] = arts
			items[i] = row
		}
	}
	out["items"] = items
	return out
}

// sessionArtifactFromTool builds a persisted card record from a tool result.
func sessionArtifactFromTool(callID, toolName, kind, path string) SessionArtifact {
	return SessionArtifact{
		Kind:     kind,
		Path:     path,
		CallID:   callID,
		ToolName: toolName,
	}
}

func sessionArtifactsUpdatedAt(path string) string {
	if path == "" {
		return ""
	}
	if st, err := os.Stat(path); err == nil {
		return st.ModTime().UTC().Format(time.RFC3339)
	}
	return ""
}
