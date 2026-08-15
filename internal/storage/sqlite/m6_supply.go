package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/m6app"
)

// sqliteNullString wraps the shared nullString helper (store.go).\nfunc sqliteNullString(v string) any { return nullString(v) }\n\n// M6 supply-chain persistence (T-6.1.x): artifact catalog, per-subject
// installs and MCP endpoint rows on the shared agent-runtime transaction.

const m6ArtifactColumns = `id,name,publisher,version,digest,signature_state,sbom_ref,manifest_json,risk,created_at`

func scanM6Artifact(s interface{ Scan(...any) error }) (m6supply.Artifact, error) {
	var a m6supply.Artifact
	var created string
	if err := s.Scan(&a.ID, &a.Name, &a.Publisher, &a.Version, &a.Digest, &a.SignatureState, &a.SBOMRef, &a.ManifestJSON, &a.Risk, &created); err != nil {
		return a, err
	}
	var err error
	if a.CreatedAt, err = parseRFC(created); err != nil {
		return a, err
	}
	return a, nil
}

func (t *agentRuntimeTx) PutM6Artifact(a m6supply.Artifact) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_extension_artifact
		(id,name,publisher,version,digest,signature_state,sbom_ref,manifest_json,risk,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Publisher, a.Version, a.Digest, a.SignatureState, a.SBOMRef, a.ManifestJSON, a.Risk, rfc(a.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6Artifact(id string) (m6supply.Artifact, error) {
	a, err := scanM6Artifact(t.tx.QueryRowContext(t.ctx, `SELECT `+m6ArtifactColumns+` FROM m6_extension_artifact WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, m6supply.ErrNotFound
	}
	return a, err
}

// SearchM6Artifacts matches the lowercase query against name/publisher and
// publisher filter exactly; risk ranking is precomputed by the caller
// (rank 1 admits low+medium). Only verified artifacts surface.
func (t *agentRuntimeTx) SearchM6Artifacts(query, publisher string, maxRiskRank int, limit int) ([]m6supply.Artifact, error) {
	if maxRiskRank < 0 {
		maxRiskRank = 0
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6ArtifactColumns+` FROM m6_extension_artifact
		WHERE signature_state='verified'
		  AND (? = '' OR instr(lower(name), ?) > 0 OR instr(lower(publisher), ?) > 0)
		  AND (? = '' OR publisher = ?)
		  AND ((risk = 'low' AND ? >= 0) OR (risk = 'medium' AND ? >= 1))
		ORDER BY publisher, name, version
		LIMIT ?`,
		query, query, query, publisher, publisher, maxRiskRank, maxRiskRank, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.Artifact
	for rows.Next() {
		a, err := scanM6Artifact(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) FindM6Artifact(publisher, name, version string) (m6supply.Artifact, error) {
	a, err := scanM6Artifact(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m6ArtifactColumns+` FROM m6_extension_artifact WHERE publisher=? AND name=? AND version=? AND signature_state='verified'`,
		publisher, name, version))
	if errors.Is(err, sql.ErrNoRows) {
		return a, m6supply.ErrNotFound
	}
	return a, err
}

const m6InstallColumns = `id,artifact_id,subject,scope,project_id,state,permission_grant,version,installed_at,updated_at`

func scanM6Install(s interface{ Scan(...any) error }) (m6supply.Install, error) {
	var i m6supply.Install
	var state, installed, updated string
	var project sql.NullString
	if err := s.Scan(&i.ID, &i.ArtifactID, &i.Subject, &i.Scope, &project, &state, &i.PermissionGrantJSON, &i.Version, &installed, &updated); err != nil {
		return i, err
	}
	if project.Valid {
		i.ProjectID = project.String
	}
	i.State = m6supply.InstallState(state)
	var err error
	if i.InstalledAt, err = parseRFC(installed); err != nil {
		return i, err
	}
	if i.UpdatedAt, err = parseRFC(updated); err != nil {
		return i, err
	}
	return i, nil
}

func (t *agentRuntimeTx) PutM6Install(i m6supply.Install) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_extension_install
		(id,artifact_id,subject,scope,project_id,state,permission_grant,version,installed_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		i.ID, i.ArtifactID, i.Subject, i.Scope, nullString(i.ProjectID), string(i.State), i.PermissionGrantJSON, i.Version, rfc(i.InstalledAt), rfc(i.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6Install(id string) (m6supply.Install, error) {
	i, err := scanM6Install(t.tx.QueryRowContext(t.ctx, `SELECT `+m6InstallColumns+` FROM m6_extension_install WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m6supply.ErrNotFound
	}
	return i, err
}

// FindM6Install resolves the live (non-terminal) install of one artifact for
// a subject; a prior quarantined install still counts so the delta history
// is retained.
func (t *agentRuntimeTx) FindM6Install(subject, artifactID string) (m6supply.Install, error) {
	i, err := scanM6Install(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m6InstallColumns+` FROM m6_extension_install WHERE subject=? AND artifact_id=? AND state != 'uninstalled' ORDER BY updated_at DESC LIMIT 1`,
		subject, artifactID))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m6supply.ErrNotFound
	}
	return i, err
}

func (t *agentRuntimeTx) ListM6Installs(subject string) ([]m6supply.Install, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6InstallColumns+` FROM m6_extension_install WHERE subject=? AND state != 'uninstalled' ORDER BY updated_at DESC`, subject)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.Install
	for rows.Next() {
		i, err := scanM6Install(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// TransitionM6Install CAS-updates the state, bumping the version.
func (t *agentRuntimeTx) TransitionM6Install(id string, expectedVersion int64, to m6supply.InstallState, at time.Time) (m6supply.Install, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_extension_install SET state=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		string(to), rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.Install{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Install{}, m6supply.ErrVersionConflict
	}
	return t.GetM6Install(id)
}

// RepointM6Install switches the install to another artifact (upgrade /
// rollback), bumping the version.
func (t *agentRuntimeTx) RepointM6Install(id string, expectedVersion int64, artifactID string, at time.Time) (m6supply.Install, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_extension_install SET artifact_id=?, state='installed', version=version+1, updated_at=? WHERE id=? AND version=?`,
		artifactID, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.Install{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Install{}, m6supply.ErrVersionConflict
	}
	return t.GetM6Install(id)
}

const m6EndpointColumns = `id,transport,url,auth_ref,capability_pin,state,version,created_at,updated_at`

func scanM6Endpoint(s interface{ Scan(...any) error }) (m6supply.Endpoint, error) {
	var e m6supply.Endpoint
	var created, updated string
	if err := s.Scan(&e.ID, &e.Transport, &e.URL, &e.AuthRef, &e.CapabilityPinJSON, &e.State, &e.Version, &created, &updated); err != nil {
		return e, err
	}
	var err error
	if e.CreatedAt, err = parseRFC(created); err != nil {
		return e, err
	}
	if e.UpdatedAt, err = parseRFC(updated); err != nil {
		return e, err
	}
	return e, nil
}

func (t *agentRuntimeTx) PutM6Endpoint(e m6supply.Endpoint) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_mcp_endpoint
		(id,transport,url,auth_ref,capability_pin,state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Transport, e.URL, e.AuthRef, e.CapabilityPinJSON, e.State, e.Version, rfc(e.CreatedAt), rfc(e.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6Endpoint(id string) (m6supply.Endpoint, error) {
	e, err := scanM6Endpoint(t.tx.QueryRowContext(t.ctx, `SELECT `+m6EndpointColumns+` FROM m6_mcp_endpoint WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m6supply.ErrNotFound
	}
	return e, err
}

func (t *agentRuntimeTx) UpdateM6EndpointState(id string, state string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_mcp_endpoint SET state=?, version=version+1, updated_at=? WHERE id=?`, state, rfc(at), id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ErrNotFound
	}
	return nil
}

func (t *agentRuntimeTx) ListM6Endpoints() ([]m6supply.Endpoint, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6EndpointColumns+` FROM m6_mcp_endpoint ORDER BY created_at`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.Endpoint
	for rows.Next() {
		e, err := scanM6Endpoint(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TransactM6 runs an m6app use case on the shared single-writer transaction.
func (r *AgentRuntimeRepository) TransactM6(ctx context.Context, fn func(m6app.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		m6tx, ok := tx.(m6app.Tx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m6app.Tx")
		}
		return fn(m6tx)
	})
}

var _ m6app.Tx = (*agentRuntimeTx)(nil)
