package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lunitide/lunitide/internal/org"
	"strings"
)

// AuthorizeDataResource treats personal NULL scope as a real partition.
func (s *Store) AuthorizeDataResource(ctx context.Context, kind, id, scope string) error {
	return authorizeDataResource(ctx, s.db, kind, id, scope, make(map[string]uint8))
}

func (t *agentRuntimeTx) AuthorizeDataResource(ctx context.Context, kind, id, scope string) error {
	return authorizeDataResource(ctx, t.tx, kind, id, scope, make(map[string]uint8))
}

type dataScopeQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func authorizeDataResource(ctx context.Context, db dataScopeQueryer, kind, id, scope string, visited map[string]uint8) error {
	key := kind + "/" + id
	if visited[key] == 2 {
		return nil
	}
	// Polymorphic evidence can reference other evidence. Reject legacy
	// cycles and bound work without granting an unknown parent ownership.
	if visited[key] == 1 || len(visited) >= 128 || id == "" {
		return org.ErrCrossOrgAccess
	}
	visited[key] = 1
	if strings.HasPrefix(kind, "trace:") {
		kind = strings.TrimPrefix(kind, "trace:")
		if kind == "review" {
			kind = "trace-review"
		}
	}
	parentQueries := map[string]string{
		"office-metric":        `SELECT 'office-task',task_id FROM office_metrics WHERE id=?`,
		"office-bundle":        `SELECT 'office-task',task_id FROM office_bundles WHERE id=?`,
		"office-version":       `SELECT 'office-task',b.task_id FROM artifact_versions v JOIN office_task_artifacts b ON b.artifact_id=v.artifact_id JOIN office_version_metadata m ON m.version_id=v.id WHERE v.id=?`,
		"subagent":             `SELECT 'agent-run',root_run_id FROM subagent_runs WHERE id=?`,
		"delegation":           `SELECT 'agent-run',root_id FROM m6_delegation WHERE id=?`,
		"barrier":              `SELECT 'agent-run',root_id FROM m6_barrier WHERE id=?`,
		"workflow_version":     `SELECT 'project',project_id FROM workflow_versions WHERE id=?`,
		"stage-definition":     `SELECT 'workflow_version',workflow_version_id FROM stage_definitions WHERE id=?`,
		"stage_input_snapshot": `SELECT 'stage_run',stage_run_id FROM stage_input_snapshots WHERE id=?`,
		"dev_task":             `SELECT 'stage_run',stage_run_id FROM dev_tasks WHERE id=?`,
		"test_run":             `SELECT 'dev_task',task_ref FROM test_runs WHERE id=?`,
		"scan_run":             `SELECT 'dev_task',task_ref FROM scan_runs WHERE id=?`,
		"gate-evaluation":      `SELECT 'stage_run',stage_run_id FROM gate_evaluations WHERE id=?`,
		"trace-review":         `SELECT 'trace:'||subject_type,subject_id FROM reviews WHERE id=?`,
		"artifact_version":     `SELECT CASE WHEN scope_type='session' AND EXISTS(SELECT 1 FROM office_version_metadata m WHERE m.version_id=artifact_versions.id) THEN 'office-version' WHEN scope_type='release' THEN 'release-package' WHEN scope_type='m6_root' THEN 'agent-run' ELSE scope_type END,CASE WHEN scope_type='session' AND EXISTS(SELECT 1 FROM office_version_metadata m WHERE m.version_id=artifact_versions.id) THEN id ELSE scope_id END FROM artifact_versions WHERE id=?`,
	}
	if query, ok := parentQueries[kind]; ok {
		var parentKind, parentID string
		if err := db.QueryRowContext(ctx, query, id).Scan(&parentKind, &parentID); err != nil {
			return dataResourceLookupError(err)
		}
		if err := authorizeDataResource(ctx, db, parentKind, parentID, scope, visited); err != nil {
			return err
		}
		visited[key] = 2
		return nil
	}
	dualQueries := map[string]string{
		"workflow_instance": `SELECT 'project',project_id,'workflow_version',workflow_version_id FROM workflow_instances WHERE id=?`,
		"stage_run":         `SELECT 'workflow_instance',project_workflow_instance_id,'stage-definition',stage_definition_id FROM stage_runs WHERE id=?`,
		"ontology-edge":     `SELECT 'ontology-node',source_node_id,'ontology-node',target_node_id FROM ontology_edges WHERE id=?`,
		"trace_edge":        `SELECT 'trace:'||from_type,from_id,'trace:'||to_type,to_id FROM trace_edges WHERE id=?`,
		"stale-mark":        `SELECT 'trace:'||subject_type,subject_id,'trace:trace_edge',cause_edge FROM stale_marks WHERE id=?`,
	}
	if query, ok := dualQueries[kind]; ok {
		var leftKind, leftID, rightKind, rightID string
		if err := db.QueryRowContext(ctx, query, id).Scan(&leftKind, &leftID, &rightKind, &rightID); err != nil {
			return dataResourceLookupError(err)
		}
		for _, ref := range [][2]string{{leftKind, leftID}, {rightKind, rightID}} {
			if err := authorizeDataResource(ctx, db, ref[0], ref[1], scope, visited); err != nil {
				return err
			}
		}
		visited[key] = 2
		return nil
	}
	switch kind {
	case "cr_revision":
		kind = "release-revision"
	case "release_package":
		kind = "release-package"
	}
	queries := map[string]string{
		"office-task":        `SELECT t.owner_org_id FROM office_tasks t LEFT JOIN sessions x ON x.id=t.session_id LEFT JOIN projects p ON p.id=x.project_id WHERE t.id=? AND (x.id IS NULL OR COALESCE(p.org_id,'')=t.owner_org_id)`,
		"memory":             `SELECT COALESCE(p.org_id,'') FROM memories m JOIN projects p ON p.id=m.project_id WHERE m.id=?`,
		"ontology-node":      `SELECT COALESCE(p.org_id,'') FROM ontology_nodes n JOIN projects p ON p.id=n.project_id WHERE n.id=?`,
		"checkpoint":         `SELECT COALESCE(p.org_id,'') FROM compaction_checkpoints c JOIN sessions x ON x.id=c.session_id JOIN projects p ON p.id=x.project_id WHERE c.id=?`,
		"capsule":            `SELECT COALESCE(p.org_id,'') FROM handoff_capsules c JOIN sessions x ON x.id=c.source_session_id JOIN projects p ON p.id=x.project_id WHERE c.id=?`,
		"project":            `SELECT COALESCE(org_id,'') FROM projects WHERE id=?`,
		"message":            `SELECT COALESCE(p.org_id,'') FROM messages m JOIN sessions x ON x.id=m.session_id JOIN projects p ON p.id=x.project_id WHERE m.id=?`,
		"release-revision":   `SELECT COALESCE(p.org_id,'') FROM cr_revisions r JOIN projects p ON p.id=json_extract(r.manifest_json,'$.projectId') WHERE r.id=?`,
		"release-cr":         `SELECT COALESCE(p.org_id,'') FROM cr_revisions r JOIN projects p ON p.id=json_extract(r.manifest_json,'$.projectId') WHERE r.cr_id=? ORDER BY r.revision_no DESC LIMIT 1`,
		"release-package":    `SELECT COALESCE(p.org_id,'') FROM release_packages k JOIN cr_revisions r ON r.id=k.cr_revision_id JOIN projects p ON p.id=json_extract(r.manifest_json,'$.projectId') WHERE k.id=?`,
		"release-promotion":  `SELECT COALESCE(p.org_id,'') FROM promotions x JOIN release_packages k ON k.id=x.package_id JOIN cr_revisions r ON r.id=k.cr_revision_id JOIN projects p ON p.id=json_extract(r.manifest_json,'$.projectId') WHERE x.id=?`,
		"template":           `SELECT COALESCE(org_id,'') FROM asset_templates WHERE id=?`,
		"session":            `SELECT COALESCE(p.org_id,'') FROM sessions x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"plan":               `SELECT COALESCE(p.org_id,'') FROM plans x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"node":               `SELECT COALESCE(p.org_id,'') FROM plan_nodes n JOIN plans x ON x.id=n.plan_id JOIN projects p ON p.id=x.project_id WHERE n.id=?`,
		"stage":              `SELECT COALESCE(p.org_id,'') FROM stages x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"deliverable":        `SELECT COALESCE(p.org_id,'') FROM project_deliverables x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"project-attachment": `SELECT COALESCE(p.org_id,'') FROM project_attachments x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"attachment":         `SELECT COALESCE(p.org_id,'') FROM attachments x JOIN projects p ON p.id=x.project_id WHERE x.id=?`,
		"plan-run":           `SELECT COALESCE(p.org_id,'') FROM agent_plan_runs r JOIN plans x ON x.id=r.plan_id JOIN projects p ON p.id=x.project_id WHERE r.id=?`,
		"agent-run":          `SELECT COALESCE(p.org_id,'') FROM agent_run r JOIN sessions x ON x.id=r.session_id JOIN projects p ON p.id=x.project_id WHERE r.id=?`,
		"command":            `SELECT COALESCE(p.org_id,'') FROM command_job j JOIN agent_run r ON r.id=j.run_id JOIN sessions x ON x.id=r.session_id JOIN projects p ON p.id=x.project_id WHERE j.id=?`,
		"review":             `SELECT COALESCE(p.org_id,'') FROM governance_reviews r JOIN plans x ON x.id=r.plan_id JOIN projects p ON p.id=x.project_id WHERE r.id=?`,
	}
	query, ok := queries[kind]
	if !ok {
		return org.ErrCrossOrgAccess
	}
	var owner string
	err := db.QueryRowContext(ctx, query, id).Scan(&owner)
	if err == nil && owner != scope {
		return org.ErrCrossOrgAccess
	}
	if err != nil {
		return dataResourceLookupError(err)
	}
	visited[key] = 2
	return nil
}

func dataResourceLookupError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return org.ErrCrossOrgAccess
	}
	return err
}
