CREATE TABLE office_storage_control (
    id INTEGER PRIMARY KEY CHECK (id=1),
    revision INTEGER NOT NULL CHECK (revision>=0),
    extra_bytes INTEGER NOT NULL DEFAULT 0 CHECK (extra_bytes>=0)
);
INSERT INTO office_storage_control(id,revision) VALUES(1,0);

CREATE TABLE office_blobs (
    digest TEXT PRIMARY KEY CHECK (length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
    size INTEGER NOT NULL CHECK (size>=0),
    state TEXT NOT NULL CHECK (state IN ('pending','ready','removed')),
    managed INTEGER NOT NULL CHECK (managed IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_office_blobs_sweep ON office_blobs(managed,state,updated_at,digest);

CREATE TABLE office_blob_leases (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    digest TEXT NOT NULL REFERENCES office_blobs(digest),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    owner_org_id TEXT NOT NULL,
    stage_name TEXT NOT NULL UNIQUE CHECK (length(stage_name)=32 AND substr(stage_name,1,6)='stage-'),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_office_blob_leases_digest ON office_blob_leases(digest,expires_at);

CREATE TABLE office_blob_references (
    digest TEXT NOT NULL REFERENCES office_blobs(digest),
    kind TEXT NOT NULL CHECK (kind IN ('version','validation')),
    reference_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(kind,reference_id,digest)
);
CREATE INDEX ix_office_blob_refs_digest ON office_blob_references(digest);

CREATE TABLE office_blob_events (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    digest TEXT NOT NULL REFERENCES office_blobs(digest),
    action TEXT NOT NULL CHECK (action IN ('reserved','ready','referenced','released','removed','reconciled')),
    detail_json TEXT NOT NULL CHECK (json_valid(detail_json) AND length(detail_json)<=8192),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_office_blob_events_digest ON office_blob_events(digest,created_at,id);
CREATE TRIGGER trg_office_blob_event_update BEFORE UPDATE ON office_blob_events
    BEGIN SELECT RAISE(ABORT,'OFFICE_STORAGE_EVENT_IMMUTABLE'); END;
CREATE TRIGGER trg_office_blob_event_delete BEFORE DELETE ON office_blob_events
    BEGIN SELECT RAISE(ABORT,'OFFICE_STORAGE_EVENT_IMMUTABLE'); END;

CREATE TABLE office_storage_sweeps (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    owner_org_id TEXT NOT NULL,
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest)=64),
    has_more INTEGER NOT NULL CHECK (has_more IN (0,1)),
    report_json TEXT CHECK (report_json IS NULL OR json_valid(report_json)),
    created_at TEXT NOT NULL,
    UNIQUE(owner_org_id,request_key)
);
CREATE TABLE office_storage_sweep_items (
    sweep_id TEXT NOT NULL REFERENCES office_storage_sweeps(id),
    digest TEXT NOT NULL REFERENCES office_blobs(digest),
    ordinal INTEGER NOT NULL CHECK (ordinal>=0),
    result_json TEXT CHECK (result_json IS NULL OR json_valid(result_json)),
    PRIMARY KEY(sweep_id,digest),
    UNIQUE(sweep_id,ordinal)
);
CREATE TRIGGER trg_office_sweep_request_immutable BEFORE UPDATE OF owner_org_id,request_key,request_digest,has_more,created_at ON office_storage_sweeps
    BEGIN SELECT RAISE(ABORT,'OFFICE_STORAGE_EVENT_IMMUTABLE'); END;
CREATE TRIGGER trg_office_sweep_report_immutable BEFORE UPDATE OF report_json ON office_storage_sweeps
    WHEN OLD.report_json IS NOT NULL BEGIN SELECT RAISE(ABORT,'OFFICE_STORAGE_EVENT_IMMUTABLE'); END;
CREATE TRIGGER trg_office_sweep_result_immutable BEFORE UPDATE ON office_storage_sweep_items
    WHEN OLD.result_json IS NOT NULL BEGIN SELECT RAISE(ABORT,'OFFICE_STORAGE_EVENT_IMMUTABLE'); END;

INSERT INTO office_blobs(digest,size,state,managed,created_at,updated_at)
SELECT v.content_ref,max(v.size),'ready',0,min(v.created_at),max(v.created_at)
FROM artifact_versions v JOIN office_version_metadata m ON m.version_id=v.id
WHERE length(v.content_ref)=64 AND v.content_ref NOT GLOB '*[^0-9a-f]*'
GROUP BY v.content_ref;
INSERT INTO office_blob_references(digest,kind,reference_id,created_at)
SELECT v.content_ref,'version',v.id,v.created_at
FROM artifact_versions v JOIN office_version_metadata m ON m.version_id=v.id
WHERE length(v.content_ref)=64 AND v.content_ref NOT GLOB '*[^0-9a-f]*';

INSERT OR IGNORE INTO office_blobs(digest,size,state,managed,created_at,updated_at)
SELECT json_extract(evidence_json,'$.pdfRef'),0,'ready',0,created_at,created_at
FROM office_validation_runs
WHERE json_type(evidence_json,'$.pdfRef')='text'
AND length(json_extract(evidence_json,'$.pdfRef'))=64
AND json_extract(evidence_json,'$.pdfRef') NOT GLOB '*[^0-9a-f]*';
INSERT INTO office_blob_references(digest,kind,reference_id,created_at)
SELECT json_extract(evidence_json,'$.pdfRef'),'validation',id,created_at
FROM office_validation_runs
WHERE json_type(evidence_json,'$.pdfRef')='text'
AND length(json_extract(evidence_json,'$.pdfRef'))=64
AND json_extract(evidence_json,'$.pdfRef') NOT GLOB '*[^0-9a-f]*';
