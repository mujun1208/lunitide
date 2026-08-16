// m9_org.go - SQLite implementation of the org.Store contract (T-9.1.1).
//
// ADR-011 repository rule: every entity-resolving statement binds org_id = ?
// into its predicate. Unknown in-org ids surface as sql.ErrNoRows which the
// org package folds into the uniform M9-003 answer.
package sqlite

import (
	"context"
	"database/sql"

	"github.com/lunitide/lunitide/internal/org"
)

// OrgStorage satisfies org.Store over the M9 0069 tables.
type OrgStorage struct {
	db *sql.DB
}

func NewOrgStorage(db *sql.DB) *OrgStorage { return &OrgStorage{db: db} }

// OrgStorage adapts the shared Store database onto the org contract.
func (s *Store) OrgStorage() *OrgStorage { return &OrgStorage{db: s.db} }

func scanOrg(row interface{ Scan(...any) error }) (org.Organization, error) {
	var o org.Organization
	var digest sql.NullString
	if err := row.Scan(&o.OrgID, &o.Name, &o.State, &o.RetentionDays, &digest, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return o, err
	}
	o.ResidencyPolicyDigest = digest.String
	return o, nil
}

func (s *OrgStorage) CreateOrg(ctx context.Context, o org.Organization) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO organizations
		(org_id,name,state,retention_days,residency_policy_digest,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		o.OrgID, o.Name, o.State, o.RetentionDays, nullDigest(o.ResidencyPolicyDigest), o.CreatedAt, o.UpdatedAt)
	return err
}

func (s *OrgStorage) OrgByID(ctx context.Context, orgID string) (org.Organization, error) {
	return scanOrg(s.db.QueryRowContext(ctx,
		`SELECT org_id,name,state,retention_days,residency_policy_digest,created_at,updated_at
		 FROM organizations WHERE org_id = ?`, orgID))
}

func (s *OrgStorage) UpdateOrgState(ctx context.Context, orgID, state, updatedAt string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE organizations SET state = ?, updated_at = ? WHERE org_id = ?`, state, updatedAt, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *OrgStorage) ListOrgs(ctx context.Context) ([]org.Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT org_id,name,state,retention_days,residency_policy_digest,created_at,updated_at
		 FROM organizations ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []org.Organization
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func scanSpace(row interface{ Scan(...any) error }) (org.TeamSpace, error) {
	var sp org.TeamSpace
	err := row.Scan(&sp.SpaceID, &sp.OrgID, &sp.Name, &sp.State, &sp.CreatedAt, &sp.UpdatedAt)
	return sp, err
}

func (s *OrgStorage) CreateSpace(ctx context.Context, sp org.TeamSpace) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO team_spaces
		(space_id,org_id,name,state,created_at,updated_at) VALUES (?,?,?,?,?,?)`,
		sp.SpaceID, sp.OrgID, sp.Name, sp.State, sp.CreatedAt, sp.UpdatedAt)
	return err
}

func (s *OrgStorage) SpaceByID(ctx context.Context, orgID, spaceID string) (org.TeamSpace, error) {
	return scanSpace(s.db.QueryRowContext(ctx,
		`SELECT space_id,org_id,name,state,created_at,updated_at FROM team_spaces
		 WHERE org_id = ? AND space_id = ?`, orgID, spaceID))
}

func (s *OrgStorage) ListSpaces(ctx context.Context, orgID string) ([]org.TeamSpace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT space_id,org_id,name,state,created_at,updated_at FROM team_spaces
		 WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []org.TeamSpace
	for rows.Next() {
		sp, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func scanPrincipal(row interface{ Scan(...any) error }) (org.Principal, error) {
	var p org.Principal
	var externalID, issuer, expiresAt, revokedAt sql.NullString
	err := row.Scan(&p.PrincipalID, &p.OrgID, &externalID, &issuer, &p.DisplayName,
		&p.State, &p.BindingVersion, &expiresAt, &revokedAt, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		p.ExternalID, p.IdpIssuer, p.ExpiresAt, p.RevokedAt = externalID.String, issuer.String, expiresAt.String, revokedAt.String
	}
	return p, err
}

const principalCols = `principal_id,org_id,external_id,idp_issuer,display_name,state,binding_version,expires_at,revoked_at,created_at,updated_at`

func (s *OrgStorage) CreatePrincipal(ctx context.Context, p org.Principal, ev org.IdentityEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO principals
		(principal_id,org_id,external_id,idp_issuer,display_name,state,binding_version,expires_at,revoked_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.PrincipalID, p.OrgID, nullText(p.ExternalID), nullText(p.IdpIssuer), p.DisplayName,
		p.State, p.BindingVersion, nullText(p.ExpiresAt), nil, p.CreatedAt, p.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_events
		(event_id,org_id,principal_id,kind,binding_version,created_at) VALUES (?,?,?,?,?,?)`,
		ev.EventID, ev.OrgID, ev.PrincipalID, ev.Kind, ev.BindingVersion, ev.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *OrgStorage) PrincipalByID(ctx context.Context, orgID, principalID string) (org.Principal, error) {
	return scanPrincipal(s.db.QueryRowContext(ctx,
		`SELECT `+principalCols+` FROM principals WHERE org_id = ? AND principal_id = ?`, orgID, principalID))
}

func (s *OrgStorage) UpdatePrincipalState(ctx context.Context, orgID, principalID, state string, bumpedVersion int, ev org.IdentityEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE principals SET state = ?, binding_version = ?, updated_at = ?
		WHERE org_id = ? AND principal_id = ?`, state, bumpedVersion, ev.CreatedAt, orgID, principalID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_events
		(event_id,org_id,principal_id,kind,binding_version,created_at) VALUES (?,?,?,?,?,?)`,
		ev.EventID, ev.OrgID, ev.PrincipalID, ev.Kind, ev.BindingVersion, ev.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *OrgStorage) ListPrincipals(ctx context.Context, orgID string) ([]org.Principal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+principalCols+` FROM principals WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []org.Principal
	for rows.Next() {
		p, err := scanPrincipal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanBinding(row interface{ Scan(...any) error }) (org.RoleBinding, error) {
	var b org.RoleBinding
	err := row.Scan(&b.BindingID, &b.OrgID, &b.PrincipalID, &b.ScopeKey, &b.Role, &b.ExpiresAt, &b.State, &b.CreatedAt, &b.UpdatedAt)
	return b, err
}

const bindingCols = `binding_id,org_id,principal_id,scope_key,role,expires_at,state,created_at,updated_at`

func (s *OrgStorage) CreateBinding(ctx context.Context, b org.RoleBinding) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO role_bindings
		(binding_id,org_id,principal_id,scope_key,role,expires_at,state,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		b.BindingID, b.OrgID, b.PrincipalID, b.ScopeKey, b.Role, b.ExpiresAt, b.State, b.CreatedAt, b.UpdatedAt)
	return err
}

func (s *OrgStorage) BindingByID(ctx context.Context, orgID, bindingID string) (org.RoleBinding, error) {
	return scanBinding(s.db.QueryRowContext(ctx,
		`SELECT `+bindingCols+` FROM role_bindings WHERE org_id = ? AND binding_id = ?`, orgID, bindingID))
}

func (s *OrgStorage) ListBindings(ctx context.Context, orgID, principalID string) ([]org.RoleBinding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bindingCols+` FROM role_bindings WHERE org_id = ? AND principal_id = ? ORDER BY created_at`,
		orgID, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []org.RoleBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *OrgStorage) RevokeBinding(ctx context.Context, orgID, bindingID, updatedAt string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE role_bindings SET state = 'revoked', updated_at = ? WHERE org_id = ? AND binding_id = ?`,
		updatedAt, orgID, bindingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullText(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullDigest(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// Ensure the interface is implemented (compile-time proof).
var _ org.Store = (*OrgStorage)(nil)
