// Legacy S5 governance persistence (migration 0053): m6_credential_ref,
// m6_integration, m6_api_operation, m6_field_mapping. All writes ride the
// agent-runtime single-writer transaction; lifecycle mutations are CAS on
// the version column (m6supply.ErrVersionConflict on a lost race).
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ── CredentialRef ───────────────────────────────────────────────────────────

const m6CredentialColumns = `id,provider,secret_handle,scopes,expires_at,revoked_at,version,created_at,updated_at`

func scanM6CredentialRef(s interface{ Scan(...any) error }) (m6supply.CredentialRef, error) {
	var c m6supply.CredentialRef
	var expires, revoked sql.NullString
	var created, updated string
	if err := s.Scan(&c.ID, &c.Provider, &c.SecretHandle, &c.ScopesJSON, &expires, &revoked, &c.Version, &created, &updated); err != nil {
		return c, err
	}
	if expires.Valid {
		t, err := parseRFC(expires.String)
		if err != nil {
			return c, err
		}
		c.ExpiresAt = &t
	}
	if revoked.Valid {
		t, err := parseRFC(revoked.String)
		if err != nil {
			return c, err
		}
		c.RevokedAt = &t
	}
	var err error
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	if c.UpdatedAt, err = parseRFC(updated); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutM6CredentialRef(c m6supply.CredentialRef) error {
	var expires, revoked any
	if c.ExpiresAt != nil {
		expires = rfc(*c.ExpiresAt)
	}
	if c.RevokedAt != nil {
		revoked = rfc(*c.RevokedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_credential_ref
		(id,provider,secret_handle,scopes,expires_at,revoked_at,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Provider, c.SecretHandle, c.ScopesJSON, expires, revoked, c.Version, rfc(c.CreatedAt), rfc(c.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6CredentialRef(id string) (m6supply.CredentialRef, error) {
	c, err := scanM6CredentialRef(t.tx.QueryRowContext(t.ctx, `SELECT `+m6CredentialColumns+` FROM m6_credential_ref WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m6supply.ErrNotFound
	}
	return c, err
}

// RevokeM6CredentialRef CAS-stamps revoked_at. Revoking an already-revoked
// row at its current version is idempotent (answers the stored row); a
// stale expectedVersion is ErrVersionConflict.
func (t *agentRuntimeTx) RevokeM6CredentialRef(id string, expectedVersion int64, at time.Time) (m6supply.CredentialRef, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_credential_ref
		SET revoked_at=?, version=version+1, updated_at=? WHERE id=? AND version=? AND revoked_at IS NULL`,
		rfc(at), rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.CredentialRef{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		cur, gerr := t.GetM6CredentialRef(id)
		if gerr != nil {
			return m6supply.CredentialRef{}, gerr
		}
		if cur.RevokedAt != nil {
			return cur, nil
		}
		return m6supply.CredentialRef{}, m6supply.ErrVersionConflict
	}
	return t.GetM6CredentialRef(id)
}

// ── Integration ─────────────────────────────────────────────────────────────

const m6IntegrationColumns = `id,name,kind,base_url,spec_digest,spec_version,auth_type,credential_ref_id,direction,role,environment_bindings,state,version,created_at,updated_at`

func scanM6Integration(s interface{ Scan(...any) error }) (m6supply.Integration, error) {
	var i m6supply.Integration
	var baseURL, credRef sql.NullString
	var created, updated string
	if err := s.Scan(&i.ID, &i.Name, &i.Kind, &baseURL, &i.SpecDigest, &i.SpecVersion, &i.AuthType, &credRef,
		&i.Direction, &i.Role, &i.EnvironmentBindings, &i.State, &i.Version, &created, &updated); err != nil {
		return i, err
	}
	i.BaseURL = baseURL.String
	i.CredentialRefID = credRef.String
	var err error
	if i.CreatedAt, err = parseRFC(created); err != nil {
		return i, err
	}
	if i.UpdatedAt, err = parseRFC(updated); err != nil {
		return i, err
	}
	return i, nil
}

func (t *agentRuntimeTx) PutM6Integration(i m6supply.Integration) error {
	var baseURL, credRef any
	if i.BaseURL != "" {
		baseURL = i.BaseURL
	}
	if i.CredentialRefID != "" {
		credRef = i.CredentialRefID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_integration
		(id,name,kind,base_url,spec_digest,spec_version,auth_type,credential_ref_id,direction,role,environment_bindings,state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.ID, i.Name, i.Kind, baseURL, i.SpecDigest, i.SpecVersion, i.AuthType, credRef,
		i.Direction, i.Role, i.EnvironmentBindings, i.State, i.Version, rfc(i.CreatedAt), rfc(i.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6Integration(id string) (m6supply.Integration, error) {
	i, err := scanM6Integration(t.tx.QueryRowContext(t.ctx, `SELECT `+m6IntegrationColumns+` FROM m6_integration WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m6supply.ErrNotFound
	}
	return i, err
}

func (t *agentRuntimeTx) FindM6IntegrationByName(name, specVersion string) (m6supply.Integration, error) {
	i, err := scanM6Integration(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m6IntegrationColumns+` FROM m6_integration WHERE name=? AND spec_version=?`, name, specVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m6supply.ErrNotFound
	}
	return i, err
}

// TransitionM6Integration CAS-updates the state, bumping the version.
func (t *agentRuntimeTx) TransitionM6Integration(id string, expectedVersion int64, to string, at time.Time) (m6supply.Integration, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_integration SET state=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		to, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.Integration{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Integration{}, m6supply.ErrVersionConflict
	}
	return t.GetM6Integration(id)
}

func (t *agentRuntimeTx) ListM6Integrations() ([]m6supply.Integration, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6IntegrationColumns+` FROM m6_integration ORDER BY created_at`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.Integration
	for rows.Next() {
		i, err := scanM6Integration(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ── ApiOperation ────────────────────────────────────────────────────────────

const m6OperationColumns = `id,integration_id,operation_id,method,path_template,input_schema,output_schema,risk,enabled,pagination_spec,retry_spec,idempotency_spec,version,created_at,updated_at`

func scanM6ApiOperation(s interface{ Scan(...any) error }) (m6supply.ApiOperation, error) {
	var o m6supply.ApiOperation
	var pagination, retry, idem sql.NullString
	var created, updated string
	var enabled int
	if err := s.Scan(&o.ID, &o.IntegrationID, &o.OperationID, &o.Method, &o.PathTemplate,
		&o.InputSchemaJSON, &o.OutputSchemaJSON, &o.Risk, &enabled,
		&pagination, &retry, &idem, &o.Version, &created, &updated); err != nil {
		return o, err
	}
	o.Enabled = enabled == 1
	o.PaginationSpecJSON = pagination.String
	o.RetrySpecJSON = retry.String
	o.IdempotencySpecJSON = idem.String
	var err error
	if o.CreatedAt, err = parseRFC(created); err != nil {
		return o, err
	}
	if o.UpdatedAt, err = parseRFC(updated); err != nil {
		return o, err
	}
	return o, nil
}

func (t *agentRuntimeTx) PutM6ApiOperation(o m6supply.ApiOperation) error {
	var pagination, retry, idem any
	if o.PaginationSpecJSON != "" {
		pagination = o.PaginationSpecJSON
	}
	if o.RetrySpecJSON != "" {
		retry = o.RetrySpecJSON
	}
	if o.IdempotencySpecJSON != "" {
		idem = o.IdempotencySpecJSON
	}
	enabled := 0
	if o.Enabled {
		enabled = 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_api_operation
		(id,integration_id,operation_id,method,path_template,input_schema,output_schema,risk,enabled,pagination_spec,retry_spec,idempotency_spec,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.ID, o.IntegrationID, o.OperationID, o.Method, o.PathTemplate,
		o.InputSchemaJSON, o.OutputSchemaJSON, o.Risk, enabled, pagination, retry, idem,
		o.Version, rfc(o.CreatedAt), rfc(o.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6ApiOperation(id string) (m6supply.ApiOperation, error) {
	o, err := scanM6ApiOperation(t.tx.QueryRowContext(t.ctx, `SELECT `+m6OperationColumns+` FROM m6_api_operation WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return o, m6supply.ErrNotFound
	}
	return o, err
}

func (t *agentRuntimeTx) FindM6ApiOperationByOperationID(integrationID, operationID string) (m6supply.ApiOperation, error) {
	o, err := scanM6ApiOperation(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m6OperationColumns+` FROM m6_api_operation WHERE integration_id=? AND operation_id=?`, integrationID, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return o, m6supply.ErrNotFound
	}
	return o, err
}

// SetM6ApiOperationEnabled CAS-flips the enabled gate, bumping the version.
func (t *agentRuntimeTx) SetM6ApiOperationEnabled(id string, expectedVersion int64, enabled bool, at time.Time) (m6supply.ApiOperation, error) {
	v := 0
	if enabled {
		v = 1
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_api_operation SET enabled=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		v, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.ApiOperation{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ApiOperation{}, m6supply.ErrVersionConflict
	}
	return t.GetM6ApiOperation(id)
}

func (t *agentRuntimeTx) ListM6ApiOperations(integrationID string) ([]m6supply.ApiOperation, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6OperationColumns+` FROM m6_api_operation WHERE integration_id=? ORDER BY created_at`, integrationID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.ApiOperation
	for rows.Next() {
		o, err := scanM6ApiOperation(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ── FieldMapping ────────────────────────────────────────────────────────────

const m6MappingColumns = `id,operation_id,source,target,direction,required,transform_id,default_value,schema_version,created_at`

func scanM6FieldMapping(s interface{ Scan(...any) error }) (m6supply.FieldMapping, error) {
	var m m6supply.FieldMapping
	var transform, defValue sql.NullString
	var created string
	var required int
	if err := s.Scan(&m.ID, &m.OperationRowID, &m.Source, &m.Target, &m.Direction, &required,
		&transform, &defValue, &m.SchemaVersion, &created); err != nil {
		return m, err
	}
	m.Required = required == 1
	m.TransformID = transform.String
	m.DefaultValueJSON = defValue.String
	var err error
	if m.CreatedAt, err = parseRFC(created); err != nil {
		return m, err
	}
	return m, nil
}

func (t *agentRuntimeTx) PutM6FieldMapping(m m6supply.FieldMapping) error {
	var transform, defValue any
	if m.TransformID != "" {
		transform = m.TransformID
	}
	if m.DefaultValueJSON != "" {
		defValue = m.DefaultValueJSON
	}
	required := 0
	if m.Required {
		required = 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_field_mapping
		(id,operation_id,source,target,direction,required,transform_id,default_value,schema_version,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.OperationRowID, m.Source, m.Target, m.Direction, required, transform, defValue, m.SchemaVersion, rfc(m.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindM6FieldMapping(operationRowID, source, target, direction string) (m6supply.FieldMapping, error) {
	m, err := scanM6FieldMapping(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m6MappingColumns+` FROM m6_field_mapping WHERE operation_id=? AND source=? AND target=? AND direction=?`,
		operationRowID, source, target, direction))
	if errors.Is(err, sql.ErrNoRows) {
		return m, m6supply.ErrNotFound
	}
	return m, err
}

func (t *agentRuntimeTx) ListM6FieldMappings(operationRowID string) ([]m6supply.FieldMapping, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6MappingColumns+` FROM m6_field_mapping WHERE operation_id=? ORDER BY created_at`, operationRowID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.FieldMapping
	for rows.Next() {
		m, err := scanM6FieldMapping(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
