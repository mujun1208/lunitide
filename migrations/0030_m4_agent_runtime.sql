-- M4 Reliable Single-Agent Runtime: durable Run/Turn/Step/ToolCall/Observation/Event/EffectJournal,
-- Workspace registration/grant/lease, ChangeSet, CommandJob, RunPlan, Evidence, RunReview.
-- PRD lunitide-prd.html m4-deep-prd §数据模型 + module-detail §6.1 状态机集合为权威。
-- M4 约束：parent_run_id/delegation_id 不存在（M6 才引入）；任意 shell/Child/fan-out 不出现。

CREATE TABLE agent_run (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('queued','running','paused_review','paused_budget','completed','failed','cancelled','interrupted','outcome_unknown')),
    budget_json TEXT NOT NULL CHECK (json_valid(budget_json)),
    used_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(used_json)),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_agent_run_session_status ON agent_run(session_id, status);

CREATE TABLE agent_turn (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    turn_no INTEGER NOT NULL CHECK (turn_no > 0),
    status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (run_id, turn_no)
);
CREATE INDEX ix_agent_turn_run_no ON agent_turn(run_id, turn_no);

CREATE TABLE agent_step (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    turn_id TEXT NOT NULL REFERENCES agent_turn(id) ON DELETE CASCADE,
    step_no INTEGER NOT NULL CHECK (step_no > 0),
    kind TEXT NOT NULL CHECK (kind IN ('model','tool','review')),
    status TEXT NOT NULL CHECK (status IN ('pending','running','completed','failed')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (turn_id, step_no)
);
CREATE INDEX ix_agent_step_turn_no ON agent_step(turn_id, step_no);

CREATE TABLE tool_call (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    step_id TEXT NOT NULL REFERENCES agent_step(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),
    args_digest TEXT NOT NULL CHECK (length(args_digest) = 64 AND args_digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL CHECK (status IN ('proposed','policy_checked','awaiting_review','approved','running','succeeded','failed','denied','cancelled','outcome_unknown')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_tool_call_step_status ON tool_call(step_id, status);

CREATE TABLE observation (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    step_id TEXT NOT NULL REFERENCES agent_step(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),
    captured_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_observation_step_captured ON observation(step_id, captured_at);

CREATE TABLE effect_journal (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    effect_key TEXT NOT NULL UNIQUE CHECK (length(effect_key) BETWEEN 1 AND 256),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    receipt_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('prepared','committed','failed','outcome_unknown')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_effect_journal_run_status ON effect_journal(run_id, status);

CREATE TABLE run_event (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 128),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    created_at TEXT NOT NULL,
    UNIQUE (run_id, sequence)
);

CREATE TABLE workspace_registration (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    canonical_root TEXT NOT NULL UNIQUE CHECK (length(canonical_root) BETWEEN 1 AND 1024),
    root_digest TEXT NOT NULL CHECK (length(root_digest) = 64 AND root_digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL CHECK (status IN ('active','revoked')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_workspace_registration_status ON workspace_registration(status);

CREATE TABLE workspace_grant (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    registration_id TEXT NOT NULL REFERENCES workspace_registration(id) ON DELETE CASCADE,
    scope_json TEXT NOT NULL CHECK (json_valid(scope_json)),
    expires_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','expired','revoked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_workspace_grant_registration_expiry ON workspace_grant(registration_id, expires_at);

CREATE TABLE workspace_lease (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    grant_id TEXT NOT NULL REFERENCES workspace_grant(id) ON DELETE CASCADE,
    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),
    expires_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','expired','released')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_workspace_lease_grant_expiry ON workspace_lease(grant_id, expires_at);

CREATE TABLE change_set (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),
    approval_digest TEXT NOT NULL CHECK (length(approval_digest) = 64 AND approval_digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL CHECK (status IN ('draft','previewed','approved','applied','reverted','conflicted')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_change_set_run_status ON change_set(run_id, status);

CREATE TABLE command_job (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    command_spec_digest TEXT NOT NULL CHECK (length(command_spec_digest) = 64 AND command_spec_digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled','outcome_unknown')),
    exit_code INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_command_job_run_status ON command_job(run_id, status);

CREATE TABLE run_plan (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE UNIQUE,
    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),
    content_json TEXT NOT NULL CHECK (json_valid(content_json)),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);

CREATE TABLE evidence (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    source_uri TEXT NOT NULL CHECK (length(source_uri) BETWEEN 1 AND 2048),
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),
    captured_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_evidence_run_captured ON evidence(run_id, captured_at);

CREATE TABLE run_review (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    approval_digest TEXT NOT NULL CHECK (length(approval_digest) = 64 AND approval_digest NOT GLOB '*[^0-9a-f]*'),
    decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
    decided_by TEXT NOT NULL CHECK (length(decided_by) BETWEEN 1 AND 128),
    decided_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_run_review_run_decided ON run_review(run_id, decided_at);

-- M4-B: rebuild idempotency_records to admit the agent.run.* operations.
DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0030_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'session.update', 'message.append', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0030_old;
DROP TABLE idempotency_records_0030_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

-- M4-B: rebuild audit_events to admit the agent.run.* audit actions.
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0030_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'stage.created', 'stage.updated', 'message.assistant.appended', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0030_old;
DROP TABLE audit_events_0030_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
