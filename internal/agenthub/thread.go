package agenthub

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
)

type ThreadRecord struct {
	ID              string `json:"threadId"`
	HarnessID       string `json:"harnessId"`
	NativeSessionID string `json:"nativeSessionId"`
	Title           string `json:"title"`
	Pinned          bool   `json:"pinned"`
	WorkspaceRoot   string `json:"workspaceRoot"`
	ExportDir       string `json:"exportDir"`
	Scene           string `json:"scene"`
	Status          string `json:"status"`
	AccessMode      string `json:"accessMode"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type ThreadFilter struct {
	HarnessID string
}

type ThreadMessage struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

type ThreadEvent struct {
	Seq    int    `json:"seq"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	TS     string `json:"ts"`
}

type ThreadFile struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Source string `json:"source"`
}

type ThreadOpenPrompt struct {
	CallID  string               `json:"callId"`
	Prompt  string               `json:"prompt"`
	Options []ThreadPromptOption `json:"options"`
	Status  string               `json:"status"`
}

type ThreadDetail struct {
	Thread     ThreadRecord      `json:"thread"`
	Messages   []ThreadMessage   `json:"messages"`
	Events     []ThreadEvent     `json:"events"`
	Files      []ThreadFile      `json:"files"`
	Prompt     *ThreadOpenPrompt `json:"prompt"`
	TokensUsed int64             `json:"tokensUsed"`
}

type WorkspaceEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

type ThreadCreateRequest struct {
	HarnessID     string `json:"harnessId"`
	Scene         string `json:"scene"`
	WorkspaceRoot string `json:"workspaceRoot"`
	ExportDir     string `json:"exportDir"`
	Title         string `json:"title"`
	AccessMode    string `json:"accessMode"`
}

type ThreadStore struct {
	db *sql.DB
}

func NewThreadStore(db *sql.DB) *ThreadStore {
	return &ThreadStore{db: db}
}

func PathAllowed(workspace, export, candidate string) bool {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	if insideDir(absWorkspace, absCandidate) {
		return true
	}
	if export == "" {
		return false
	}
	absExport, err := filepath.Abs(export)
	if err != nil {
		return false
	}
	return insideDir(absExport, absCandidate)
}

func DefaultThreadDir(root, threadID string) string {
	return filepath.Join(root, "threads", threadID)
}

func (s *ThreadStore) Insert(thread ThreadRecord) error {
	_, err := s.db.Exec(`INSERT INTO agent_hub_threads(
id, harness_id, native_session_id, title, pinned, workspace_root, export_dir, scene, status, access_mode, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		thread.ID, thread.HarnessID, thread.NativeSessionID, thread.Title, boolToInt(thread.Pinned),
		thread.WorkspaceRoot, thread.ExportDir, thread.Scene, thread.Status, thread.AccessMode,
		thread.CreatedAt, thread.UpdatedAt)
	return err
}

func (s *ThreadStore) Get(id string) (ThreadRecord, error) {
	thread, err := scanThread(s.db.QueryRow(`SELECT id, harness_id, native_session_id, title, pinned, workspace_root, export_dir, scene, status, access_mode, created_at, updated_at
FROM agent_hub_threads WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return ThreadRecord{}, ErrNotFound
	}
	return thread, err
}

func (s *ThreadStore) List(filter ThreadFilter) ([]ThreadRecord, error) {
	query := `SELECT id, harness_id, native_session_id, title, pinned, workspace_root, export_dir, scene, status, access_mode, created_at, updated_at
FROM agent_hub_threads`
	var args []any
	if filter.HarnessID != "" {
		query += ` WHERE harness_id=?`
		args = append(args, filter.HarnessID)
	}
	rows, err := s.db.Query(query+` ORDER BY pinned DESC, updated_at DESC, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ThreadRecord
	for rows.Next() {
		thread, scanErr := scanThread(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, thread)
	}
	return items, rows.Err()
}

func (s *ThreadStore) Update(id, title string, pinned bool) error {
	res, err := s.db.Exec(`UPDATE agent_hub_threads SET title=?, pinned=?, updated_at=? WHERE id=?`, title, boolToInt(pinned), threadNow(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *ThreadStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM agent_hub_threads WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanThread(row interface{ Scan(...any) error }) (ThreadRecord, error) {
	var thread ThreadRecord
	var pinned int
	err := row.Scan(&thread.ID, &thread.HarnessID, &thread.NativeSessionID, &thread.Title, &pinned,
		&thread.WorkspaceRoot, &thread.ExportDir, &thread.Scene, &thread.Status, &thread.AccessMode,
		&thread.CreatedAt, &thread.UpdatedAt)
	thread.Pinned = pinned == 1
	return thread, err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *ThreadStore) ListMessages(threadID string) ([]ThreadMessage, error) {
	rows, err := s.db.Query(`SELECT id, seq, role, content, created_at FROM agent_hub_messages WHERE thread_id=? ORDER BY seq`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ThreadMessage{}
	for rows.Next() {
		var item ThreadMessage
		if err = rows.Scan(&item.ID, &item.Seq, &item.Role, &item.Content, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ThreadStore) ListEvents(threadID string) ([]ThreadEvent, error) {
	rows, err := s.db.Query(`SELECT seq, type, title, detail, ts FROM agent_hub_thread_events WHERE thread_id=? ORDER BY seq`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ThreadEvent{}
	for rows.Next() {
		var item ThreadEvent
		if err = rows.Scan(&item.Seq, &item.Type, &item.Title, &item.Detail, &item.TS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *ThreadStore) TokensUsed(threadID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(json_extract(payload_json, '$.tokens')), 0) FROM agent_hub_thread_events WHERE thread_id=?`, threadID).Scan(&n)
	return n, err
}

func (s *ThreadStore) UpsertFile(threadID, rel, abs string, size int64, source string) error {
	rel = filepath.ToSlash(rel)
	_, err := s.db.Exec(`INSERT INTO agent_hub_thread_files(thread_id, rel_path, abs_path, size, source)
VALUES(?,?,?,?,?)
ON CONFLICT(thread_id, rel_path) DO UPDATE SET abs_path=excluded.abs_path, size=excluded.size,
source=CASE WHEN agent_hub_thread_files.source='export' OR excluded.source='export' THEN 'export' ELSE excluded.source END`,
		threadID, rel, abs, size, source)
	return err
}

func (s *ThreadStore) ListFiles(threadID string) ([]ThreadFile, error) {
	rows, err := s.db.Query(`SELECT rel_path, abs_path, size, source FROM agent_hub_thread_files WHERE thread_id=? ORDER BY rel_path`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ThreadFile{}
	for rows.Next() {
		var rel, abs, source string
		var size int64
		if err = rows.Scan(&rel, &abs, &size, &source); err != nil {
			return nil, err
		}
		items = append(items, ThreadFile{Name: filepath.Base(rel), Path: filepath.ToSlash(rel), Size: size, Source: source})
	}
	return items, rows.Err()
}

func (s *ThreadStore) OpenPrompt(threadID string) (*ThreadOpenPrompt, error) {
	var item ThreadOpenPrompt
	var optionsJSON string
	err := s.db.QueryRow(`SELECT call_id, prompt, options_json, status FROM agent_hub_prompts WHERE thread_id=? AND status='open' LIMIT 1`, threadID).
		Scan(&item.CallID, &item.Prompt, &optionsJSON, &item.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if optionsJSON != "" {
		_ = json.Unmarshal([]byte(optionsJSON), &item.Options)
	}
	if item.Options == nil {
		item.Options = []ThreadPromptOption{}
	}
	return &item, nil
}

func (s *ThreadStore) CancelOpenPrompts(threadID string) error {
	_, err := s.db.Exec(`UPDATE agent_hub_prompts SET status='cancelled' WHERE thread_id=? AND status='open'`, threadID)
	return err
}
