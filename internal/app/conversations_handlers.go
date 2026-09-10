package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
)

func handleConversationsRootGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.conversations == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "对话存储服务不可用", true)
	}
	status, err := e.conversations.Status()
	if err != nil {
		return r.Fail("INTERNAL_ERROR", "读取对话存储路径失败", false)
	}
	return r.Ok(status)
}

func handleConversationsRootSet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Path string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || len(strings.TrimSpace(p.Path)) < 1 || len(p.Path) > 1024 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "conversations.root.set 参数无效", false)
	}
	if e.conversations == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "对话存储服务不可用", true)
	}
	migrated, err := e.conversations.SetRoot(p.Path)
	if err != nil {
		return r.Fail("CONVERSATIONS_ROOT_INVALID", conversationsRootMessage(err), false)
	}
	status, _ := e.conversations.Status()
	return r.Ok(struct {
		Path             string `json:"path"`
		Configured       bool   `json:"configured"`
		MigratedSessions int    `json:"migratedSessions"`
		LegacyPath       string `json:"legacyPath,omitempty"`
	}{status.Path, status.Configured, migrated, status.LegacyPath})
}

func handleSessionFolderGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.folder.get 参数无效", false)
	}
	path, err := e.sessionOutputDir(p.SessionID)
	if err != nil {
		return r.Fail("SESSION_FOLDER_UNAVAILABLE", "会话目录暂时不可用", false)
	}
	return r.Ok(map[string]any{"path": path})
}

func handleSessionFolderList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID    string `json:"sessionId"`
		RelativePath string `json:"relativePath"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.folder.list 参数无效", false)
	}
	dir, err := e.resolveSessionArtifactTarget(p.SessionID, p.RelativePath)
	if err != nil {
		return r.Fail("SESSION_FOLDER_DENIED", "路径无效", false)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return r.Fail("SESSION_FOLDER_UNAVAILABLE", "无法读取目录", false)
	}
	type item struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		Directory bool   `json:"directory"`
	}
	prefix := strings.Trim(strings.ReplaceAll(p.RelativePath, `\`, `/`), "/")
	out := make([]item, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}
		out = append(out, item{Name: name, Path: rel, Directory: ent.IsDir()})
	}
	return r.Ok(map[string]any{"items": out})
}

func handleSessionFolderOpen(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID    string `json:"sessionId"`
		RelativePath string `json:"relativePath"`
		Reveal       bool   `json:"reveal"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "session.folder.open 参数无效", false)
	}
	target, err := e.resolveSessionArtifactTarget(p.SessionID, p.RelativePath)
	if err != nil {
		return r.Fail("SESSION_FOLDER_DENIED", "路径无效", false)
	}
	selectFile := strings.TrimSpace(p.RelativePath) != ""
	if err := openArtifactTarget(target, selectFile, p.Reveal); err != nil {
		return r.Fail("SESSION_FOLDER_OPEN_FAILED", "无法打开文件", false)
	}
	return r.Ok(map[string]any{"opened": target})
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
	return "", errors.New("会话目录暂时不可用")
}

func conversationsRootMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "empty") || strings.Contains(msg, "不能为空"):
		return "对话存储路径不能为空"
	default:
		return "对话存储路径必须是存在的本地目录"
	}
}

var openArtifactTarget = openArtifactInShell

func openArtifactInShell(target string, file, reveal bool) error {
	if reveal {
		return openInShell(filepath.Dir(target), false)
	}
	return openInShell(target, file)
}

func openInShell(target string, _ bool) error { return openLocalArtifactPath(filepath.Clean(target)) }
