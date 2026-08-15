package sqlite

// M5 Artifact persistence (T-5.2.5) on the agent-runtime transaction.

import (
	"database/sql"

	"github.com/lunitide/lunitide/internal/domain/m5workspace"
)

func (t *agentRuntimeTx) PutM5Artifact(a m5workspace.Artifact) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m5_artifact
		(id,run_id,mime,size,sha256,generator,download_state,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		a.ID, a.RunID, a.Mime, a.Size, a.SHA256, a.Generator, string(a.DownloadState), rfc(a.CreatedAt))
	return t.fail(err)
}

const m5ArtifactColumns = `id,run_id,mime,size,sha256,generator,download_state,created_at`

func scanM5Artifact(s interface{ Scan(...any) error }) (m5workspace.Artifact, error) {
	var a m5workspace.Artifact
	var state, created string
	if err := s.Scan(&a.ID, &a.RunID, &a.Mime, &a.Size, &a.SHA256, &a.Generator, &state, &created); err != nil {
		return a, err
	}
	a.DownloadState = m5workspace.DownloadState(state)
	var err error
	if a.CreatedAt, err = parseRFC(created); err != nil {
		return a, err
	}
	return a, nil
}

func (t *agentRuntimeTx) GetM5Artifact(id string) (m5workspace.Artifact, error) {
	a, err := scanM5Artifact(t.tx.QueryRowContext(t.ctx, `SELECT `+m5ArtifactColumns+` FROM m5_artifact WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return a, m5workspace.ErrArtifactNotFound
	}
	return a, err
}

func (t *agentRuntimeTx) ListM5ArtifactsByRun(runID string) ([]m5workspace.Artifact, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m5ArtifactColumns+` FROM m5_artifact WHERE run_id=? ORDER BY created_at`, runID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m5workspace.Artifact
	for rows.Next() {
		a, err := scanM5Artifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TransitionM5ArtifactDownload CAS-updates the download state along the
// frozen blocked -> allowed -> downloaded line.
func (t *agentRuntimeTx) TransitionM5ArtifactDownload(id string, from, to m5workspace.DownloadState) (m5workspace.Artifact, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_artifact SET download_state=? WHERE id=? AND download_state=?`,
		string(to), id, string(from))
	if err != nil {
		return m5workspace.Artifact{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m5workspace.Artifact{}, m5workspace.ErrArtifactStateBad
	}
	return t.GetM5Artifact(id)
}
