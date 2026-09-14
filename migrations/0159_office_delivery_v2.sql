-- 0159: office FormalDecision policies and persisted decisions (T13 persist).
CREATE TABLE office_delivery_policies (
    revision TEXT PRIMARY KEY CHECK (length(revision) BETWEEN 1 AND 64),
    policy_json TEXT NOT NULL CHECK (json_valid(policy_json) AND length(policy_json)<=65536),
    digest TEXT NOT NULL CHECK (length(digest)=64),
    created_at TEXT NOT NULL
);
CREATE TABLE office_delivery_decisions (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    owner_scope TEXT NOT NULL,
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    source_sha256 TEXT NOT NULL CHECK (length(source_sha256)=64),
    policy_revision TEXT NOT NULL REFERENCES office_delivery_policies(revision),
    evidence_digest TEXT NOT NULL CHECK (length(evidence_digest)=64),
    decision_json TEXT NOT NULL CHECK (json_valid(decision_json) AND length(decision_json)<=1048576),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_office_delivery_decisions_version ON office_delivery_decisions(version_id,policy_revision,created_at);
CREATE TRIGGER trg_office_delivery_policies_update BEFORE UPDATE ON office_delivery_policies
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_delivery_policies_delete BEFORE DELETE ON office_delivery_policies
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_delivery_decisions_update BEFORE UPDATE ON office_delivery_decisions
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_delivery_decisions_delete BEFORE DELETE ON office_delivery_decisions
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
INSERT INTO office_delivery_policies(revision,policy_json,digest,created_at) VALUES
('office-basic-v2','{"revision":"office-basic-v2","tier":"basic","requiredCheckIDs":["file-integrity","source-content","locked-facts"],"officeRequiredCheckIDs":["font-availability","actual-render"]}','99b3ce923f745a340e6fdbf8cd3a34da6ac27e0ece991118144d5a4ce34cec7d','2026-09-13T00:00:00.000000000Z'),
('office-assured-v2','{"revision":"office-assured-v2","tier":"assured","requiredCheckIDs":["file-integrity","source-content","locked-facts","font-actual","page-coverage"],"officeRequiredCheckIDs":["font-availability","actual-render"]}','e7aa1c75feca2d63dc7ac8898765f737ca4f16231508723e1ad9585abfd9a792','2026-09-13T00:00:00.000000000Z');
