CREATE TABLE office_metrics (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    source_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    metric_json TEXT NOT NULL CHECK (json_valid(metric_json) AND length(metric_json)<=65536),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest)=64),
    created_at TEXT NOT NULL,
    UNIQUE(task_id,idempotency_key)
);
CREATE INDEX ix_office_metrics_task ON office_metrics(task_id,created_at DESC,id);
CREATE TRIGGER trg_office_metric_update BEFORE UPDATE ON office_metrics
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_metric_delete BEFORE DELETE ON office_metrics
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;

CREATE TABLE office_bundles (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json) AND length(manifest_json)<=1048576),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest)=64),
    created_at TEXT NOT NULL,
    UNIQUE(task_id,idempotency_key)
);
CREATE INDEX ix_office_bundles_task ON office_bundles(task_id,created_at DESC,id);
CREATE TRIGGER trg_office_bundle_update BEFORE UPDATE ON office_bundles
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_bundle_delete BEFORE DELETE ON office_bundles
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
