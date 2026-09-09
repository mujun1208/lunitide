package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
)

var _ officestudio.SnapshotStore = (*Store)(nil)

func officeSnapshotText(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit], true
}

// One read transaction makes heads, quality and evidence a consistent view.
// Unlike the compatibility list methods this has no silent 1000-row cutoff.
func (s *Store) ReadOfficeSnapshot(ctx context.Context, id string) (officestudio.Snapshot, error) {
	out := officestudio.Snapshot{Checks: map[string][]officestudio.SnapshotCheck{}, ValidationIDs: map[string]string{}}
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
	rows, err := tx.QueryContext(ctx, `SELECT b.task_id,h.artifact_id,h.latest_version_id,COALESCE(h.accepted_version_id,''),h.revision FROM office_artifact_heads h JOIN office_task_artifacts b ON b.artifact_id=h.artifact_id WHERE b.task_id=? ORDER BY h.artifact_id`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var h officestudio.Head
		if err = rows.Scan(&h.TaskID, &h.ArtifactID, &h.LatestVersionID, &h.AcceptedVersionID, &h.Revision); err != nil {
			rows.Close()
			return out, err
		}
		out.Heads = append(out.Heads, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// Neither spec_json nor index_json belongs in a history overview.
	columns := strings.Replace(officeVersionColumns, "m.spec_json,m.index_json", "'{}','{}'", 1)
	rows, err = tx.QueryContext(ctx, `SELECT `+columns+officeVersionFrom+` WHERE b.task_id=? ORDER BY v.artifact_id,v.version_no DESC,v.id DESC`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanOfficeVersion(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Versions = append(out.Versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT q.id,q.version_id,q.checks_json FROM office_validation_runs q JOIN artifact_versions v ON v.id=q.version_id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id WHERE b.task_id=? AND q.id=(SELECT latest.id FROM office_validation_runs latest WHERE latest.version_id=q.version_id ORDER BY latest.created_at DESC,latest.id DESC LIMIT 1) ORDER BY q.version_id`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var qaID, versionID, raw string
		if err = rows.Scan(&qaID, &versionID, &raw); err != nil {
			rows.Close()
			return out, err
		}
		var checks []officestudio.Check
		if err = json.Unmarshal([]byte(raw), &checks); err != nil {
			rows.Close()
			return out, err
		}
		out.ValidationIDs[versionID] = qaID
		for _, c := range checks {
			detail, truncated := officeSnapshotText(c.Detail, 512)
			c.Detail = detail
			out.Checks[versionID] = append(out.Checks[versionID], officestudio.SnapshotCheck{Check: c, DetailTruncated: truncated})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	// SQL bounds each raw record before it reaches Go; flags preserve that
	// these are excerpts. IDs still identify immutable complete stored rows.
	rows, err = tx.QueryContext(ctx, `SELECT id,run_id,step_key,state,substr(result_json,1,2048),length(result_json)>2048,created_at FROM office_step_receipts WHERE task_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var st officestudio.SnapshotStep
		var created string
		if err = rows.Scan(&st.ID, &st.RunID, &st.Label, &st.State, &st.Summary, &st.SummaryTruncated, &created); err != nil {
			rows.Close()
			return out, err
		}
		st.CreatedAt, err = parseRFC(created)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Steps = append(out.Steps, st)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,source_version_id,target_version_id,source_node,target_node,substr(metric_json,1,2048),length(metric_json)>2048,created_at FROM office_evidence_edges WHERE task_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var edge officestudio.SnapshotSource
		var created string
		if err = rows.Scan(&edge.ID, &edge.VersionID, &edge.TargetVersionID, &edge.Location, &edge.TargetLocation, &edge.Transform, &edge.TransformTruncated, &created); err != nil {
			rows.Close()
			return out, err
		}
		edge.CreatedAt, err = parseRFC(created)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Sources = append(out.Sources, edge)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) ReadOfficeTaskListSnapshot(ctx context.Context, sessionID, query string) ([]officestudio.SnapshotTask, error) {
	if sessionID != "" && !officestudio.ValidID(sessionID) {
		return nil, officestudio.ErrInvalid
	}
	columns := strings.Replace(officeTaskColumns, "checkpoint_json,", "'{}',", 1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+`,COALESCE((SELECT project_id FROM sessions WHERE id=office_tasks.session_id),'') FROM office_tasks WHERE owner_org_id=? AND (?='' OR session_id=?) AND NOT EXISTS(SELECT 1 FROM sessions x JOIN projects p ON p.id=x.project_id WHERE x.id=office_tasks.session_id AND COALESCE(p.org_id,'')<>office_tasks.owner_org_id) ORDER BY updated_at DESC,id DESC`, officestudio.Scope(ctx), sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.SnapshotTask{}
	for rows.Next() {
		var item officestudio.SnapshotTask
		var checkpoint, created, updated string
		t := &item.Task
		if err = rows.Scan(&t.ID, &t.SessionID, &t.Title, &t.Goal, &t.Status, &t.Revision, &t.RunID, &t.StartMessageID, &checkpoint, &created, &updated, &item.ProjectID); err != nil {
			return nil, err
		}
		// Preserve Unicode-aware matching and search the complete goal, then
		// discard its long tail before retaining the list snapshot in memory.
		if query != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Goal), strings.ToLower(query)) {
			continue
		}
		t.Goal, item.GoalTruncated = officeSnapshotText(t.Goal, 256)
		t.Checkpoint = json.RawMessage(checkpoint)
		if t.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		if t.UpdatedAt, err = parseRFC(updated); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
