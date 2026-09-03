package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/lunitide/lunitide/internal/datasourceapp"
)

func scanDatasourceConnection(s interface{ Scan(...any) error }) (datasourceapp.Connection, error) {
	var c datasourceapp.Connection
	var verified *string
	if err := s.Scan(&c.ID, &c.Name, &c.Kind, &c.DSNSecretRef, &verified, &c.CreatedAt, &c.CreatedBy, &c.State); err != nil {
		return c, err
	}
	c.ReadOnlyVerifiedAt = verified
	if c.State == "" {
		c.State = "active"
	}
	return c, nil
}

func (s *Store) PutConnection(ctx context.Context, row datasourceapp.Connection) error {
	state := strings.TrimSpace(row.State)
	if state == "" {
		state = "active"
	}
	var verified any
	if row.ReadOnlyVerifiedAt != nil {
		verified = *row.ReadOnlyVerifiedAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO db_connections
		(id,name,kind,dsn_secret_ref,readonly_verified_at,created_at,created_by,state)
		VALUES(?,?,?,?,?,?,?,?)`,
		row.ID, row.Name, row.Kind, row.DSNSecretRef, verified, row.CreatedAt, row.CreatedBy, state)
	if isUniqueViolation(err) {
		return datasourceapp.ErrDuplicateName
	}
	return err
}

func (s *Store) GetConnection(ctx context.Context, id string) (datasourceapp.Connection, error) {
	c, err := scanDatasourceConnection(s.db.QueryRowContext(ctx,
		`SELECT id,name,kind,dsn_secret_ref,readonly_verified_at,created_at,created_by,state
		 FROM db_connections WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return c, datasourceapp.ErrNotFound
	}
	return c, err
}

func (s *Store) ListConnections(ctx context.Context) ([]datasourceapp.Connection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,kind,dsn_secret_ref,readonly_verified_at,created_at,created_by,state
		 FROM db_connections ORDER BY created_at, id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []datasourceapp.Connection{}
	for rows.Next() {
		c, err := scanDatasourceConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Store) SetConnectionVerified(ctx context.Context, id, verifiedAt string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE db_connections SET readonly_verified_at=? WHERE id=? AND state='active'`, verifiedAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return datasourceapp.ErrNotFound
	}
	return nil
}

func (s *Store) DisableConnection(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE db_connections SET state='disabled', readonly_verified_at=NULL WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return datasourceapp.ErrNotFound
	}
	return nil
}

func (s *Store) PutBinding(ctx context.Context, row datasourceapp.Binding) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO datasource_bindings
		(binding_id,owner_type,owner_id,connection_id,purpose,table_map_json,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		row.BindingID, row.OwnerType, row.OwnerID, row.ConnectionID, row.Purpose, row.TableMapJSON, row.CreatedAt)
	if isUniqueViolation(err) {
		return datasourceapp.ErrDuplicateBinding
	}
	return err
}

func (s *Store) GetBinding(ctx context.Context, id string) (datasourceapp.Binding, error) {
	var b datasourceapp.Binding
	err := s.db.QueryRowContext(ctx,
		`SELECT binding_id,owner_type,owner_id,connection_id,purpose,table_map_json,created_at
		 FROM datasource_bindings WHERE binding_id=?`, id).
		Scan(&b.BindingID, &b.OwnerType, &b.OwnerID, &b.ConnectionID, &b.Purpose, &b.TableMapJSON, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return b, datasourceapp.ErrNotFound
	}
	return b, err
}

func (s *Store) ListBindings(ctx context.Context, ownerType, ownerID string) ([]datasourceapp.Binding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT binding_id,owner_type,owner_id,connection_id,purpose,table_map_json,created_at
		 FROM datasource_bindings WHERE owner_type=? AND owner_id=? ORDER BY created_at, binding_id LIMIT 50`,
		ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []datasourceapp.Binding{}
	for rows.Next() {
		var b datasourceapp.Binding
		if err := rows.Scan(&b.BindingID, &b.OwnerType, &b.OwnerID, &b.ConnectionID, &b.Purpose, &b.TableMapJSON, &b.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}
