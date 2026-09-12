package agenthub

import (
	"database/sql"
	"path/filepath"
)

type ThreadRecord struct {
	ID              string
	HarnessID       string
	NativeSessionID string
	Title           string
	Pinned          bool
	WorkspaceRoot   string
	ExportDir       string
	Scene           string
	Status          string
	AccessMode      string
	CreatedAt       string
	UpdatedAt       string
}

type ThreadFilter struct {
	HarnessID string
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
	res, err := s.db.Exec(`UPDATE agent_hub_threads SET title=?, pinned=? WHERE id=?`, title, boolToInt(pinned), id)
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
