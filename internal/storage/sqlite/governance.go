package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/governance"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

// CreateReview inserts a new governance review.
func (s *Store) CreateReview(ctx context.Context, r governance.Review) (governance.Review, error) {
	if r.ID == "" {
		var err error
		r.ID, err = s.newULID(time.Now())
		if err != nil {
			return r, err
		}
	}
	r.CreatedAt = time.Now().UTC()
	if r.Status == "" {
		r.Status = governance.ReviewStatusPending
	}
	if err := r.Validate(); err != nil {
		return r, err
	}
	var planID, nodeID, expiresAt, reviewedAt any
	if r.PlanID != nil {
		planID = *r.PlanID
	}
	if r.NodeID != nil {
		nodeID = *r.NodeID
	}
	if r.ExpiresAt != nil {
		expiresAt = formatTime(*r.ExpiresAt)
	}
	if r.ReviewedAt != nil {
		reviewedAt = formatTime(*r.ReviewedAt)
	}
	err := s.execWithAudit(ctx, "review.created", r.ID, "engine",
		map[string]any{"actionType": r.ActionType, "riskLevel": r.RiskLevel},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO governance_reviews(id, plan_id, node_id, action_type, action_digest,
				 input_digest, state_digest, policy_version, risk_level, status,
				 reviewer_note, expires_at, created_at, reviewed_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				r.ID, planID, nodeID, r.ActionType, r.ActionDigest,
				r.InputDigest, r.StateDigest, r.PolicyVersion, r.RiskLevel, r.Status,
				r.ReviewerNote, expiresAt, formatTime(r.CreatedAt), reviewedAt)
			return err
		})
	return r, mapWriteError(err)
}

// GetReview returns a review by ID.
func (s *Store) GetReview(ctx context.Context, id string) (*governance.Review, error) {
	var r governance.Review
	var planID, nodeID sql.NullString
	var created, expiresAt, reviewedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, plan_id, node_id, action_type, action_digest,
		 input_digest, state_digest, policy_version, risk_level, status,
		 reviewer_note, expires_at, created_at, reviewed_at
		 FROM governance_reviews WHERE id=?`, id).Scan(
		&r.ID, &planID, &nodeID, &r.ActionType, &r.ActionDigest,
		&r.InputDigest, &r.StateDigest, &r.PolicyVersion, &r.RiskLevel, &r.Status,
		&r.ReviewerNote, &expiresAt, &created, &reviewedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	if planID.Valid {
		v := planID.String
		r.PlanID = &v
	}
	if nodeID.Valid {
		v := nodeID.String
		r.NodeID = &v
	}
	if expiresAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return nil, err
		}
		r.ExpiresAt = &t
	}
	if reviewedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, reviewedAt.String)
		if err != nil {
			return nil, err
		}
		r.ReviewedAt = &t
	}
	return &r, nil
}

// ListReviewsByPlan returns reviews for a plan ordered by created_at descending.
func (s *Store) ListReviewsByPlan(ctx context.Context, planID string, limit int) ([]governance.Review, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, plan_id, node_id, action_type, action_digest,
		 input_digest, state_digest, policy_version, risk_level, status,
		 reviewer_note, expires_at, created_at, reviewed_at
		 FROM governance_reviews WHERE plan_id=? ORDER BY created_at DESC LIMIT ?`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []governance.Review
	for rows.Next() {
		var r governance.Review
		var planID, nodeID sql.NullString
		var created, expiresAt, reviewedAt sql.NullString
		if err = rows.Scan(
			&r.ID, &planID, &nodeID, &r.ActionType, &r.ActionDigest,
			&r.InputDigest, &r.StateDigest, &r.PolicyVersion, &r.RiskLevel, &r.Status,
			&r.ReviewerNote, &expiresAt, &created, &reviewedAt); err != nil {
			return nil, err
		}
		r.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		if planID.Valid {
			v := planID.String
			r.PlanID = &v
		}
		if nodeID.Valid {
			v := nodeID.String
			r.NodeID = &v
		}
		if expiresAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
			if err != nil {
				return nil, err
			}
			r.ExpiresAt = &t
		}
		if reviewedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, reviewedAt.String)
			if err != nil {
				return nil, err
			}
			r.ReviewedAt = &t
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// UpdateReviewStatus updates the status, reviewer_note, and reviewed_at of a review.
func (s *Store) UpdateReviewStatus(ctx context.Context, id string, status governance.ReviewStatus, reviewerNote string, reviewedAt *time.Time) error {
	var reviewedAtVal any
	if reviewedAt != nil {
		reviewedAtVal = formatTime(*reviewedAt)
	}
	err := s.execWithAudit(ctx, "review.status_updated", id, "engine",
		map[string]any{"status": status},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE governance_reviews SET status=?, reviewer_note=?, reviewed_at=? WHERE id=?`,
				status, reviewerNote, reviewedAtVal, id)
			if err != nil {
				return err
			}
			if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
				return provider.ErrNotFound
			}
			return nil
		})
	return mapWriteError(err)
}

// CreatePolicy inserts a new governance policy.
func (s *Store) CreatePolicy(ctx context.Context, p governance.Policy) (governance.Policy, error) {
	if p.ID == "" {
		var err error
		p.ID, err = s.newULID(time.Now())
		if err != nil {
			return p, err
		}
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Description == "" {
		p.Description = ""
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	isActive := 0
	if p.IsActive {
		isActive = 1
	}
	err := s.execWithAudit(ctx, "policy.created", p.ID, "engine",
		map[string]any{"name": p.Name, "version": p.Version},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO governance_policies(id, name, description, version, is_active, rules_json, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?)`,
				p.ID, p.Name, p.Description, p.Version, isActive, p.RulesJSON, formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
			return err
		})
	return p, mapWriteError(err)
}

// GetPolicy returns a policy by ID.
func (s *Store) GetPolicy(ctx context.Context, id string) (*governance.Policy, error) {
	var p governance.Policy
	var created, updated sql.NullString
	var isActive int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, version, is_active, rules_json, created_at, updated_at
		 FROM governance_policies WHERE id=?`, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.Version, &isActive, &p.RulesJSON, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.IsActive = isActive != 0
	p.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPolicies returns policies ordered by created_at descending.
func (s *Store) ListPolicies(ctx context.Context, activeOnly bool, limit int) ([]governance.Policy, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := `SELECT id, name, description, version, is_active, rules_json, created_at, updated_at
		 FROM governance_policies`
	var rows *sql.Rows
	var err error
	if activeOnly {
		query += ` WHERE is_active=1 ORDER BY created_at DESC LIMIT ?`
		rows, err = s.db.QueryContext(ctx, query, limit)
	} else {
		query += ` ORDER BY created_at DESC LIMIT ?`
		rows, err = s.db.QueryContext(ctx, query, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []governance.Policy
	for rows.Next() {
		var p governance.Policy
		var created, updated sql.NullString
		var isActive int64
		if err = rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.Version, &isActive, &p.RulesJSON, &created, &updated); err != nil {
			return nil, err
		}
		p.IsActive = isActive != 0
		p.CreatedAt, err = time.Parse(time.RFC3339Nano, created.String)
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated.String)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// UpdatePolicy updates the rules_json and bumps updated_at for a policy.
func (s *Store) UpdatePolicy(ctx context.Context, id string, rulesJSON string) error {
	err := s.execWithAudit(ctx, "policy.updated", id, "engine",
		map[string]any{"id": id},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE governance_policies SET rules_json=?, updated_at=? WHERE id=?`,
				rulesJSON, formatTime(time.Now().UTC()), id)
			if err != nil {
				return err
			}
			if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
				return provider.ErrNotFound
			}
			return nil
		})
	return mapWriteError(err)
}

// DeactivatePolicy sets is_active=0 and bumps updated_at for a policy.
func (s *Store) DeactivatePolicy(ctx context.Context, id string) error {
	err := s.execWithAudit(ctx, "policy.deactivated", id, "engine",
		map[string]any{"id": id},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE governance_policies SET is_active=0, updated_at=? WHERE id=?`,
				formatTime(time.Now().UTC()), id)
			if err != nil {
				return err
			}
			if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
				return provider.ErrNotFound
			}
			return nil
		})
	return mapWriteError(err)
}