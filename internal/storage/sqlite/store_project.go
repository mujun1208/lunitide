package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
)

func (s *Store) ListProjects(ctx context.Context, filter project.Filter) ([]project.Project, error) {
	query := `SELECT ` + projectColumns + ` FROM projects`
	args := []any{}
	conditions := []string{`COALESCE(org_id,'')=?`}
	args = append(args, filter.OrgID)
	if filter.Status != "" {
		conditions = append(conditions, `status=?`)
		args = append(args, filter.Status)
	} else {
		conditions = append(conditions, `status!='archived'`)
	}
	if filter.Type != "" {
		conditions = append(conditions, `project_type=?`)
		args = append(args, filter.Type)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY created_at,id LIMIT 101`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []project.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("project data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (project.Project, error) {
	var p project.Project
	var created, updated string
	var orgID, spaceID sql.NullString
	row := s.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id=?`, id)
	if err := row.Scan(&p.ID, &p.Name, &p.ProjectCode, &p.Type, &p.Description, &p.Summary, &p.Objective, &p.Client, &p.ContractNo, &p.Amount, &p.Budget, &p.PlanStart, &p.PlanEnd, &p.Remark, &p.CloseReason, &p.StatusBeforeClose, &p.ReopenReason, &p.Status, &created, &updated, &p.Version, &orgID, &spaceID, &p.RootPath, &p.TreeStatus, &p.TreeDigest, &p.TreeGeneratedAt, &p.DefaultExecutor, &p.RulesDigest, &p.RulesMaterializedAt, &p.DBStatus, &p.DBPath, &p.DBDigest, &p.DBVerifiedAt); err != nil {
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
	p.OrgID = optionalProjectID(orgID)
	p.SpaceID = optionalProjectID(spaceID)
	p.Status = project.NormalizeStatus(p.Status)
	return p, p.Validate()
}

func (s *Store) ProjectHasArtifacts(ctx context.Context, projectID string) (bool, error) {
	var messageCount int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages m INNER JOIN sessions s ON s.id=m.session_id WHERE s.project_id=?`, projectID).Scan(&messageCount)
	if err != nil {
		return false, err
	}
	if messageCount > 0 {
		return true, nil
	}
	var deliverableCount int
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_deliverables WHERE project_id=?`, projectID).Scan(&deliverableCount)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, err
	}
	if deliverableCount > 0 {
		return true, nil
	}
	var attachmentCount int
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_attachments WHERE project_id=?`, projectID).Scan(&attachmentCount)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, err
	}
	return attachmentCount > 0, nil
}

func scanProject(rows *sql.Rows) (project.Project, error) {
	var p project.Project
	var created, updated string
	var orgID, spaceID sql.NullString
	if err := rows.Scan(&p.ID, &p.Name, &p.ProjectCode, &p.Type, &p.Description, &p.Summary, &p.Objective, &p.Client, &p.ContractNo, &p.Amount, &p.Budget, &p.PlanStart, &p.PlanEnd, &p.Remark, &p.CloseReason, &p.StatusBeforeClose, &p.ReopenReason, &p.Status, &created, &updated, &p.Version, &orgID, &spaceID, &p.RootPath, &p.TreeStatus, &p.TreeDigest, &p.TreeGeneratedAt, &p.DefaultExecutor, &p.RulesDigest, &p.RulesMaterializedAt, &p.DBStatus, &p.DBPath, &p.DBDigest, &p.DBVerifiedAt); err != nil {
		return p, err
	}
	var err error
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return p, err
	}
	if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return p, err
	}
	p.OrgID = optionalProjectID(orgID)
	p.SpaceID = optionalProjectID(spaceID)
	p.Status = project.NormalizeStatus(p.Status)
	return p, p.Validate()
}
