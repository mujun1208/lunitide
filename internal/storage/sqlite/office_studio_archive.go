package sqlite

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
)

var _ officestudio.ArchiveStore = (*Store)(nil)

func (s *Store) ReadOfficeArchiveSnapshot(ctx context.Context, id string) (officestudio.ArchiveSnapshot, error) {
	var out officestudio.ArchiveSnapshot
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", id); err != nil {
		return out, err
	}
	out.Task, err = scanOfficeTask(tx.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, id))
	if err != nil {
		return out, err
	}
	columns := strings.Replace(officeTaskColumns, "title,goal", "'',''", 1)
	columns = strings.Replace(columns, "checkpoint_json", "'{}'", 1)
	rows, err := tx.QueryContext(ctx, `SELECT `+columns+` FROM office_tasks WHERE session_id=? AND owner_org_id=? ORDER BY created_at,id`, out.Task.SessionID, officestudio.Scope(ctx))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		t, scanErr := scanOfficeTask(rows)
		if scanErr != nil {
			rows.Close()
			return out, scanErr
		}
		out.Tasks = append(out.Tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// No list cutoff: an old formally owned version must not be mistaken for
	// an unowned chat file once the session exceeds 1000 versions.
	rows, err = tx.QueryContext(ctx, `SELECT v.id,b.task_id,v.artifact_id,v.sha256,CASE WHEN json_type(m.spec_json,'$.sourcePath')='text' THEN json_extract(m.spec_json,'$.sourcePath') ELSE '' END FROM artifact_versions v JOIN office_version_metadata m ON m.version_id=v.id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id JOIN office_tasks t ON t.id=b.task_id WHERE t.session_id=? AND t.owner_org_id=? ORDER BY v.created_at,v.id`, out.Task.SessionID, officestudio.Scope(ctx))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v officestudio.ArchiveVersion
		if err = rows.Scan(&v.ID, &v.TaskID, &v.ArtifactID, &v.SHA256, &v.SourcePath); err != nil {
			rows.Close()
			return out, err
		}
		out.Versions = append(out.Versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}
