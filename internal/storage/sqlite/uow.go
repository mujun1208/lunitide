package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/stageapp"
	"github.com/oklog/ulid/v2"
)

// Do runs the complete provider application mutation under one SQLite writer
// lock. txAdapter methods only use this connection and never start nested
// transactions.
func (s *Store) Do(ctx context.Context, fn func(providerapp.Tx) error) (resultErr error) {
	return s.do(ctx, func(tx *txAdapter) error { return fn(tx) })
}

func (s *Store) DoProject(ctx context.Context, fn func(projectapp.Tx) error) (resultErr error) {
	return s.do(ctx, func(tx *txAdapter) error { return fn(tx) })
}

func (s *Store) DoSession(ctx context.Context, fn func(sessionapp.Tx) error) error {
	return s.do(ctx, func(tx *txAdapter) error { return fn(tx) })
}

func (s *Store) DoStage(ctx context.Context, fn func(stageapp.Tx) error) error {
	return s.do(ctx, func(tx *txAdapter) error { return fn(tx) })
}

func (s *Store) DoMessage(ctx context.Context, fn func(messageapp.Tx) error) error {
	return s.do(ctx, func(tx *txAdapter) error { return fn(tx) })
}

func (s *Store) do(ctx context.Context, fn func(*txAdapter) error) (resultErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return mapWriteError(err)
	}
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if err = fn(&txAdapter{s: s, q: conn}); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return mapWriteError(err)
	}
	return nil
}

type txAdapter struct {
	s *Store
	q *sql.Conn
}

const projectColumns = `id,name,project_code,project_type,description,summary,objective,client,contract_no,amount,budget,plan_start,plan_end,remark,close_reason,status,created_at,updated_at,version`

func (t *txAdapter) getProject(ctx context.Context, id string) (project.Project, error) {
	var p project.Project
	var created, updated string
	row := t.q.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id=?`, id)
	if err := row.Scan(&p.ID, &p.Name, &p.ProjectCode, &p.Type, &p.Description, &p.Summary, &p.Objective, &p.Client, &p.ContractNo, &p.Amount, &p.Budget, &p.PlanStart, &p.PlanEnd, &p.Remark, &p.CloseReason, &p.Status, &created, &updated, &p.Version); err != nil {
		if err == sql.ErrNoRows {
			return p, project.ErrNotFound
		}
		return p, err
	}
	var err error
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return p, err
	}
	if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return p, err
	}
	return p, p.Validate()
}

func (t *txAdapter) nextProjectCode(ctx context.Context) (string, error) {
	var max int
	if err := t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(substr(project_code,4) AS INTEGER)),0) FROM projects`).Scan(&max); err != nil {
		return "", err
	}
	return fmt.Sprintf("ITM%05d", max+1), nil
}

func (t *txAdapter) CreateProject(ctx context.Context, p project.Project) (project.Project, error) {
	var count int
	if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&count); err != nil {
		return p, err
	}
	if count >= 100 {
		return p, projectapp.ErrProjectCapacityReached
	}
	var err error
	p.Name, err = project.NormalizeName(p.Name)
	if err != nil {
		return p, err
	}
	if p.ID == "" {
		p.ID, err = t.s.newULID(time.Now())
	} else if id, e := ulid.ParseStrict(p.ID); e != nil || id.String() != p.ID || p.ID[0] > '7' {
		return p, fmt.Errorf("project ID must be an uppercase canonical ULID")
	}
	if err != nil {
		return p, err
	}
	if p.Status == "" {
		p.Status = project.StatusCreated
	}
	if p.Type == "" {
		p.Type = project.TypeImplementation
	}
	if p.ProjectCode == "" {
		if p.ProjectCode, err = t.nextProjectCode(ctx); err != nil {
			return p, err
		}
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt, p.Version = now, now, 1
	if err = p.Validate(); err != nil {
		return p, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO projects(`+projectColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.ProjectCode, p.Type, p.Description, p.Summary, p.Objective, p.Client, p.ContractNo, p.Amount, p.Budget, p.PlanStart, p.PlanEnd, p.Remark, p.CloseReason, p.Status, formatTime(now), formatTime(now), p.Version)
	if err == nil {
		_, err = t.q.ExecContext(ctx, `INSERT INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, p.ID)
	}
	return p, mapWriteError(err)
}

func (t *txAdapter) UpdateProject(ctx context.Context, id string, version int64, mutate func(*project.Project) error) (project.Project, error) {
	p, err := t.getProject(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Version != version {
		return p, projectapp.ErrProjectVersionConflict
	}
	if err = mutate(&p); err != nil {
		return p, err
	}
	p.UpdatedAt = time.Now().UTC()
	p.Version = version + 1
	if err = p.Validate(); err != nil {
		return p, err
	}
	result, err := t.q.ExecContext(ctx, `UPDATE projects SET name=?,project_type=?,description=?,summary=?,objective=?,client=?,contract_no=?,amount=?,budget=?,plan_start=?,plan_end=?,remark=?,close_reason=?,status=?,updated_at=?,version=? WHERE id=? AND version=?`,
		p.Name, p.Type, p.Description, p.Summary, p.Objective, p.Client, p.ContractNo, p.Amount, p.Budget, p.PlanStart, p.PlanEnd, p.Remark, p.CloseReason, p.Status, formatTime(p.UpdatedAt), p.Version, id, version)
	if err != nil {
		return p, mapWriteError(err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return p, projectapp.ErrProjectVersionConflict
	}
	return p, nil
}

func (t *txAdapter) GetProject(ctx context.Context, id string) (project.Project, error) {
	return t.getProject(ctx, id)
}

func (t *txAdapter) CreateSession(ctx context.Context, v session.Session) (session.Session, error) {
	var exists int
	if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, v.ProjectID).Scan(&exists); err != nil {
		return v, err
	}
	if exists != 1 {
		return v, sessionapp.ErrProjectNotFound
	}
	var count int
	if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE project_id=?`, v.ProjectID).Scan(&count); err != nil {
		return v, err
	}
	if count >= 100 {
		return v, sessionapp.ErrSessionCapacityReached
	}
	var err error
	v.Title, err = session.NormalizeTitle(v.Title)
	if err != nil {
		return v, err
	}
	v.ID, err = t.s.newULID(time.Now())
	if err != nil {
		return v, err
	}
	now := time.Now().UTC()
	v.Status = session.StatusActive
	v.CreatedAt = now
	v.UpdatedAt = now
	v.Version = 1
	if err = v.Validate(); err != nil {
		return v, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?)`, v.ID, v.ProjectID, v.Title, v.Status, formatTime(now), formatTime(now), v.Version)
	if err == nil {
		_, err = t.q.ExecContext(ctx, `INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES(?,0,0,0)`, v.ID)
	}
	return v, mapWriteError(err)
}

func (t *txAdapter) UpdateSession(ctx context.Context, id string, version int64, title string, pinned bool, now time.Time) (session.Session, error) {
	title, err := session.NormalizeTitle(title)
	if err != nil {
		return session.Session{}, err
	}
	result, err := t.q.ExecContext(ctx, `UPDATE sessions SET title=?,pinned=?,updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, title, pinned, formatTime(now), id, version)
	if err != nil {
		return session.Session{}, mapWriteError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return session.Session{}, err
	}
	if n == 0 {
		var exists int
		if e := t.q.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, id).Scan(&exists); e != nil {
			return session.Session{}, e
		}
		if exists == 0 {
			return session.Session{}, sessionapp.ErrSessionNotFound
		}
		return session.Session{}, sessionapp.ErrSessionVersionConflict
	}
	var v session.Session
	var created, updated string
	err = t.q.QueryRowContext(ctx, `SELECT id,project_id,title,pinned,status,created_at,updated_at,revision FROM sessions WHERE id=?`, id).Scan(&v.ID, &v.ProjectID, &v.Title, &v.Pinned, &v.Status, &created, &updated, &v.Version)
	if err != nil {
		return v, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return v, err
	}
	v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return v, err
	}
	return v, v.Validate()
}

func (t *txAdapter) CreateStage(ctx context.Context, v stage.Stage) (stage.Stage, error) {
	var exists int
	if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, v.ProjectID).Scan(&exists); err != nil {
		return v, err
	}
	if exists != 1 {
		return v, stageapp.ErrProjectNotFound
	}
	var conflict int
	if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM stages WHERE project_id=? AND phase=?`, v.ProjectID, v.Phase).Scan(&conflict); err != nil {
		return v, err
	}
	if conflict != 0 {
		return v, stageapp.ErrStagePhaseConflict
	}
	var err error
	v.Title, err = stage.NormalizeTitle(v.Title)
	if err != nil {
		return v, err
	}
	v.ID, err = t.s.newULID(time.Now())
	if err != nil {
		return v, err
	}
	now := time.Now().UTC()
	v.Status = stage.StatusNotStarted
	v.CreatedAt = now
	v.UpdatedAt = now
	v.Version = 1
	if err = v.Validate(); err != nil {
		return v, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO stages(id,project_id,phase,title,status,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,?)`, v.ID, v.ProjectID, v.Phase, v.Title, v.Status, formatTime(now), formatTime(now), v.Version)
	return v, mapWriteError(err)
}

func (t *txAdapter) AppendMessage(ctx context.Context, v message.Message) (message.Message, error) {
	var projectID string
	if err := t.q.QueryRowContext(ctx, `SELECT project_id FROM sessions WHERE id=?`, v.SessionID).Scan(&projectID); err != nil {
		if err == sql.ErrNoRows {
			return v, messageapp.ErrSessionNotFound
		}
		return v, err
	}
	var sequence, count, sessionBytes int64
	if err := t.q.QueryRowContext(ctx, `SELECT last_sequence,message_count,text_bytes FROM message_session_state WHERE session_id=?`, v.SessionID).Scan(&sequence, &count, &sessionBytes); err != nil {
		return v, err
	}
	if sequence != count {
		return v, messageapp.ErrDataInvariantViolation
	}
	if sequence > 0 {
		var tail int64
		if err := t.q.QueryRowContext(ctx, `SELECT sequence FROM messages WHERE session_id=? ORDER BY sequence DESC LIMIT 1`, v.SessionID).Scan(&tail); err != nil || tail != sequence {
			return v, messageapp.ErrDataInvariantViolation
		}
	}
	sequence++
	if sequence < 1 || sequence > message.MaxSafeSequence {
		return v, messageapp.ErrDataInvariantViolation
	}
	// Validate text based on role: assistant and tool messages allow wider limits.
	var text string
	var err error
	if v.Role == message.RoleAssistant || v.Role == message.RoleTool {
		text, err = message.NormalizeAssistantText(v.Text)
	} else {
		text, err = message.NormalizeText(v.Text)
	}
	if err != nil {
		return v, err
	}
	v.Text = text
	textBytes := int64(len(text))
	res, err := t.q.ExecContext(ctx, `UPDATE message_project_usage SET text_bytes=text_bytes+? WHERE project_id=? AND text_bytes<=?-?`, textBytes, projectID, message.ProjectTextQuotaBytes, textBytes)
	if err != nil {
		return v, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		var used int64
		if err = t.q.QueryRowContext(ctx, `SELECT text_bytes FROM message_project_usage WHERE project_id=?`, projectID).Scan(&used); err != nil {
			if err == sql.ErrNoRows {
				return v, messageapp.ErrDataInvariantViolation
			}
			return v, err
		}
		if n != 0 || used < 0 || used > message.ProjectTextQuotaBytes || textBytes <= message.ProjectTextQuotaBytes-used {
			return v, messageapp.ErrDataInvariantViolation
		}
		return v, messageapp.ErrMessageStorageQuotaReached
	}
	res, err = t.q.ExecContext(ctx, `UPDATE message_workspace_usage SET text_bytes=text_bytes+? WHERE singleton=1 AND text_bytes<=?-?`, textBytes, message.WorkspaceTextQuotaBytes, textBytes)
	if err != nil {
		return v, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		var used int64
		if err = t.q.QueryRowContext(ctx, `SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&used); err != nil {
			if err == sql.ErrNoRows {
				return v, messageapp.ErrDataInvariantViolation
			}
			return v, err
		}
		if n != 0 || used < 0 || used > message.WorkspaceTextQuotaBytes || textBytes <= message.WorkspaceTextQuotaBytes-used {
			return v, messageapp.ErrDataInvariantViolation
		}
		return v, messageapp.ErrMessageStorageQuotaReached
	}
	res, err = t.q.ExecContext(ctx, `UPDATE message_session_state SET last_sequence=last_sequence+1,message_count=message_count+1,text_bytes=text_bytes+? WHERE session_id=? AND last_sequence=?`, textBytes, v.SessionID, sequence-1)
	if err != nil {
		return v, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		return v, messageapp.ErrDataInvariantViolation
	}
	v.ID, err = t.s.newULID(time.Now())
	if err != nil {
		return v, err
	}
	if v.Role == "" {
		v.Role = message.RoleUser
	}
	if v.Status == "" {
		v.Status = message.StatusCompleted
	}
	v.Sequence, v.CreatedAt = sequence, time.Now().UTC()
	if err = v.Validate(); err != nil {
		return v, err
	}
	if _, err = t.q.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,status,sequence,created_at) VALUES(?,?,?,?,?,?)`, v.ID, v.SessionID, v.Role, v.Status, v.Sequence, formatTime(v.CreatedAt)); err != nil {
		return v, mapWriteError(err)
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO message_parts(message_id,ordinal,type,text) VALUES(?,1,'text',?)`, v.ID, v.Text)
	if err != nil {
		return v, mapWriteError(err)
	}
	// Only write char-ratio token estimate for user messages.
	// Assistant messages get canonical + provider-reported entries via
	// PutTokenLedgerEntry in messageapp.AppendAssistant.
	// The canonical tokenizer revision is persisted so that context.status
	// can report canonicalTokenizerVersion and the ledger can be queried by
	// the frozen revision (architecture doc §12.1.1).
	if v.Role == message.RoleUser {
		tokenID, err := t.s.newULID(time.Now())
		if err != nil {
			return v, err
		}
		tokenEstimate := token.EstimateTokens(v.Text)
		if _, err = t.q.ExecContext(ctx,
			`INSERT INTO token_ledger(id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at, subject_type, subject_id, tokenizer_id)
			 VALUES(?,?,?,?,?,?,?,?,?, 'message', ?, ?)`,
			tokenID, v.ID, "", "", token.CanonicalTokenizerRevision, tokenEstimate, string(token.CharRatio), int64(len(v.Text)), formatTime(time.Now().UTC()), v.ID, token.CanonicalTokenizerID); err != nil {
			return v, mapWriteError(err)
		}
	}
	return v, nil
}

func (t *txAdapter) RewindMessages(ctx context.Context, sessionID, messageID string) (messageapp.RewindResult, error) {
	r := messageapp.RewindResult{SessionID: sessionID, MessageID: messageID}
	var seq, bytes int64
	var role, projectID string
	if err := t.q.QueryRowContext(ctx, `SELECT m.sequence,m.role,s.project_id FROM messages m JOIN sessions s ON s.id=m.session_id WHERE m.id=? AND m.session_id=?`, messageID, sessionID).Scan(&seq, &role, &projectID); err != nil {
		if err == sql.ErrNoRows {
			return r, messageapp.ErrMessageNotFound
		}
		return r, err
	}
	if role != "user" {
		return r, messageapp.ErrRewindRequiresUserMessage
	}
	if err := t.q.QueryRowContext(ctx, `SELECT count(*),COALESCE(SUM(length(CAST(p.text AS BLOB))),0) FROM messages m JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=? AND m.sequence>=?`, sessionID, seq).Scan(&r.DeletedCount, &bytes); err != nil {
		return r, err
	}
	if _, err := t.q.ExecContext(ctx, `DELETE FROM handoff_imports WHERE capsule_id IN(SELECT h.id FROM handoff_capsules h JOIN compaction_checkpoints c ON c.id=h.checkpoint_id WHERE c.session_id=?)`, sessionID); err != nil {
		return r, err
	}
	if _, err := t.q.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE checkpoint_id IN(SELECT id FROM compaction_checkpoints WHERE session_id=?)`, sessionID); err != nil {
		return r, err
	}
	if _, err := t.q.ExecContext(ctx, `UPDATE compaction_activations SET checkpoint_id=NULL,revision=revision+1,updated_at=? WHERE session_id=?`, formatTime(time.Now().UTC()), sessionID); err != nil {
		return r, err
	}
	if _, err := t.q.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, sessionID); err != nil {
		return r, err
	}
	if _, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_records WHERE operation IN('message.append','message.append-assistant') AND json_extract(response_json,'$.sessionId')=? AND json_extract(response_json,'$.sequence')>=?`, sessionID, seq); err != nil {
		return r, err
	}
	deleted, err := t.q.ExecContext(ctx, `DELETE FROM messages WHERE session_id=? AND sequence>=?`, sessionID, seq)
	if err != nil {
		return r, err
	}
	if n, err := deleted.RowsAffected(); err != nil || n != r.DeletedCount {
		return r, messageapp.ErrDataInvariantViolation
	}
	r.LastSequence = seq - 1
	state, err := t.q.ExecContext(ctx, `UPDATE message_session_state SET last_sequence=?,message_count=?,text_bytes=text_bytes-?,history_revision=history_revision+1 WHERE session_id=? AND message_count=last_sequence AND last_sequence>=? AND text_bytes>=?`, r.LastSequence, r.LastSequence, bytes, sessionID, seq, bytes)
	if err != nil {
		return r, err
	}
	if n, err := state.RowsAffected(); err != nil || n != 1 {
		return r, messageapp.ErrDataInvariantViolation
	}
	project, err := t.q.ExecContext(ctx, `UPDATE message_project_usage SET text_bytes=text_bytes-? WHERE project_id=? AND text_bytes>=?`, bytes, projectID, bytes)
	if err != nil {
		return r, err
	}
	if n, err := project.RowsAffected(); err != nil || n != 1 {
		return r, messageapp.ErrDataInvariantViolation
	}
	workspace, err := t.q.ExecContext(ctx, `UPDATE message_workspace_usage SET text_bytes=text_bytes-? WHERE singleton=1 AND text_bytes>=?`, bytes, bytes)
	if err != nil {
		return r, err
	}
	if n, err := workspace.RowsAffected(); err != nil || n != 1 {
		return r, messageapp.ErrDataInvariantViolation
	}
	if err := t.q.QueryRowContext(ctx, `SELECT history_revision FROM message_session_state WHERE session_id=?`, sessionID).Scan(&r.HistoryRevision); err != nil {
		return r, err
	}
	return r, nil
}

func (t *txAdapter) Message(ctx context.Context, id string) (message.Message, error) {
	var v message.Message
	var created string
	err := t.q.QueryRowContext(ctx, `SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id WHERE m.id=? GROUP BY m.id HAVING count(p.message_id)=1 AND count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END)=1`, id).Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &v.Text, &created)
	if err != nil {
		return v, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil || v.Validate() != nil {
		return v, messageapp.ErrDataInvariantViolation
	}
	return v, nil
}

func (t *txAdapter) Get(ctx context.Context, id string) (provider.Provider, error) {
	return getProvider(ctx, t.q, id)
}
func (t *txAdapter) Create(ctx context.Context, p provider.Provider) (provider.Provider, error) {
	base, err := provider.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return p, err
	}
	p.BaseURL = base
	if p.ID == "" {
		p.ID, err = t.s.newULID(time.Now())
		if err != nil {
			return p, err
		}
	} else if id, e := ulid.ParseStrict(p.ID); e != nil || id.String() != p.ID {
		return p, fmt.Errorf("provider ID must be an uppercase canonical ULID")
	}
	if p.Status == "" {
		p.Status = provider.StatusEnabled
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt, p.Version = now, now, 1
	if err = p.Validate(); err != nil {
		return p, err
	}
	fp, err := provider.OriginFingerprint(p.Protocol, p.BaseURL)
	if err != nil {
		return p, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?)`, p.ID, nullString(p.LegacyID), p.Name, p.Protocol, p.BaseURL, p.CredentialRef, p.CredentialState, p.Status, formatTime(p.CreatedAt), formatTime(p.UpdatedAt), p.Version, fp)
	if err != nil {
		return p, fmt.Errorf("create provider: %w", err)
	}
	if err = replaceModels(ctx, t.q, p.ID, p.Models); err != nil {
		return p, err
	}
	return p, nil
}
func (t *txAdapter) Update(ctx context.Context, p provider.Provider, v int64) (provider.Provider, error) {
	base, err := provider.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return p, err
	}
	p.BaseURL = base
	old, err := getProvider(ctx, t.q, p.ID)
	if err != nil {
		return p, err
	}
	if old.Version != v {
		return p, provider.ErrConflict
	}
	var oldFP string
	if err = t.q.QueryRowContext(ctx, `SELECT origin_fingerprint FROM providers WHERE id=?`, p.ID).Scan(&oldFP); err != nil {
		return p, err
	}
	verified, _ := provider.OriginFingerprint(old.Protocol, old.BaseURL)
	if oldFP != verified {
		return p, fmt.Errorf("provider origin fingerprint mismatch")
	}
	newFP, _ := provider.OriginFingerprint(p.Protocol, p.BaseURL)
	if oldFP != newFP {
		if p.CredentialRef == old.CredentialRef && old.CredentialRef != "" {
			return p, provider.ErrCredentialReentryRequired
		}
		if p.CredentialRef == "" && p.CredentialState != provider.CredentialRequiresReentry {
			return p, provider.ErrCredentialReentryRequired
		}
	}
	p.CreatedAt, p.UpdatedAt, p.Version = old.CreatedAt, time.Now().UTC(), v+1
	if err = p.Validate(); err != nil {
		return p, err
	}
	r, err := t.q.ExecContext(ctx, `UPDATE providers SET legacy_id=?,name=?,protocol=?,base_url=?,credential_ref=NULLIF(?,''),credential_state=?,status=?,updated_at=?,version=?,origin_fingerprint=? WHERE id=? AND version=? AND deleted_at IS NULL`, nullString(p.LegacyID), p.Name, p.Protocol, p.BaseURL, p.CredentialRef, p.CredentialState, p.Status, formatTime(p.UpdatedAt), p.Version, newFP, p.ID, v)
	if err != nil {
		return p, mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return p, provider.ErrConflict
	}
	if err = replaceModels(ctx, t.q, p.ID, p.Models); err != nil {
		return p, err
	}
	return p, nil
}
func (t *txAdapter) Delete(ctx context.Context, id string, v int64) error {
	now := formatTime(time.Now().UTC())
	r, err := t.q.ExecContext(ctx, `UPDATE providers SET deleted_at=?,updated_at=?,version=version+1 WHERE id=? AND version=? AND deleted_at IS NULL`, now, now, id, v)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n == 1 {
		return nil
	}
	var live int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&live); err != nil {
		return err
	}
	if live == 1 {
		return provider.ErrConflict
	}
	return provider.ErrNotFound
}

func (t *txAdapter) Idempotency(ctx context.Context, op, key string, now time.Time) (providerapp.Record, bool, error) {
	// Reclaim expiry while holding the same BEGIN IMMEDIATE lock. This makes
	// cleanup an optimization rather than a correctness dependency.
	if _, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_records WHERE operation=? AND idempotency_key=? AND expires_at<=?`, op, key, formatTime(now)); err != nil {
		return providerapp.Record{}, false, err
	}
	var r providerapp.Record
	var response, created, expires string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest,response_json,created_at,expires_at FROM idempotency_records WHERE operation=? AND idempotency_key=?`, op, key).Scan(&r.Digest, &response, &created, &expires)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Operation, r.Key, r.Response = op, key, []byte(response)
	r.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return r, false, err
	}
	r.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return r, false, err
	}
	return r, true, nil
}
func (t *txAdapter) PutIdempotency(ctx context.Context, r providerapp.Record) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES(?,?,?,?,?,?)`, r.Operation, r.Key, r.Digest, string(r.Response), formatTime(r.CreatedAt), formatTime(r.ExpiresAt))
	return err
}
func (t *txAdapter) ClaimIdempotency(ctx context.Context, c providerapp.Claim, now time.Time, limit int) (bool, error) {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_claims WHERE expires_at<=?`, formatTime(now)); err != nil {
		return false, err
	}
	var digest string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest FROM idempotency_claims WHERE operation=? AND idempotency_key=?`, c.Operation, c.Key).Scan(&digest)
	if err == nil {
		if digest != c.Digest {
			return false, providerapp.ErrIdempotencyConflict
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	var count int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM idempotency_claims`).Scan(&count); err != nil {
		return false, err
	}
	if count >= limit {
		return false, providerapp.ErrStorageBusy
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO idempotency_claims(operation,idempotency_key,request_digest,owner,expires_at) VALUES(?,?,?,?,?)`, c.Operation, c.Key, c.Digest, c.Owner, formatTime(c.ExpiresAt))
	return err == nil, err
}
func (t *txAdapter) ReleaseIdempotencyClaim(ctx context.Context, op, key, owner string) error {
	_, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_claims WHERE operation=? AND idempotency_key=? AND owner=?`, op, key, owner)
	return err
}
func (t *txAdapter) PutAudit(ctx context.Context, a providerapp.Audit) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`, a.ID, a.Action, a.AggregateID, a.Actor, string(a.Metadata), formatTime(a.CreatedAt))
	return err
}
func (t *txAdapter) PutTokenLedgerEntry(ctx context.Context, entry token.LedgerEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	_, err := t.q.ExecContext(ctx,
		`INSERT INTO token_ledger(id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at, subject_type, subject_id, tokenizer_id)
		 VALUES(?,?,?,?,?,?,?,?,?, 'message', ?, ?)`,
		entry.ID, entry.MessageID, entry.Provider, entry.Model, entry.TokenizerRevision,
		entry.TokenCount, string(entry.EstimationMethod), entry.UTF8Bytes, formatTime(entry.ComputedAt), entry.MessageID, entry.TokenizerID)
	return mapWriteError(err)
}
func (t *txAdapter) PutOutbox(ctx context.Context, e providerapp.Event) error {
	if len(e.Payload) < 2 {
		return fmt.Errorf("invalid outbox payload")
	}
	_, err := t.q.ExecContext(ctx, `INSERT INTO outbox_events(id,topic,aggregate_id,payload_json,available_at,created_at) VALUES(?,?,?,?,?,?)`, e.ID, e.Topic, e.AggregateID, string(e.Payload), formatTime(e.CreatedAt), formatTime(e.CreatedAt))
	return err
}
func (t *txAdapter) PutCredentialAdoption(ctx context.Context, ref secret.Ref, receipt string, at time.Time) error {
	if _, err := ref.Validate(); err != nil {
		return err
	}
	_, err := t.q.ExecContext(ctx, `INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at) VALUES(?,?,?,?,?,?) ON CONFLICT(credential_ref) DO UPDATE SET receipt_id=excluded.receipt_id WHERE provider_id=excluded.provider_id AND origin=excluded.origin AND protocol=excluded.protocol`, ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol, receipt, formatTime(at))
	return err
}

var _ providerapp.UnitOfWork = (*Store)(nil)
var _ providerapp.Tx = (*txAdapter)(nil)
var _ projectapp.UnitOfWork = (*Store)(nil)
var _ projectapp.Tx = (*txAdapter)(nil)
var _ sessionapp.UnitOfWork = (*Store)(nil)
var _ sessionapp.Tx = (*txAdapter)(nil)
var _ stageapp.UnitOfWork = (*Store)(nil)
var _ stageapp.Tx = (*txAdapter)(nil)
var _ messageapp.UnitOfWork = (*Store)(nil)
var _ messageapp.Tx = (*txAdapter)(nil)
