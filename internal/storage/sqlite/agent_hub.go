package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lunitide/lunitide/internal/agenthub"
)

var _ agenthub.TaskStore = (*Store)(nil)

func (s *Store) InsertTask(task agenthub.TaskRecord) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO agent_hub_tasks(id,agent,prompt,work_dir,sandbox,status,exit_code,tokens_used,error_msg,created_at,started_at,finished_at,idempotency_key)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, task.ID, task.Agent, task.Prompt, task.WorkDir, nullString(task.Sandbox), task.Status, task.ExitCode, task.TokensUsed, task.ErrorMsg, task.CreatedAt, nullString(task.StartedAt), nullString(task.FinishedAt), task.IdempotencyKey)
	return err
}

func (s *Store) GetTask(id string) (agenthub.TaskRecord, error) {
	return scanHubTask(s.db.QueryRowContext(context.Background(), `SELECT id,agent,prompt,work_dir,COALESCE(sandbox,''),status,exit_code,tokens_used,error_msg,created_at,COALESCE(started_at,''),COALESCE(finished_at,''),idempotency_key FROM agent_hub_tasks WHERE id=?`, id))
}

func (s *Store) GetTaskByKey(key string) (agenthub.TaskRecord, error) {
	return scanHubTask(s.db.QueryRowContext(context.Background(), `SELECT id,agent,prompt,work_dir,COALESCE(sandbox,''),status,exit_code,tokens_used,error_msg,created_at,COALESCE(started_at,''),COALESCE(finished_at,''),idempotency_key FROM agent_hub_tasks WHERE idempotency_key=?`, key))
}

func (s *Store) UpdateTask(task agenthub.TaskRecord) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE agent_hub_tasks SET status=?,exit_code=?,tokens_used=?,error_msg=?,started_at=?,finished_at=? WHERE id=?`,
		task.Status, task.ExitCode, task.TokensUsed, task.ErrorMsg, nullString(task.StartedAt), nullString(task.FinishedAt), task.ID)
	return err
}

func (s *Store) ListTasks(filter agenthub.ListFilter) ([]agenthub.TaskRecord, agenthub.TaskCounts, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id,agent,prompt,work_dir,COALESCE(sandbox,''),status,exit_code,tokens_used,error_msg,created_at,COALESCE(started_at,''),COALESCE(finished_at,''),idempotency_key FROM agent_hub_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, agenthub.TaskCounts{}, err
	}
	defer rows.Close()
	var items []agenthub.TaskRecord
	var counts agenthub.TaskCounts
	for rows.Next() {
		task, scanErr := scanHubTask(rows)
		if scanErr != nil {
			return nil, agenthub.TaskCounts{}, scanErr
		}
		switch task.Status {
		case "queued":
			counts.Queued++
		case "running":
			counts.Running++
		case "success":
			counts.Success++
		case "failed", "timeout":
			counts.Failed++
		}
		if filter.Agent != "" && task.Agent != filter.Agent {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if filter.DateFrom != "" && task.CreatedAt < filter.DateFrom {
			continue
		}
		if filter.DateTo != "" && task.CreatedAt > filter.DateTo+"T23:59:59Z" && !strings.HasPrefix(task.CreatedAt, filter.DateTo) {
			continue
		}
		items = append(items, task)
	}
	return items, counts, rows.Err()
}

func (s *Store) NextEventSeq(taskID string) (int, error) {
	var seq int
	err := s.db.QueryRowContext(context.Background(), `SELECT COALESCE(MAX(seq),0)+1 FROM agent_hub_events WHERE task_id=?`, taskID).Scan(&seq)
	return seq, err
}

func (s *Store) InsertEvent(taskID string, event agenthub.AgentEvent) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO agent_hub_events(task_id,seq,type,title,detail,ts) VALUES(?,?,?,?,?,?)`,
		taskID, event.Seq, event.Type, event.Title, event.Detail, event.TS)
	return err
}

func (s *Store) ListEvents(taskID string) ([]agenthub.AgentEvent, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT seq,type,title,detail,ts FROM agent_hub_events WHERE task_id=? ORDER BY seq`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []agenthub.AgentEvent
	for rows.Next() {
		var ev agenthub.AgentEvent
		if err = rows.Scan(&ev.Seq, &ev.Type, &ev.Title, &ev.Detail, &ev.TS); err != nil {
			return nil, err
		}
		items = append(items, ev)
	}
	return items, rows.Err()
}

func (s *Store) UpsertArtifact(taskID string, art agenthub.Artifact) error {
	_, err := s.db.ExecContext(context.Background(), `INSERT INTO agent_hub_artifacts(task_id,name,path,size,mime,source) VALUES(?,?,?,?,?,?)
ON CONFLICT(task_id,path) DO UPDATE SET name=excluded.name,size=excluded.size,mime=excluded.mime,source=excluded.source`,
		taskID, art.Name, art.Path, art.Size, art.MIME, art.Source)
	return err
}

func (s *Store) ListArtifacts(taskID string) ([]agenthub.Artifact, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT name,path,size,mime,source FROM agent_hub_artifacts WHERE task_id=?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []agenthub.Artifact
	for rows.Next() {
		var art agenthub.Artifact
		if err = rows.Scan(&art.Name, &art.Path, &art.Size, &art.MIME, &art.Source); err != nil {
			return nil, err
		}
		items = append(items, art)
	}
	return items, rows.Err()
}

func (s *Store) ListAllArtifacts(filter agenthub.ListFilter) ([]agenthub.Artifact, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT a.task_id,t.agent,a.name,a.path,a.size,a.mime,a.source,t.created_at
FROM agent_hub_artifacts a JOIN agent_hub_tasks t ON t.id=a.task_id ORDER BY t.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []agenthub.Artifact
	for rows.Next() {
		var art agenthub.Artifact
		if err = rows.Scan(&art.TaskID, &art.Agent, &art.Name, &art.Path, &art.Size, &art.MIME, &art.Source, &art.CreatedAt); err != nil {
			return nil, err
		}
		if filter.Agent != "" && art.Agent != filter.Agent {
			continue
		}
		if filter.Ext != "" && !strings.HasSuffix(strings.ToLower(art.Name), strings.ToLower(filter.Ext)) {
			continue
		}
		items = append(items, art)
	}
	return items, rows.Err()
}

func scanHubTask(row interface{ Scan(...any) error }) (agenthub.TaskRecord, error) {
	var task agenthub.TaskRecord
	var exit sql.NullInt64
	err := row.Scan(&task.ID, &task.Agent, &task.Prompt, &task.WorkDir, &task.Sandbox, &task.Status, &exit, &task.TokensUsed, &task.ErrorMsg, &task.CreatedAt, &task.StartedAt, &task.FinishedAt, &task.IdempotencyKey)
	if err != nil {
		return agenthub.TaskRecord{}, err
	}
	if exit.Valid {
		v := exit.Int64
		task.ExitCode = &v
	}
	return task, nil
}
