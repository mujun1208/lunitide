package sqlite

import (
	"context"
	"fmt"
	"time"
)

// CapabilityRoleBinding is one of the six capability routing rows.
type CapabilityRoleBinding struct {
	Role             string
	ProviderID       string
	ModelID          string
	AllowJudgeEqChat bool
	UpdatedAt        time.Time
}

func (s *Store) ListCapabilityRoles(ctx context.Context) ([]CapabilityRoleBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, COALESCE(provider_id,''), COALESCE(model_id,''), allow_judge_eq_chat, updated_at FROM capability_role_bindings`)
	if err != nil {
		return nil, fmt.Errorf("list capability roles: %w", err)
	}
	defer rows.Close()
	var out []CapabilityRoleBinding
	for rows.Next() {
		var b CapabilityRoleBinding
		var updated string
		var allow int
		if err := rows.Scan(&b.Role, &b.ProviderID, &b.ModelID, &allow, &updated); err != nil {
			return nil, err
		}
		b.AllowJudgeEqChat = allow == 1
		if b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			if b.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
				return nil, err
			}
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceCapabilityRoles(ctx context.Context, rows []CapabilityRoleBinding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM capability_role_bindings`); err != nil {
		return fmt.Errorf("clear capability roles: %w", err)
	}
	for _, row := range rows {
		allow := 0
		if row.AllowJudgeEqChat {
			allow = 1
		}
		updated := row.UpdatedAt
		if updated.IsZero() {
			updated = time.Now().UTC()
		}
		var providerID, modelID any
		if row.ProviderID != "" {
			providerID = row.ProviderID
		}
		if row.ModelID != "" {
			modelID = row.ModelID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO capability_role_bindings(role,provider_id,model_id,allow_judge_eq_chat,updated_at) VALUES(?,?,?,?,?)`, row.Role, providerID, modelID, allow, formatTime(updated)); err != nil {
			return fmt.Errorf("write capability role %s: %w", row.Role, err)
		}
	}
	return tx.Commit()
}
