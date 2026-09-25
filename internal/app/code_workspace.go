package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/codehost"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type codeHostRuntime struct {
	mu      sync.Mutex
	lsp     map[string]*codehost.Session
	folders map[string]*codehost.Folder
	model   func(context.Context, string, int) (string, error)
}

func (e *Engine) codeRuntime() *codeHostRuntime {
	e.codeHostMu.Lock()
	defer e.codeHostMu.Unlock()
	if e.codeHost == nil {
		e.codeHost = &codeHostRuntime{lsp: map[string]*codehost.Session{}, folders: map[string]*codehost.Folder{}}
	}
	return e.codeHost
}

func (h *codeHostRuntime) session(root string) (*codehost.Session, error) {
	h.mu.Lock()
	if s := h.lsp[root]; s != nil {
		h.mu.Unlock()
		return s, nil
	}
	h.mu.Unlock()
	s, err := codehost.Start(root)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	if old := h.lsp[root]; old != nil {
		h.mu.Unlock()
		s.Close()
		return old, nil
	}
	if h.lsp == nil {
		h.lsp = map[string]*codehost.Session{}
	}
	h.lsp[root] = s
	h.mu.Unlock()
	return s, nil
}

func (h *codeHostRuntime) folder(root string) (*codehost.Folder, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if f := h.folders[root]; f != nil {
		return f, nil
	}
	f, err := codehost.OpenFolder(root)
	if err != nil {
		return nil, err
	}
	if h.folders == nil {
		h.folders = map[string]*codehost.Folder{}
	}
	h.folders[root] = f
	return f, nil
}

func (e *Engine) closeCodeHosts() {
	if e == nil || e.codeHost == nil {
		return
	}
	e.codeHost.mu.Lock()
	defer e.codeHost.mu.Unlock()
	for _, s := range e.codeHost.lsp {
		s.Close()
	}
	e.codeHost.lsp = map[string]*codehost.Session{}
}

func handleCodeWorkspace(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	var p struct {
		Action    string `json:"action"`
		Root      string `json:"root"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
		Content   string `json:"content"`
		Test      string `json:"test"`
		Accept    bool   `json:"accept"`
		SessionID string `json:"sessionId"`
	}
	if decodePayload(request.Payload, &p) != nil || p.Action == "" || p.Root == "" {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "code.workspace 参数无效", false)
	}
	root, err := filepath.Abs(p.Root)
	if err != nil {
		return request.Fail("CODE_ROOT", err.Error(), false)
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		return request.Fail("CODE_ROOT", "代码目录不存在", false)
	}
	if p.SessionID != "" && e.tools != nil {
		_ = e.tools.SetSessionCodeRoot(p.SessionID, root)
	}
	switch p.Action {
	case "diagnostics":
		abs, err := confineCodePath(root, p.Path)
		if err != nil {
			return request.Fail("CODE_PATH", err.Error(), false)
		}
		text := p.Content
		if text == "" {
			body, readErr := os.ReadFile(abs)
			if readErr != nil {
				return request.Fail("CODE_PATH", readErr.Error(), false)
			}
			text = string(body)
		}
		s, err := e.codeRuntime().session(root)
		if err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		if err = s.Open(abs, text); err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		items := s.WaitDiagnostics(abs, 8*time.Second)
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, map[string]any{"path": item.Path, "line": item.Line, "message": item.Message})
		}
		if p.SessionID != "" && len(items) > 0 {
			e.rememberCodeDiagnostic(p.SessionID, fmt.Sprintf("%s:%d: %s", filepath.Base(items[0].Path), items[0].Line, items[0].Message))
		}
		return request.Ok(map[string]any{"ok": true, "diagnostics": out})
	case "definition":
		abs, err := confineCodePath(root, p.Path)
		if err != nil {
			return request.Fail("CODE_PATH", err.Error(), false)
		}
		text := p.Content
		if text == "" {
			body, readErr := os.ReadFile(abs)
			if readErr != nil {
				return request.Fail("CODE_PATH", readErr.Error(), false)
			}
			text = string(body)
		}
		s, err := e.codeRuntime().session(root)
		if err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		if err = s.Open(abs, text); err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		if p.Column < 1 {
			p.Column = 1
		}
		loc, err := s.Definition(abs, p.Line, p.Column)
		if err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "path": loc.Path, "line": loc.Line, "column": loc.Column})
	case "debug":
		abs, err := confineCodePath(root, p.Path)
		if err != nil {
			return request.Fail("CODE_PATH", err.Error(), false)
		}
		if p.Line < 1 {
			return request.Fail("CODE_DEBUG", "需要行号", false)
		}
		stopped, err := codehost.StopOnLine(root, abs, p.Line, p.Test)
		if err != nil {
			return request.Fail("CODE_DEBUG", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "stopped": stopped, "line": stopped, "path": abs})
	case "complete":
		abs, err := confineCodePath(root, p.Path)
		if err != nil {
			return request.Fail("CODE_PATH", err.Error(), false)
		}
		text := p.Content
		if text == "" {
			body, readErr := os.ReadFile(abs)
			if readErr != nil {
				return request.Fail("CODE_PATH", readErr.Error(), false)
			}
			text = string(body)
		}
		suggestion, source := e.suggestCodeLine(ctx, text, p.Line)
		if p.Accept && suggestion != "" {
			if err = codehost.WriteSourceLine(abs, p.Line, suggestion); err != nil {
				return request.Fail("CODE_WRITE", err.Error(), false)
			}
		}
		return request.Ok(map[string]any{"ok": true, "suggestion": suggestion, "source": source, "line": p.Line})
	case "edit":
		folder, err := e.codeRuntime().folder(root)
		if err != nil {
			return request.Fail("CODE_ROOT", err.Error(), false)
		}
		if err = folder.Write(p.Path, p.Content); err != nil {
			return request.Fail("CODE_WRITE", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "diff": folder.Diffs()})
	case "diff":
		folder, err := e.codeRuntime().folder(root)
		if err != nil {
			return request.Fail("CODE_ROOT", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "diff": folder.Diffs()})
	case "accept":
		folder, err := e.codeRuntime().folder(root)
		if err != nil {
			return request.Fail("CODE_ROOT", err.Error(), false)
		}
		n, err := e.sessionCheckpoint(ctx, p.SessionID, "workspace.accept")
		if err != nil {
			return request.Fail("CODE_ACCEPT", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "accepted": n + folder.Accept()})
	case "references":
		abs, err := confineCodePath(root, p.Path)
		if err != nil {
			return request.Fail("CODE_PATH", err.Error(), false)
		}
		text := p.Content
		if text == "" {
			body, readErr := os.ReadFile(abs)
			if readErr != nil {
				return request.Fail("CODE_PATH", readErr.Error(), false)
			}
			text = string(body)
		}
		s, err := e.codeRuntime().session(root)
		if err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		if err = s.Open(abs, text); err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		if p.Column < 1 {
			p.Column = 1
		}
		refs, err := s.References(abs, p.Line, p.Column)
		if err != nil {
			return request.Fail("CODE_LSP", err.Error(), false)
		}
		out := make([]map[string]any, 0, len(refs))
		for _, ref := range refs {
			out = append(out, map[string]any{"path": ref.Path, "line": ref.Line})
		}
		return request.Ok(map[string]any{"ok": true, "references": out})
	case "restore":
		folder, err := e.codeRuntime().folder(root)
		if err != nil {
			return request.Fail("CODE_ROOT", err.Error(), false)
		}
		n, err := e.sessionCheckpoint(ctx, p.SessionID, "workspace.restore")
		if err != nil {
			return request.Fail("CODE_RESTORE", err.Error(), false)
		}
		folderN, err := folder.Restore()
		if err != nil {
			return request.Fail("CODE_RESTORE", err.Error(), false)
		}
		return request.Ok(map[string]any{"ok": true, "restored": n + folderN})
	default:
		return request.Fail("BRIDGE_SCHEMA_INVALID", "code.workspace 参数无效", false)
	}
}

func (e *Engine) sessionCheckpoint(ctx context.Context, session, action string) (int, error) {
	if e == nil || e.tools == nil || session == "" {
		return 0, nil
	}
	out, err := e.tools.Execute(ctx, toolruntime.FullAccess, session, action, []byte(`{}`), true)
	if err != nil {
		if action == "workspace.restore" && strings.Contains(err.Error(), "nothing to restore") {
			return 0, nil
		}
		return 0, err
	}
	fields := strings.Fields(out.Output)
	if len(fields) < 2 {
		return 0, nil
	}
	n, _ := strconv.Atoi(fields[1])
	return n, nil
}

func (e *Engine) suggestCodeLine(ctx context.Context, content string, line int) (string, string) {
	h := e.codeRuntime()
	h.mu.Lock()
	model := h.model
	h.mu.Unlock()
	if model != nil {
		if got, err := model(ctx, content, line); err == nil {
			if one := oneCodeLine(got); one != "" {
				return one, "model"
			}
		}
	} else if got, err := e.completeWithPreferredModel(ctx, content, line); err == nil {
		if one := oneCodeLine(got); one != "" {
			return one, "model"
		}
	}
	if line >= 1 {
		if local, ok := toolruntime.CompleteSourceLine(content, line-1); ok {
			return local, "local"
		}
	}
	return "", ""
}

func (e *Engine) completeWithPreferredModel(ctx context.Context, content string, line int) (string, error) {
	if e == nil || e.providers == nil {
		return "", fmt.Errorf("no model")
	}
	pref := e.loadPreferredChatModel()
	if pref.ProviderID == "" || pref.ModelID == "" {
		return "", fmt.Errorf("no model")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	var hit provider.Provider
	found := false
	for _, item := range items {
		if item.ID != pref.ProviderID {
			continue
		}
		for _, m := range item.Models {
			if m.ModelID == pref.ModelID {
				hit = item
				found = true
				break
			}
		}
	}
	if !found || hit.CredentialRef == "" {
		return "", fmt.Errorf("no model")
	}
	var raw string
	err = e.withProviderLease(ctx, hit, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		a, adapterErr := e.adapter(op, hit)
		if adapterErr != nil {
			return adapterErr
		}
		resp, completeErr := a.Complete(op, secret, llmadapter.Request{
			Model: pref.ModelID, MaxTokens: 80, MaxAttempts: 1, DisableReasoning: true,
			Messages: []llmadapter.Message{
				{Role: llmadapter.RoleSystem, Content: "只返回光标所在行的完整文本。不要解释，不要代码块。"},
				{Role: llmadapter.RoleUser, Content: fmt.Sprintf("行号 %d\n%s", line, content)},
			},
		})
		if completeErr != nil {
			return completeErr
		}
		raw = resp.Message.Content
		return nil
	})
	return raw, err
}

func oneCodeLine(s string) string {
	s = strings.Trim(s, "\r\n ")
	s = strings.TrimPrefix(s, "```go")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.Trim(s, "\r\n")
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

func confineCodePath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("path required")
	}
	if filepath.IsAbs(rel) {
		next, err := filepath.Rel(root, rel)
		if err != nil {
			return "", err
		}
		rel = next
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal")
	}
	return filepath.Join(root, clean), nil
}
