package agenthub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ErrNoOpenPrompt   = errors.New("no open prompt")
	ErrCallIDMismatch = errors.New("call id mismatch")
)

func (s *Service) CreateThread(req ThreadCreateRequest) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	if !validThreadHarness(req.HarnessID) || !validThreadScene(req.Scene) {
		return ThreadDetail{}, fmt.Errorf("参数无效")
	}
	access := req.AccessMode
	if access == "" {
		access = "approval"
	}
	if !validThreadAccess(access) {
		return ThreadDetail{}, fmt.Errorf("参数无效")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "新会话"
	}
	id := ulid.Make().String()
	workspace := strings.TrimSpace(req.WorkspaceRoot)
	if workspace == "" {
		workspace = DefaultThreadDir(s.Root, id)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return ThreadDetail{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	thread := ThreadRecord{
		ID:            id,
		HarnessID:     req.HarnessID,
		Title:         title,
		WorkspaceRoot: workspace,
		ExportDir:     strings.TrimSpace(req.ExportDir),
		Scene:         req.Scene,
		Status:        "idle",
		AccessMode:    access,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Threads.Insert(thread); err != nil {
		return ThreadDetail{}, err
	}
	if adapter, err := s.threadAdapter(req.HarnessID); err == nil {
		_ = adapter.Open(thread)
	}
	return s.GetThread(id)
}

func (s *Service) GetThread(id string) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	messages, err := s.Threads.ListMessages(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	events, err := s.Threads.ListEvents(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	files, err := s.Threads.ListFiles(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	files = mergeThreadFiles(files, scanThreadWorkspace(thread))
	prompt, err := s.Threads.OpenPrompt(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	return ThreadDetail{Thread: thread, Messages: messages, Events: events, Files: files, Prompt: prompt}, nil
}

func (s *Service) ListThreads(harnessID string) ([]ThreadRecord, error) {
	if s.Threads == nil {
		return nil, fmt.Errorf("thread store unavailable")
	}
	items, err := s.Threads.List(ThreadFilter{HarnessID: harnessID})
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ThreadRecord{}
	}
	return items, nil
}

func (s *Service) UpdateThread(id, title string, pinned *bool) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		thread.Title = trimmed
	}
	if pinned != nil {
		thread.Pinned = *pinned
	}
	if err = s.Threads.Update(id, thread.Title, thread.Pinned); err != nil {
		return ThreadDetail{}, err
	}
	return s.GetThread(id)
}

func (s *Service) CancelThread(id string) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	if adapter, err := s.threadAdapter(thread.HarnessID); err == nil {
		_ = adapter.Close(id)
	}
	if err = s.Threads.CancelOpenPrompts(id); err != nil {
		return ThreadDetail{}, err
	}
	if err = setThreadStatus(s.Threads, id, "cancelled"); err != nil {
		return ThreadDetail{}, err
	}
	return s.GetThread(id)
}

func (s *Service) PromptThread(id, text string) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	adapter, err := s.threadAdapter(thread.HarnessID)
	if err != nil {
		return ThreadDetail{}, err
	}
	if err = adapter.Prompt(id, text); err != nil {
		return ThreadDetail{}, err
	}
	return s.GetThread(id)
}

func (s *Service) RespondThread(id, callID, optionID string) (ThreadDetail, error) {
	if s.Threads == nil {
		return ThreadDetail{}, fmt.Errorf("thread store unavailable")
	}
	detail, err := s.GetThread(id)
	if err != nil {
		return ThreadDetail{}, err
	}
	if detail.Prompt == nil || detail.Prompt.CallID == "" {
		return ThreadDetail{}, ErrNoOpenPrompt
	}
	if callID != detail.Prompt.CallID {
		return ThreadDetail{}, ErrCallIDMismatch
	}
	adapter, err := s.threadAdapter(detail.Thread.HarnessID)
	if err != nil {
		return ThreadDetail{}, err
	}
	if err = adapter.Respond(id, detail.Prompt.CallID, optionID); err != nil {
		return ThreadDetail{}, err
	}
	return s.GetThread(id)
}

func (s *Service) ResolveThreadFile(threadID, rel string) (string, error) {
	return s.threadPath(threadID, rel, false)
}

func (s *Service) OpenThreadPath(threadID, rel string) (string, error) {
	return s.threadPath(threadID, rel, true)
}

func (s *Service) ListWorkspace(threadID, relativePath string) ([]WorkspaceEntry, error) {
	if s.Threads == nil {
		return nil, fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(threadID)
	if err != nil {
		return nil, err
	}
	target := thread.WorkspaceRoot
	rel := strings.TrimSpace(relativePath)
	if rel != "" {
		target = joinThreadPath(thread.WorkspaceRoot, rel)
	}
	if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, target) {
		return nil, ErrPathOutside
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []WorkspaceEntry{{Name: info.Name(), Path: slashRel(thread.WorkspaceRoot, target), Size: info.Size()}}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	items := []WorkspaceEntry{}
	for _, entry := range entries {
		abs := filepath.Join(target, entry.Name())
		if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, abs) {
			continue
		}
		item := WorkspaceEntry{Name: entry.Name(), Path: slashRel(thread.WorkspaceRoot, abs), IsDir: entry.IsDir()}
		if info, infoErr := entry.Info(); infoErr == nil {
			item.Size = info.Size()
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) threadPath(threadID, rel string, allowRoot bool) (string, error) {
	if s.Threads == nil {
		return "", fmt.Errorf("thread store unavailable")
	}
	thread, err := s.Threads.Get(threadID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(rel) == "" {
		if allowRoot {
			return thread.WorkspaceRoot, nil
		}
		return "", fmt.Errorf("参数无效")
	}
	abs := joinThreadPath(thread.WorkspaceRoot, rel)
	if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, abs) {
		return "", ErrPathOutside
	}
	return abs, nil
}

func (s *Service) threadAdapter(harness string) (ThreadAdapter, error) {
	if s.Threads == nil {
		return nil, fmt.Errorf("thread store unavailable")
	}
	if harness == "loopback" {
		return NewLoopbackAdapter(s.Threads), nil
	}
	if harness == "cursor" {
		return NewCursorACP(s.Threads), nil
	}
	if harness == "codex" {
		return NewCodexThread(s.Threads), nil
	}
	return nil, fmt.Errorf("%w: 该 Agent 尚未接入会话", ErrNotAvailable)
}

func joinThreadPath(workspace, rel string) string {
	clean := filepath.Clean(strings.ReplaceAll(rel, "/", string(filepath.Separator)))
	if filepath.IsAbs(clean) {
		return clean
	}
	return filepath.Join(workspace, clean)
}

func slashRel(root, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return filepath.ToSlash(filepath.Base(target))
	}
	return filepath.ToSlash(rel)
}

func scanThreadWorkspace(thread ThreadRecord) []ThreadFile {
	entries, err := os.ReadDir(thread.WorkspaceRoot)
	if err != nil {
		return nil
	}
	var out []ThreadFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		abs := filepath.Join(thread.WorkspaceRoot, entry.Name())
		if !PathAllowed(thread.WorkspaceRoot, thread.ExportDir, abs) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		out = append(out, ThreadFile{Name: entry.Name(), Path: filepath.ToSlash(entry.Name()), Size: info.Size(), Source: "scan"})
	}
	return out
}

func mergeThreadFiles(stored, live []ThreadFile) []ThreadFile {
	seen := map[string]ThreadFile{}
	for _, file := range stored {
		seen[filepath.ToSlash(file.Path)] = file
	}
	for _, file := range live {
		key := filepath.ToSlash(file.Path)
		if _, ok := seen[key]; !ok {
			seen[key] = file
		}
	}
	out := make([]ThreadFile, 0, len(seen))
	for _, file := range seen {
		out = append(out, file)
	}
	if out == nil {
		out = []ThreadFile{}
	}
	return out
}

func validThreadHarness(id string) bool {
	if len(id) < 1 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validThreadScene(scene string) bool {
	switch scene {
	case "write_project", "fix", "ppt", "free":
		return true
	default:
		return false
	}
}

func validThreadAccess(mode string) bool {
	switch mode {
	case "approval", "auto-edit", "full-access":
		return true
	default:
		return false
	}
}
