package app

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
)

func handleConversationsRootGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.conversations == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "对话存储服务不可用", true)
	}
	status, err := e.conversations.Status()
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", "读取对话存储路径失败", false)
	}
	return bridge.Success(r.ID, status)
}

func handleConversationsRootSet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Path string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || len(strings.TrimSpace(p.Path)) < 1 || len(p.Path) > 1024 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "conversations.root.set 参数无效", false)
	}
	if e.conversations == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "对话存储服务不可用", true)
	}
	migrated, err := e.conversations.SetRoot(p.Path)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "CONVERSATIONS_ROOT_INVALID", err.Error(), false)
	}
	status, _ := e.conversations.Status()
	return bridge.Success(r.ID, struct {
		Path              string `json:"path"`
		Configured        bool   `json:"configured"`
		MigratedSessions  int    `json:"migratedSessions"`
		LegacyPath        string `json:"legacyPath,omitempty"`
	}{status.Path, status.Configured, migrated, status.LegacyPath})
}

func handleSessionFolderGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "session.folder.get 参数无效", false)
	}
	path, err := e.sessionOutputDir(p.SessionID)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "SESSION_FOLDER_UNAVAILABLE", err.Error(), false)
	}
	return bridge.Success(r.ID, map[string]any{"path": path})
}

func handleSessionFolderOpen(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID    string `json:"sessionId"`
		RelativePath string `json:"relativePath"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "session.folder.open 参数无效", false)
	}
	target, err := e.resolveSessionArtifactTarget(p.SessionID, p.RelativePath)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "SESSION_FOLDER_DENIED", "路径无效", false)
	}
	selectFile := strings.TrimSpace(p.RelativePath) != ""
	if err := openInShell(target, selectFile); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "SESSION_FOLDER_OPEN_FAILED", "无法在资源管理器中打开", false)
	}
	return bridge.Success(r.ID, map[string]any{"opened": target})
}

func (e *Engine) resolveSessionArtifactTarget(sessionID, relativePath string) (string, error) {
	if e.tools != nil {
		return e.tools.ResolveSessionArtifact(sessionID, relativePath)
	}
	dir, err := e.sessionOutputDir(sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(relativePath) == "" {
		return dir, nil
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(relativePath)))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("path denied")
	}
	return filepath.Join(dir, rel), nil
}

func (e *Engine) sessionOutputDir(sessionID string) (string, error) {
	if e.tools != nil {
		return e.tools.SessionFolder(sessionID)
	}
	if e.conversations != nil {
		return e.conversations.SessionDir(sessionID)
	}
	return "", errors.New("session folder unavailable")
}

func openInShell(target string, selectFile bool) error {
	if runtime.GOOS != "windows" {
		return exec.Command("xdg-open", target).Start()
	}
	if selectFile {
		return exec.Command("explorer.exe", "/select,", filepath.Clean(target)).Start()
	}
	return exec.Command("explorer.exe", filepath.Clean(target)).Start()
}
