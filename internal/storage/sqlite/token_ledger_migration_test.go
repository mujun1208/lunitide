package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeV26RemovesLegacyTokenLedgerUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v26-token-ledger.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	// Recreate the exact pre-0027 table shape and migration boundary. This is
	// deliberately test-only; historical migration bytes remain untouched.
	db := openRaw(t, path)
	if _, err = db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
ALTER TABLE token_ledger RENAME TO token_ledger_current;
CREATE TABLE token_ledger (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),
    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),
    tokenizer_revision TEXT NOT NULL DEFAULT '' CHECK (length(tokenizer_revision) <= 64),
    token_count INTEGER NOT NULL CHECK (token_count >= 0),
    estimation_method TEXT NOT NULL CHECK (estimation_method IN ('char-ratio', 'tiktoken', 'provider-reported', 'manual')),
    utf8_bytes INTEGER NOT NULL CHECK (utf8_bytes >= 0),
    computed_at TEXT NOT NULL,
    subject_type TEXT NOT NULL DEFAULT 'message' CHECK (subject_type IN ('message', 'message_part', 'tool_result', 'summary', 'injected_instruction')),
    subject_id TEXT NOT NULL DEFAULT '',
    tokenizer_id TEXT NOT NULL DEFAULT 'lunitide-canonical-v1' CHECK (length(tokenizer_id) > 0 AND length(tokenizer_id) <= 128),
    invalidated_at TEXT,
    UNIQUE (message_id, provider, model, tokenizer_revision)
);
DROP TABLE token_ledger_current;
CREATE INDEX ix_token_ledger_message ON token_ledger(message_id);
CREATE INDEX ix_token_ledger_computed ON token_ledger(computed_at);
CREATE INDEX ix_token_ledger_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model);
CREATE INDEX ix_token_ledger_invalidation ON token_ledger(tokenizer_id, invalidated_at);
CREATE UNIQUE INDEX ux_token_ledger_subject_identity_revision
    ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model, tokenizer_revision);
DROP TABLE agent_plan_run_events;
DROP TABLE agent_plan_runs;
DROP TABLE run_review;
DROP TABLE evidence;
DROP TABLE run_plan;
DROP TABLE command_job;
DROP TABLE change_set_operation;
DROP TABLE change_set;
DROP TABLE workspace_lease;
DROP TABLE workspace_grant;
DROP TABLE workspace_registration;
DROP TABLE run_event;
DROP TABLE run_usage_reservation;
DROP TABLE effect_journal;
DROP TABLE observation;
DROP TABLE tool_call;
DROP TABLE agent_step;
DROP TABLE agent_turn;
DROP TABLE agent_run;
DROP TABLE m5_workspace_conversion;
DROP TABLE m5_changeset_item;
DROP TABLE m5_changeset;
DROP TABLE m5_artifact;
DROP TABLE m5_adhoc_workspace;
DROP TABLE m6_outbox;
DROP TABLE m6_merge_intent;
DROP TABLE m6_barrier_arrival;
DROP TABLE m6_barrier;
DROP TABLE m6_budget_account;
DROP TABLE m6_delegation;
DROP TABLE m6_cloud_task;
DROP TABLE m6_connector_catalog;
DROP TABLE m6_extension_install;
DROP TABLE m6_mcp_endpoint;
DROP TABLE m6_extension_artifact;
DROP TABLE stale_resolutions;
DROP TABLE stale_marks;
DROP TABLE gate_evaluations;
DROP TABLE checkpoints;
DROP TABLE dev_tasks;
DROP TABLE test_runs;
DROP TABLE scan_runs;
DROP TABLE artifact_derivations;
DROP TABLE reproduction_manifests;
DROP TABLE evaluation_baselines;
DROP TABLE reviews;
DROP TABLE trace_edges;
DROP TABLE stage_input_snapshots;
DROP TABLE stage_runs;
DROP TABLE stage_definitions;
DROP TABLE workflow_instances;
DROP TABLE artifact_versions;
DROP TABLE workflow_versions;
DROP TABLE rollback_attempts;
DROP TABLE deployments;
DROP TABLE migration_executions;
DROP TABLE promotions;
DROP TABLE release_packages;
DROP TABLE release_blobs;
DROP TABLE cr_revisions;
DROP TABLE m6_reconcile_decision;
DROP TABLE m6_remote_receipt;
DROP TABLE m6_worker_lease;
DROP TABLE m6_cloudrunner;
DROP TABLE m6_region_policy;
DROP TABLE m6_synthesis_record;
DROP TABLE m6_result_bundle;
DROP TABLE m6_child_manifest;
DROP TABLE m6_complexity_decision;
DROP TABLE m6_import_candidate;
DROP TABLE m6_skill_trigger;
DROP TABLE m6_skill_install;
DROP TABLE m6_skill_dependency;
DROP TABLE m6_skill_version;
DROP TABLE m6_skill;
DROP TABLE m6_call_log;
DROP TABLE m6_health_sample;
DROP TABLE m6_field_mapping;
DROP TABLE m6_api_operation;
DROP TABLE m6_integration;
DROP TABLE m6_credential_ref;
DROP TABLE update_receipts;
DROP TABLE update_installations;
DROP TABLE update_rollback_attempts;
DROP TABLE consumed_nonces;
DROP TABLE update_packages;
DROP TABLE update_channels;
DROP TABLE m7_audit_events;
DROP TABLE subagent_observations;
DROP TABLE subagent_runs;
DROP TABLE db_connections;
DROP TABLE tool_manifest_v2;
DROP TABLE tool_call_quota;
DROP TABLE tool_results;
DROP TABLE mcp_endpoint_settings;
DROP TABLE mcp_market_items;
DROP TABLE mc_confirm_tokens;
DROP TABLE mc_endpoint_usage;
DROP TABLE br_permissions;
DROP TABLE br_data_usage;
DROP TABLE br_sessions;
DROP TABLE br_settings;
DROP VIEW cc_recent_audit;
DROP TABLE cc_audit_log;
DROP TABLE cc_security_config;
DROP TABLE memory_nominations;
DROP TABLE memory_candidates;
DROP TABLE memory_facts;
DROP TABLE memory_source_leaves;
DROP TABLE memory_recall_traces;
DROP TABLE kb_collections;
DROP TABLE kb_documents;
DROP TABLE kb_chunks;
DROP TABLE ontology_snapshots;
DROP TABLE graph_nodes;
DROP TABLE graph_edges;
DROP TABLE graph_index_versions;
DROP TABLE handoffs;
DROP TABLE memory_tombstones;
DROP TABLE sync_conflicts;
DROP TABLE device_replicas;
DROP TABLE workflow_bundles;
DROP TABLE automation_runs;
DROP TABLE eligibility_snapshots;
DROP TABLE skill_candidates;
DROP TABLE workflow_candidates;
DROP TABLE candidate_evaluation_bindings;
DROP TABLE feedback_events;
DROP TABLE collab_gate_evaluations;
DROP TABLE collab_gate_decisions;
DROP TABLE plugin_bundles;
DROP TABLE plugin_installs;
DROP TABLE plugin_capability_bindings;
DROP TABLE expert_catalog;
DROP TABLE expert_versions;
DROP TABLE project_phase_expert_mounting;
DROP TABLE expert_scenario_cards;
DROP TABLE sk_category_map;
DROP TABLE queued_user_messages;
DROP TABLE memory_settings;
DROP TABLE memory_fact_flags;
DROP TABLE memory_growth_box;
DROP TABLE identity_events;
DROP TABLE role_bindings;
DROP TABLE principals;
DROP TABLE team_spaces;
DROP TABLE organizations;
DROP TABLE project_deliverables;
DROP TABLE project_attachments;
DROP TABLE asset_templates;ALTER TABLE sessions DROP COLUMN pinned;
ALTER TABLE sessions DROP COLUMN revision;
DELETE FROM schema_migrations WHERE version >= '0027_token_ledger_remove_legacy_unique.sql';
INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA0','project','ITM00001','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
INSERT INTO sessions(id,project_id,title,created_at,updated_at) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA1','01ARZ3NDEKTSV4RRFFQ69G5FA0','session','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA2','01ARZ3NDEKTSV4RRFFQ69G5FA1','assistant',1,'2026-01-01T00:00:00Z');
INSERT INTO message_parts(message_id,ordinal,type,text) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA2',1,'text','hello');
INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA1',1,1,5);
INSERT INTO message_project_usage(project_id,text_bytes) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA0',5);
UPDATE message_workspace_usage SET text_bytes=5 WHERE singleton=1;
INSERT INTO token_ledger(id,message_id,provider,model,tokenizer_revision,token_count,estimation_method,utf8_bytes,computed_at,subject_type,subject_id,tokenizer_id,invalidated_at)
VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA3','01ARZ3NDEKTSV4RRFFQ69G5FA2','provider','model','revision',11,'manual',22,'2026-01-01T00:00:00Z','message','01ARZ3NDEKTSV4RRFFQ69G5FA2','tokenizer-a','2026-01-02');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var count, bytes int
	var tokenizerID, invalidatedAt string
	if err = store.db.QueryRow(`SELECT token_count,utf8_bytes,tokenizer_id,invalidated_at FROM token_ledger WHERE id='01ARZ3NDEKTSV4RRFFQ69G5FA3'`).Scan(&count, &bytes, &tokenizerID, &invalidatedAt); err != nil {
		t.Fatal(err)
	}
	if count != 11 || bytes != 22 || tokenizerID != "tokenizer-a" || invalidatedAt != "2026-01-02" {
		t.Fatalf("ledger data changed: count=%d bytes=%d tokenizer=%q invalidated=%q", count, bytes, tokenizerID, invalidatedAt)
	}

	// These rows differ only by id and tokenizer_id. V26 rejected the second
	// row through its legacy message/provider/model/revision table constraint.
	_, err = store.db.Exec(`INSERT INTO token_ledger(id,message_id,provider,model,tokenizer_revision,token_count,estimation_method,utf8_bytes,computed_at,subject_type,subject_id,tokenizer_id,invalidated_at)
VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA4','01ARZ3NDEKTSV4RRFFQ69G5FA2','provider','model','revision',11,'manual',22,'2026-01-01T00:00:00Z','message','01ARZ3NDEKTSV4RRFFQ69G5FA2','tokenizer-b','2026-01-02')`)
	if err != nil {
		t.Fatalf("tokenizer-specific identity still blocked: %v", err)
	}

	var tableSQL string
	if err = store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='table' AND name='token_ledger'`).Scan(&tableSQL); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tableSQL, "UNIQUE") {
		t.Fatalf("legacy table UNIQUE survived rebuild: %s", tableSQL)
	}
	var indexSQL string
	if err = store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='index' AND name='ux_token_ledger_subject_identity_revision'`).Scan(&indexSQL); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"subject_type", "subject_id", "tokenizer_id", "provider", "model", "tokenizer_revision"} {
		if !strings.Contains(indexSQL, column) {
			t.Fatalf("complete identity index missing %s: %s", column, indexSQL)
		}
	}
	if _, err = store.db.Exec(`INSERT INTO token_ledger(id,message_id,token_count,estimation_method,utf8_bytes,computed_at,subject_id,tokenizer_id) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FA5','01ARZ3NDEKTSV4RRFFQ69G5FA9',1,'manual',1,'2026-01-01T00:00:00Z','missing','tokenizer-c')`); err == nil {
		t.Fatal("rebuilt token_ledger foreign key is not enforced")
	}
}
