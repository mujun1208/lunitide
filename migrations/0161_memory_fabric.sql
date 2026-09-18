-- Memory Fabric canonical store. Allocation: logical memory_fabric after
-- 0160_openai_responses_protocol. Backfill copies each legacy settings row
-- once; migrated_* stays immutable after this INSERT.
CREATE TABLE memory_v2_settings (
    subject_id TEXT PRIMARY KEY CHECK (length(subject_id) BETWEEN 1 AND 128),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    capture_mode TEXT NOT NULL CHECK (capture_mode IN ('auto','manual','off')),
    last_non_off_capture_mode TEXT NOT NULL CHECK (last_non_off_capture_mode IN ('auto','manual')),
    personal_memory_enabled INTEGER NOT NULL CHECK (personal_memory_enabled IN (0,1)),
    project_memory_enabled INTEGER NOT NULL CHECK (project_memory_enabled IN (0,1)),
    migrated_memory_enabled INTEGER CHECK (migrated_memory_enabled IN (0,1)),
    migrated_capture_mode TEXT CHECK (migrated_capture_mode IN ('auto','manual','off')),
    memory_v2_write INTEGER NOT NULL DEFAULT 0 CHECK (memory_v2_write IN (0,1)),
    memory_v2_read INTEGER NOT NULL DEFAULT 0 CHECK (memory_v2_read IN (0,1)),
    memory_v2_auto_capture INTEGER NOT NULL DEFAULT 0 CHECK (memory_v2_auto_capture IN (0,1)),
    memory_v2_hybrid_recall INTEGER NOT NULL DEFAULT 0 CHECK (memory_v2_hybrid_recall IN (0,1)),
    memory_v2_consolidation INTEGER NOT NULL DEFAULT 0 CHECK (memory_v2_consolidation IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO memory_v2_settings(
    subject_id,revision,capture_mode,last_non_off_capture_mode,
    personal_memory_enabled,project_memory_enabled,
    migrated_memory_enabled,migrated_capture_mode,
    memory_v2_write,memory_v2_read,memory_v2_auto_capture,memory_v2_hybrid_recall,memory_v2_consolidation,
    created_at,updated_at
)
SELECT
    subject_id,
    1,
    CASE WHEN memory_enabled = 0 THEN 'off' ELSE capture_mode END,
    CASE WHEN capture_mode IN ('auto','manual') THEN capture_mode ELSE 'auto' END,
    1,
    1,
    memory_enabled,
    capture_mode,
    0,0,0,0,0,
    created_at,
    updated_at
FROM memory_settings;

CREATE TABLE memory_fact_heads (
    fact_id TEXT PRIMARY KEY CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    current_version INTEGER NOT NULL CHECK (current_version >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    is_forgotten INTEGER NOT NULL DEFAULT 0 CHECK (is_forgotten IN (0,1)),
    updated_at TEXT NOT NULL,
    CHECK ((scope_kind = 'user' AND scope_id = subject_id) OR scope_kind IN ('workspace','project','expert','session'))
);

CREATE INDEX ix_memory_fact_heads_scope ON memory_fact_heads(subject_id, scope_kind, scope_id, is_forgotten);

CREATE TABLE memory_content_versions (
    fact_id TEXT NOT NULL CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_version INTEGER NOT NULL CHECK (fact_version >= 1),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK (kind IN ('profile','preference','goal','constraint','decision','procedure','episode','observation','working')),
    body_ref TEXT NOT NULL DEFAULT 'body' CHECK (body_ref = 'body'),
    authority TEXT NOT NULL CHECK (authority IN ('user_explicit','tool_verified','imported_unverified','user_approved_import','derived_observation')),
    stability TEXT NOT NULL CHECK (stability IN ('transient','stable','durable')),
    importance REAL NOT NULL CHECK (importance >= 0 AND importance <= 1),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    valid_from TEXT,
    valid_to TEXT,
    observed_at TEXT,
    ingested_at TEXT NOT NULL,
    origin_plane TEXT NOT NULL CHECK (origin_plane IN ('legacy','m8','native','import')),
    origin_id TEXT NOT NULL CHECK (length(origin_id) BETWEEN 1 AND 128),
    extractor_kind TEXT NOT NULL CHECK (extractor_kind IN ('deterministic','model','import','tool')),
    extractor_model TEXT NOT NULL DEFAULT '' CHECK (length(extractor_model) <= 128),
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    PRIMARY KEY (fact_id, fact_version),
    CHECK ((scope_kind = 'user' AND scope_id = subject_id) OR scope_kind IN ('workspace','project','expert','session')),
    CHECK (valid_from IS NULL OR valid_to IS NULL OR valid_from < valid_to)
);

CREATE INDEX ix_memory_content_versions_scope ON memory_content_versions(subject_id, scope_kind, scope_id, kind, fact_id);

CREATE TABLE memory_content_bodies (
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    canonical_text TEXT NOT NULL CHECK (length(canonical_text) BETWEEN 1 AND 8192),
    canonical_json TEXT CHECK (canonical_json IS NULL OR (json_valid(canonical_json) AND length(canonical_json) <= 16384)),
    PRIMARY KEY (fact_id, fact_version),
    FOREIGN KEY (fact_id, fact_version) REFERENCES memory_content_versions(fact_id, fact_version)
);

CREATE TABLE memory_fact_supersessions (
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    fact_id TEXT NOT NULL CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    old_version INTEGER NOT NULL CHECK (old_version >= 1),
    new_version INTEGER NOT NULL CHECK (new_version >= 1),
    effective_at TEXT NOT NULL,
    event_seq INTEGER NOT NULL CHECK (event_seq >= 1),
    PRIMARY KEY (fact_id, old_version, new_version),
    UNIQUE (fact_id, event_seq),
    CHECK (old_version < new_version),
    CHECK ((scope_kind = 'user' AND scope_id = subject_id) OR scope_kind IN ('workspace','project','expert','session'))
);

CREATE TABLE memory_fact_candidate_links (
    candidate_id TEXT NOT NULL CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    relation TEXT NOT NULL CHECK (relation IN ('proposed','accepted','superseded','rejected')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (candidate_id, fact_id, fact_version)
);

CREATE TABLE memory_evidence_spans (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('user_message','tool_receipt','import_record','user_direct_entry')),
    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 512),
    start_byte INTEGER,
    end_byte INTEGER,
    quote_digest TEXT NOT NULL CHECK (length(quote_digest) = 64 AND quote_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    CHECK ((start_byte IS NULL AND end_byte IS NULL) OR (start_byte IS NOT NULL AND end_byte IS NOT NULL AND start_byte >= 0 AND end_byte > start_byte)),
    CHECK (source_kind <> 'user_direct_entry' OR (start_byte IS NULL AND end_byte IS NULL))
);

CREATE INDEX ix_memory_evidence_spans_fact ON memory_evidence_spans(fact_id, fact_version);

CREATE TABLE memory_candidate_assessments (
    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    kind TEXT NOT NULL CHECK (kind IN ('profile','preference','goal','constraint','decision','procedure','episode','observation','working')),
    decision TEXT NOT NULL CHECK (decision IN ('auto_accept','review','drop')),
    reason_codes_json TEXT NOT NULL CHECK (json_valid(reason_codes_json) AND length(reason_codes_json) <= 4096),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    novelty TEXT NOT NULL CHECK (novelty IN ('new','duplicate','conflict','unknown')),
    conflict_fact_id TEXT CHECK (conflict_fact_id IS NULL OR (length(conflict_fact_id) = 26 AND substr(conflict_fact_id, 1, 1) GLOB '[0-7]' AND conflict_fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    extractor_json TEXT NOT NULL CHECK (json_valid(extractor_json) AND length(extractor_json) <= 8192),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    created_at TEXT NOT NULL
);

CREATE TABLE memory_migration_map (
    origin_plane TEXT NOT NULL CHECK (origin_plane IN ('legacy','m8','native','import')),
    origin_id TEXT NOT NULL CHECK (length(origin_id) BETWEEN 1 AND 128),
    fact_id TEXT CHECK (fact_id IS NULL OR (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    fact_version INTEGER CHECK (fact_version IS NULL OR fact_version >= 1),
    state TEXT NOT NULL CHECK (state IN ('pending','migrated','review','failed')),
    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (origin_plane, origin_id)
);

CREATE TABLE memory_event_log (
    event_seq INTEGER PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    entity_type TEXT NOT NULL CHECK (length(entity_type) BETWEEN 1 AND 64),
    entity_id TEXT NOT NULL CHECK (length(entity_id) BETWEEN 1 AND 128),
    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 64),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND length(payload_json) <= 4096),
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    occurred_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);

CREATE INDEX ix_memory_event_log_subject ON memory_event_log(subject_id, scope_kind, scope_id, event_seq);

CREATE TABLE memory_capture_jobs (
    job_id TEXT PRIMARY KEY CHECK (length(job_id) = 26 AND substr(job_id, 1, 1) GLOB '[0-7]' AND job_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    source_message_id TEXT NOT NULL CHECK (length(source_message_id) BETWEEN 1 AND 128),
    source_revision TEXT NOT NULL CHECK (length(source_revision) BETWEEN 1 AND 128),
    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),
    priority INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL CHECK (state IN ('queued','running','deferred','succeeded','failed','cancelled')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    next_attempt_at TEXT NOT NULL,
    cursor_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(cursor_json) AND length(cursor_json) <= 4096),
    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    lease_until TEXT,
    heartbeat_at TEXT,
    fence INTEGER NOT NULL DEFAULT 0 CHECK (fence >= 0),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (subject_id, source_message_id, source_revision)
);

CREATE INDEX ix_memory_capture_jobs_claim ON memory_capture_jobs(state, next_attempt_at, job_id);

CREATE TABLE memory_capture_cursor (
    subject_id TEXT PRIMARY KEY CHECK (length(subject_id) BETWEEN 1 AND 128),
    source_message_id TEXT NOT NULL CHECK (length(source_message_id) BETWEEN 1 AND 128),
    source_revision TEXT NOT NULL CHECK (length(source_revision) BETWEEN 1 AND 128),
    cursor_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(cursor_json) AND length(cursor_json) <= 4096),
    updated_at TEXT NOT NULL
);

CREATE TABLE memory_capture_policies (
    policy_id TEXT PRIMARY KEY CHECK (length(policy_id) = 26 AND substr(policy_id, 1, 1) GLOB '[0-7]' AND policy_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    category TEXT NOT NULL CHECK (length(category) BETWEEN 1 AND 64),
    rule_json TEXT NOT NULL CHECK (json_valid(rule_json) AND length(rule_json) <= 4096),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (subject_id, category)
);

CREATE TABLE memory_model_usage (
    usage_id TEXT PRIMARY KEY CHECK (length(usage_id) = 26 AND substr(usage_id, 1, 1) GLOB '[0-7]' AND usage_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    job_id TEXT CHECK (job_id IS NULL OR (length(job_id) = 26 AND substr(job_id, 1, 1) GLOB '[0-7]' AND job_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    source_message_id TEXT NOT NULL DEFAULT '' CHECK (length(source_message_id) <= 128),
    purpose TEXT NOT NULL CHECK (purpose IN ('extract','consolidate','embed','query_embed')),
    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),
    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),
    tokenizer_id TEXT NOT NULL DEFAULT '' CHECK (length(tokenizer_id) <= 128),
    tokenizer_mode TEXT NOT NULL CHECK (tokenizer_mode IN ('exact','estimated')),
    safety_margin REAL NOT NULL CHECK (safety_margin >= 1),
    estimated_input_tokens INTEGER CHECK (estimated_input_tokens IS NULL OR estimated_input_tokens >= 0),
    reported_input_tokens INTEGER CHECK (reported_input_tokens IS NULL OR reported_input_tokens >= 0),
    reported_output_tokens INTEGER CHECK (reported_output_tokens IS NULL OR reported_output_tokens >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE memory_import_previews (
    preview_id TEXT PRIMARY KEY CHECK (length(preview_id) = 26 AND substr(preview_id, 1, 1) GLOB '[0-7]' AND preview_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    source_artifact_id TEXT NOT NULL CHECK (length(source_artifact_id) BETWEEN 1 AND 256),
    archive_digest TEXT NOT NULL CHECK (length(archive_digest) = 64 AND archive_digest NOT GLOB '*[^0-9a-f]*'),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    database_revision INTEGER NOT NULL CHECK (database_revision >= 0),
    summary_json TEXT NOT NULL CHECK (json_valid(summary_json) AND length(summary_json) <= 16384),
    state TEXT NOT NULL CHECK (state IN ('staging','ready','committed','discarded','expired','failed')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE memory_purge_grants (
    grant_digest TEXT PRIMARY KEY CHECK (length(grant_digest) = 64 AND grant_digest NOT GLOB '*[^0-9a-f]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT CHECK (scope_id IS NULL OR length(scope_id) BETWEEN 1 AND 128),
    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 64 AND snapshot_digest NOT GLOB '*[^0-9a-f]*'),
    expected_revision INTEGER NOT NULL CHECK (expected_revision >= 0),
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((scope_kind = 'user' AND scope_id IS NULL) OR (scope_kind = 'project' AND scope_id IS NOT NULL))
);

CREATE TABLE memory_source_suppressions (
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 128),
    source_revision TEXT NOT NULL CHECK (length(source_revision) BETWEEN 1 AND 128),
    start_byte INTEGER,
    end_byte INTEGER,
    fact_id TEXT CHECK (fact_id IS NULL OR (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 128),
    created_at TEXT NOT NULL,
    PRIMARY KEY (subject_id, source_id, source_revision, reason)
);

CREATE TABLE memory_budget_days (
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    utc_day TEXT NOT NULL CHECK (length(utc_day) = 10),
    limit_tokens INTEGER NOT NULL CHECK (limit_tokens >= 0),
    reserved_tokens INTEGER NOT NULL DEFAULT 0 CHECK (reserved_tokens >= 0),
    settled_tokens INTEGER NOT NULL DEFAULT 0 CHECK (settled_tokens >= 0),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    PRIMARY KEY (subject_id, utc_day)
);

CREATE TABLE memory_budget_reservations (
    job_id TEXT NOT NULL CHECK (length(job_id) = 26 AND substr(job_id, 1, 1) GLOB '[0-7]' AND job_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    attempt_id TEXT NOT NULL CHECK (length(attempt_id) BETWEEN 1 AND 64),
    maximum_tokens INTEGER NOT NULL CHECK (maximum_tokens >= 0),
    state TEXT NOT NULL CHECK (state IN ('reserved','settled','released','expired')),
    actual_tokens INTEGER CHECK (actual_tokens IS NULL OR actual_tokens >= 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY (job_id, attempt_id)
);

CREATE TABLE memory_archive_artifacts (
    artifact_id TEXT PRIMARY KEY CHECK (length(artifact_id) = 26 AND substr(artifact_id, 1, 1) GLOB '[0-7]' AND artifact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    content_ref TEXT NOT NULL CHECK (length(content_ref) BETWEEN 1 AND 512),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size INTEGER NOT NULL CHECK (size >= 0),
    state TEXT NOT NULL CHECK (state IN ('staging','sealed','expired')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE memory_archive_leases (
    lease_id TEXT PRIMARY KEY CHECK (length(lease_id) = 26 AND substr(lease_id, 1, 1) GLOB '[0-7]' AND lease_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_id TEXT NOT NULL REFERENCES memory_archive_artifacts(artifact_id),
    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),
    expires_at TEXT NOT NULL
);
