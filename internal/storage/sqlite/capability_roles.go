package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	return listCapabilityRoles(ctx, s.db)
}

func listCapabilityRoles(ctx context.Context, q sqlRunner) ([]CapabilityRoleBinding, error) {
	rows, err := q.QueryContext(ctx, `SELECT role, COALESCE(provider_id,''), COALESCE(model_id,''), allow_judge_eq_chat, updated_at FROM capability_role_bindings`)
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

var ErrCapabilityRevisionConflict = errors.New("capability settings revision conflict")

// CapabilityRolesRevision includes the persisted monotonic timestamp to detect ABA edits.
func CapabilityRolesRevision(rows []CapabilityRoleBinding) string {
	ordered := append([]CapabilityRoleBinding{}, rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Role < ordered[j].Role })
	raw, _ := json.Marshal(ordered)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) ReplaceCapabilityRoles(ctx context.Context, rows []CapabilityRoleBinding) error {
	_, err := s.replaceCapabilityRoles(ctx, rows, nil)
	return err
}
func (s *Store) CompareAndReplaceCapabilityRoles(ctx context.Context, rows []CapabilityRoleBinding, expected string) ([]CapabilityRoleBinding, error) {
	return s.replaceCapabilityRoles(ctx, rows, &expected)
}
func (s *Store) replaceCapabilityRoles(ctx context.Context, rows []CapabilityRoleBinding, expected *string) ([]CapabilityRoleBinding, error) {
	out := append([]CapabilityRoleBinding{}, rows...)
	err := s.do(ctx, func(t *txAdapter) error {
		current, err := listCapabilityRoles(ctx, t.q)
		if err != nil {
			return err
		}
		if expected != nil && (*expected == "" || CapabilityRolesRevision(current) != *expected) {
			return ErrCapabilityRevisionConflict
		}
		updated := time.Now().UTC()
		for _, row := range current {
			if !updated.After(row.UpdatedAt) {
				updated = row.UpdatedAt.Add(time.Nanosecond)
			}
		}
		if _, err = t.q.ExecContext(ctx, `DELETE FROM capability_role_bindings`); err != nil {
			return fmt.Errorf("clear capability roles: %w", err)
		}
		for i := range out {
			row := &out[i]
			row.UpdatedAt = updated
			allow := 0
			if row.AllowJudgeEqChat {
				allow = 1
			}
			var providerID, modelID any
			if row.ProviderID != "" {
				providerID = row.ProviderID
			}
			if row.ModelID != "" {
				modelID = row.ModelID
			}
			if _, err = t.q.ExecContext(ctx, `INSERT INTO capability_role_bindings(role,provider_id,model_id,allow_judge_eq_chat,updated_at) VALUES(?,?,?,?,?)`, row.Role, providerID, modelID, allow, formatTime(updated)); err != nil {
				return fmt.Errorf("write capability role %s: %w", row.Role, err)
			}
		}
		return nil
	})
	return out, err
}
