package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

var _ officestudio.Store = (*Store)(nil)

func officeJSON(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage(`{}`)
	}
	return v
}

func officeDigest(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func officeError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return officestudio.ErrNotFound
	}
	return err
}

func officeKey(key string) bool {
	return len(key) > 0 && len(key) <= 128 && !strings.ContainsAny(key, "\x00\r\n")
}

type officeScanner interface{ Scan(...any) error }

const officeTaskColumns = `id,session_id,title,goal,status,revision,run_id,start_message_id,checkpoint_json,created_at,updated_at`

func scanOfficeTask(row officeScanner) (officestudio.Task, error) {
	var t officestudio.Task
	var checkpoint, created, updated string
	err := row.Scan(&t.ID, &t.SessionID, &t.Title, &t.Goal, &t.Status, &t.Revision, &t.RunID, &t.StartMessageID, &checkpoint, &created, &updated)
	if err != nil {
		return t, officeError(err)
	}
	t.Checkpoint = json.RawMessage(checkpoint)
	if t.CreatedAt, err = parseRFC(created); err != nil {
		return t, err
	}
	t.UpdatedAt, err = parseRFC(updated)
	return t, err
}

func (s *Store) GetOfficeTask(ctx context.Context, id string) (officestudio.Task, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", id); err != nil {
		return officestudio.Task{}, err
	}
	return scanOfficeTask(s.db.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, id))
}

func (s *Store) FindOfficeTaskByKey(ctx context.Context, sessionID, key string) (officestudio.Task, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM office_tasks WHERE session_id=? AND idempotency_key=? AND owner_org_id=?`, sessionID, key, officestudio.Scope(ctx)).Scan(&id); err != nil {
		return officestudio.Task{}, officeError(err)
	}
	return s.GetOfficeTask(ctx, id)
}

func (s *Store) ListOfficeTasks(ctx context.Context, sessionID string, limit int) ([]officestudio.Task, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE owner_org_id=? AND (?='' OR session_id=?) AND NOT EXISTS(SELECT 1 FROM sessions x JOIN projects p ON p.id=x.project_id WHERE x.id=office_tasks.session_id AND COALESCE(p.org_id,'')<>office_tasks.owner_org_id) ORDER BY updated_at DESC,id DESC LIMIT ?`, officestudio.Scope(ctx), sessionID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Task{}
	for rows.Next() {
		t, e := scanOfficeTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateOfficeTask(ctx context.Context, t officestudio.Task, key string) (officestudio.Task, error) {
	if !officeKey(key) {
		return t, officestudio.ErrInvalid
	}
	if t.ID == "" {
		t.ID = ulid.Make().String()
	}
	if t.Status == "" {
		t.Status = "draft"
	}
	t.Revision = 1
	t.Checkpoint = officeJSON(t.Checkpoint)
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	t.UpdatedAt = t.CreatedAt
	if err := t.Validate(); err != nil {
		return t, err
	}
	digest := officeDigest(struct{ SessionID, Title, Goal, StartMessageID string }{t.SessionID, t.Title, t.Goal, t.StartMessageID})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	var existingID, existingDigest string
	err = tx.QueryRowContext(ctx, `SELECT id,request_digest FROM office_tasks WHERE session_id=? AND idempotency_key=?`, t.SessionID, key).Scan(&existingID, &existingDigest)
	if err == nil {
		if err = officeAuthorize(ctx, tx, "office-task", existingID); err != nil {
			return t, err
		}
		if existingDigest != digest {
			return t, officestudio.ErrConflict
		}
		return scanOfficeTask(tx.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, existingID))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	if err = officeAuthorize(ctx, tx, "session", t.SessionID); err != nil {
		return t, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, t.SessionID).Scan(&count); err != nil {
		return t, err
	}
	if count != 1 {
		return t, officestudio.ErrScope
	}
	if t.StartMessageID != "" {
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE id=? AND session_id=?`, t.StartMessageID, t.SessionID).Scan(&count); err != nil {
			return t, err
		}
		if count != 1 {
			return t, officestudio.ErrScope
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_tasks(id,session_id,owner_org_id,title,goal,status,revision,run_id,start_message_id,checkpoint_json,idempotency_key,request_digest,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, t.ID, t.SessionID, officestudio.Scope(ctx), t.Title, t.Goal, t.Status, t.Revision, t.RunID, t.StartMessageID, string(t.Checkpoint), key, digest, officeRFC(t.CreatedAt), officeRFC(t.UpdatedAt))
	if err != nil {
		return t, err
	}
	if err = appendOfficeEvent(ctx, tx, t.ID, "", "task.created", map[string]any{"status": t.Status}, t.CreatedAt); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

func (s *Store) UpdateOfficeTask(ctx context.Context, t officestudio.Task, expected int64) (officestudio.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", t.ID); err != nil {
		return t, err
	}
	old, err := scanOfficeTask(tx.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, t.ID))
	if err != nil {
		return t, err
	}
	if old.SessionID != t.SessionID {
		return t, officestudio.ErrScope
	}
	if expected != old.Revision {
		return t, officestudio.ErrConflict
	}
	if !officestudio.ValidTransition(old.Status, t.Status) {
		return t, officestudio.ErrInvalid
	}
	t.CreatedAt = old.CreatedAt
	t.StartMessageID = old.StartMessageID
	t.UpdatedAt = time.Now().UTC()
	t.Revision = expected + 1
	t.Checkpoint = officeJSON(t.Checkpoint)
	if err = t.Validate(); err != nil {
		return t, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_tasks SET title=?,goal=?,status=?,revision=?,run_id=?,checkpoint_json=?,updated_at=? WHERE id=? AND revision=?`, t.Title, t.Goal, t.Status, t.Revision, t.RunID, string(t.Checkpoint), officeRFC(t.UpdatedAt), t.ID, expected)
	if err != nil {
		return t, err
	}
	if err = appendOfficeEvent(ctx, tx, t.ID, "", "task.updated", map[string]any{"from": old.Status, "to": t.Status, "revision": t.Revision}, t.UpdatedAt); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

const officeVersionColumns = `v.id,b.task_id,v.artifact_id,v.version_no,m.kind,m.name,v.content_ref,v.sha256,v.size,v.media_type,m.content_mode,COALESCE(m.base_version_id,''),m.spec_json,m.index_json,m.quality,v.created_at`
const officeVersionFrom = ` FROM artifact_versions v JOIN office_version_metadata m ON m.version_id=v.id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id `

func scanOfficeVersion(row officeScanner) (officestudio.Version, error) {
	var v officestudio.Version
	var spec, index, created string
	err := row.Scan(&v.ID, &v.TaskID, &v.ArtifactID, &v.VersionNo, &v.Kind, &v.Name, &v.ContentRef, &v.SHA256, &v.Size, &v.MediaType, &v.ContentMode, &v.BaseVersionID, &spec, &index, &v.Quality, &created)
	if err != nil {
		return v, officeError(err)
	}
	v.Spec = json.RawMessage(spec)
	v.Index = json.RawMessage(index)
	v.CreatedAt, err = parseRFC(created)
	return v, err
}

func (s *Store) GetOfficeVersion(ctx context.Context, id string) (officestudio.Version, error) {
	if err := officeAuthorize(ctx, s.db, "office-version", id); err != nil {
		return officestudio.Version{}, err
	}
	return scanOfficeVersion(s.db.QueryRowContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE v.id=?`, id))
}

func (s *Store) ListOfficeVersions(ctx context.Context, taskID, artifactID string) ([]officestudio.Version, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE b.task_id=? AND (?='' OR v.artifact_id=?) ORDER BY v.version_no DESC,v.id DESC LIMIT 1000`, taskID, artifactID, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Version{}
	for rows.Next() {
		v, e := scanOfficeVersion(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListOfficeHeads(ctx context.Context, taskID string) ([]officestudio.Head, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT b.task_id,h.artifact_id,h.latest_version_id,COALESCE(h.accepted_version_id,''),h.revision FROM office_artifact_heads h JOIN office_task_artifacts b ON b.artifact_id=h.artifact_id WHERE b.task_id=? ORDER BY h.artifact_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Head{}
	for rows.Next() {
		var h officestudio.Head
		if err = rows.Scan(&h.TaskID, &h.ArtifactID, &h.LatestVersionID, &h.AcceptedVersionID, &h.Revision); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) PublishOfficeVersion(ctx context.Context, r officestudio.PublishRequest) (officestudio.Version, error) {
	v := r.Version
	generatedArtifactID := v.ArtifactID == ""
	if generatedArtifactID {
		v.ArtifactID = ulid.Make().String()
	}
	if !officeKey(r.IdempotencyKey) || r.ExpectedHeadRevision < 0 {
		return v, officestudio.ErrInvalid
	}
	if v.ID == "" {
		v.ID = ulid.Make().String()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if v.ContentMode == "" {
		v.ContentMode = "managed"
	}
	v.Spec = officeJSON(v.Spec)
	v.Index = officeJSON(v.Index)
	v.Quality = "unverified"
	if err := v.Validate(); err != nil {
		return v, err
	}
	if r.CreatedBy == "" {
		r.CreatedBy = "local-user"
	}
	if len(r.CreatedBy) > 128 {
		return v, officestudio.ErrInvalid
	}
	identity := v
	if generatedArtifactID {
		identity.ArtifactID = ""
	}
	identity.ID = ""
	identity.CreatedAt = time.Time{}
	identity.VersionNo = 0
	evidenceIdentity := make([]officestudio.EvidenceEdge, len(r.Evidence))
	for i, e := range r.Evidence {
		e.ID = ""
		e.TaskID = v.TaskID
		e.TargetVersionID = ""
		e.CreatedAt = time.Time{}
		evidenceIdentity[i] = e
	}
	digest := officeDigest(struct {
		Version  officestudio.Version
		Expected int64
		Actor    string
		Evidence []officestudio.EvidenceEdge
	}{identity, r.ExpectedHeadRevision, r.CreatedBy, evidenceIdentity})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = lockOfficeStorage(ctx, tx); err != nil {
		return v, err
	}
	if err = officeAuthorize(ctx, tx, "office-task", v.TaskID); err != nil {
		return v, err
	}
	var oldID, oldDigest string
	err = tx.QueryRowContext(ctx, `SELECT m.version_id,m.request_digest FROM office_version_metadata m JOIN artifact_versions v ON v.id=m.version_id JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id WHERE b.task_id=? AND m.idempotency_key=?`, v.TaskID, r.IdempotencyKey).Scan(&oldID, &oldDigest)
	if err == nil {
		if oldDigest != digest {
			return v, officestudio.ErrConflict
		}
		return scanOfficeVersion(tx.QueryRowContext(ctx, `SELECT `+officeVersionColumns+officeVersionFrom+` WHERE v.id=?`, oldID))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	task, err := scanOfficeTask(tx.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, v.TaskID))
	if err != nil {
		return v, err
	}
	if task.Status == "cancelling" || task.Status == "cancelled" || task.Status == "interrupted" {
		return v, officestudio.ErrConflict
	}
	var binding string
	err = tx.QueryRowContext(ctx, `SELECT task_id FROM office_task_artifacts WHERE artifact_id=?`, v.ArtifactID).Scan(&binding)
	if err == nil && binding != v.TaskID {
		return v, officestudio.ErrScope
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		var existing int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM artifact_versions WHERE artifact_id=?`, v.ArtifactID).Scan(&existing); err != nil {
			return v, err
		}
		if existing > 0 {
			return v, officestudio.ErrScope
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO office_task_artifacts(artifact_id,task_id,role,created_at) VALUES(?,?,'output',?)`, v.ArtifactID, v.TaskID, officeRFC(v.CreatedAt))
		if err != nil {
			return v, err
		}
	}
	var latest string
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT latest_version_id,revision FROM office_artifact_heads WHERE artifact_id=?`, v.ArtifactID).Scan(&latest, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, err
	}
	if revision != r.ExpectedHeadRevision {
		return v, officestudio.ErrConflict
	}
	if v.BaseVersionID != "" {
		var baseArtifact string
		if err = tx.QueryRowContext(ctx, `SELECT artifact_id FROM artifact_versions WHERE id=?`, v.BaseVersionID).Scan(&baseArtifact); err != nil {
			return v, officeError(err)
		}
		if baseArtifact != v.ArtifactID {
			return v, officestudio.ErrScope
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version_no),0)+1 FROM artifact_versions WHERE artifact_id=?`, v.ArtifactID).Scan(&v.VersionNo); err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO artifact_versions(id,artifact_id,version_no,kind,scope_type,scope_id,content_ref,sha256,size,media_type,state,created_by,created_at) VALUES(?,?,?,'document','session',?,?,?,?,?,'active',?,?)`, v.ID, v.ArtifactID, v.VersionNo, task.SessionID, v.ContentRef, v.SHA256, v.Size, v.MediaType, r.CreatedBy, officeRFC(v.CreatedAt))
	if err != nil {
		return v, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_version_metadata(version_id,kind,name,content_mode,base_version_id,spec_json,index_json,quality,idempotency_key,request_digest) VALUES(?,?,?,?,?,?,?,'unverified',?,?)`, v.ID, v.Kind, v.Name, v.ContentMode, nullString(v.BaseVersionID), string(v.Spec), string(v.Index), r.IdempotencyKey, digest)
	if err != nil {
		return v, err
	}
	if err = bindOfficeBlob(ctx, tx, v.TaskID, v.ContentRef, v.Size, r.BlobLeaseID, "version", v.ID, time.Now().UTC()); err != nil {
		return v, err
	}
	if err = publishOfficeEvidence(ctx, tx, v, r.Evidence); err != nil {
		return v, err
	}
	inheritedStale, err := inheritOfficeEvidence(ctx, tx, v, r.Evidence)
	if err != nil {
		return v, err
	}
	if inheritedStale {
		v.Quality = "stale"
	}
	if revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO office_artifact_heads(artifact_id,latest_version_id,accepted_version_id,revision) VALUES(?,?,NULL,1)`, v.ArtifactID, v.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE office_artifact_heads SET latest_version_id=?,revision=revision+1 WHERE artifact_id=? AND revision=?`, v.ID, v.ArtifactID, revision)
	}
	if err != nil {
		return v, err
	}
	if v.BaseVersionID != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO artifact_derivations(id,artifact_version_id,derived_from_version,relation,created_at) VALUES(?,?,?,'derived_from',?)`, ulid.Make().String(), v.ID, v.BaseVersionID, officeRFC(v.CreatedAt))
		if err != nil {
			return v, err
		}
	}
	if err = appendOfficeEvent(ctx, tx, v.TaskID, v.ID, "version.published", map[string]any{"artifactId": v.ArtifactID, "headRevision": revision + 1, "sha256": v.SHA256, "previousHead": latest}, v.CreatedAt); err != nil {
		return v, err
	}
	if latest != "" {
		if _, err = markOfficeDependentsStale(ctx, tx, latest); err != nil {
			return v, err
		}
	}
	return v, tx.Commit()
}

func (s *Store) AcceptOfficeVersion(ctx context.Context, taskID, artifactID, versionID string, expected int64) (officestudio.Head, error) {
	var h officestudio.Head
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", taskID); err != nil {
		return h, err
	}
	err = tx.QueryRowContext(ctx, `SELECT b.task_id,h.artifact_id,h.latest_version_id,COALESCE(h.accepted_version_id,''),h.revision FROM office_artifact_heads h JOIN office_task_artifacts b ON b.artifact_id=h.artifact_id WHERE h.artifact_id=?`, artifactID).Scan(&h.TaskID, &h.ArtifactID, &h.LatestVersionID, &h.AcceptedVersionID, &h.Revision)
	if err != nil {
		return h, officeError(err)
	}
	if h.TaskID != taskID {
		return h, officestudio.ErrScope
	}
	if expected != h.Revision {
		return h, officestudio.ErrConflict
	}
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT artifact_id FROM artifact_versions WHERE id=?`, versionID).Scan(&owner); err != nil {
		return h, officeError(err)
	}
	if owner != artifactID {
		return h, officestudio.ErrScope
	}
	if h.AcceptedVersionID == versionID {
		return h, nil
	}
	previous := h.AcceptedVersionID
	h.AcceptedVersionID = versionID
	h.Revision++
	_, err = tx.ExecContext(ctx, `UPDATE office_artifact_heads SET accepted_version_id=?,revision=? WHERE artifact_id=? AND revision=?`, versionID, h.Revision, artifactID, expected)
	if err != nil {
		return h, err
	}
	if err = appendOfficeEvent(ctx, tx, taskID, versionID, "version.accepted", map[string]any{"previousAcceptedVersionId": previous, "revision": h.Revision}, time.Now().UTC()); err != nil {
		return h, err
	}
	return h, tx.Commit()
}

func appendOfficeEvent(ctx context.Context, tx *sql.Tx, taskID, versionID, action string, detail any, at time.Time) error {
	b, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO artifact_lifecycle_events(id,task_id,version_id,action,detail_json,created_at) VALUES(?,?,?,?,?,?)`, ulid.Make().String(), taskID, nullString(versionID), action, string(b), officeRFC(at))
	return err
}

func (s *Store) ListOfficeEvents(ctx context.Context, taskID string, limit int) ([]officestudio.Event, error) {
	if err := officeAuthorize(ctx, s.db, "office-task", taskID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,COALESCE(version_id,''),action,detail_json,created_at FROM artifact_lifecycle_events WHERE task_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Event{}
	for rows.Next() {
		var e officestudio.Event
		var detail, created string
		if err = rows.Scan(&e.ID, &e.TaskID, &e.VersionID, &e.Action, &detail, &created); err != nil {
			return nil, err
		}
		e.Detail = json.RawMessage(detail)
		e.CreatedAt, err = parseRFC(created)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func officeAuthorize(ctx context.Context, db dataScopeQueryer, kind, id string) error {
	return authorizeDataResource(ctx, db, kind, id, officestudio.Scope(ctx), make(map[string]uint8))
}

// Fixed fractional precision keeps chronological ordering exact in SQLite TEXT.
func officeRFC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func (s *Store) ListOfficeActiveTasks(ctx context.Context, limit int) ([]officestudio.Task, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE owner_org_id=? AND status IN ('queued','planning','running','validating','cancelling') AND NOT EXISTS(SELECT 1 FROM sessions x JOIN projects p ON p.id=x.project_id WHERE x.id=office_tasks.session_id AND COALESCE(p.org_id,'')<>office_tasks.owner_org_id) ORDER BY updated_at,id LIMIT ?`, officestudio.Scope(ctx), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []officestudio.Task{}
	for rows.Next() {
		t, e := scanOfficeTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
