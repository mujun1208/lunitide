-- 0169: settlement may record overrun and receipt_conflict.
-- 0158 only allowed reported/partial/unknown, so SettleCall CHECK-failed
-- after a thinking-token overrun and left the reservation reserved forever.
CREATE TABLE run_usage_reservation_0169 (
    id TEXT PRIMARY KEY CHECK (length(id) = 26),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    reserved_json TEXT NOT NULL CHECK (json_valid(reserved_json)),
    committed_json TEXT CHECK (committed_json IS NULL OR json_valid(committed_json)),
    status TEXT NOT NULL CHECK (status IN ('reserved','committed','released','isolated')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    task_id TEXT CHECK (task_id IS NULL OR length(task_id) BETWEEN 1 AND 64),
    scope_id TEXT CHECK (scope_id IS NULL OR length(scope_id) BETWEEN 1 AND 64),
    attempt_id TEXT CHECK (attempt_id IS NULL OR length(attempt_id) BETWEEN 1 AND 64),
    request_digest TEXT CHECK (request_digest IS NULL OR length(request_digest) BETWEEN 1 AND 128),
    dispatched INTEGER NOT NULL DEFAULT 0 CHECK (dispatched IN (0,1)),
    integrity TEXT CHECK (integrity IS NULL OR integrity IN ('reported','partial','unknown','overrun','receipt_conflict')),
    reserved_v2_json TEXT CHECK (reserved_v2_json IS NULL OR json_valid(reserved_v2_json)),
    settled_v2_json TEXT CHECK (settled_v2_json IS NULL OR json_valid(settled_v2_json)),
    receipt_id TEXT CHECK (receipt_id IS NULL OR length(receipt_id) BETWEEN 1 AND 128),
    settlement_revision INTEGER CHECK (settlement_revision IS NULL OR settlement_revision >= 1),
    settlement_digest TEXT CHECK (settlement_digest IS NULL OR length(settlement_digest) BETWEEN 1 AND 128),
    isolated_json TEXT CHECK (isolated_json IS NULL OR json_valid(isolated_json)),
    CHECK (
        (task_id IS NULL AND scope_id IS NULL AND attempt_id IS NULL)
        OR
        (task_id IS NOT NULL AND scope_id IS NOT NULL AND attempt_id IS NOT NULL)
    )
);
INSERT INTO run_usage_reservation_0169(
    id, run_id, reserved_json, committed_json, status, created_at, updated_at,
    task_id, scope_id, attempt_id, request_digest, dispatched, integrity,
    reserved_v2_json, settled_v2_json, receipt_id, settlement_revision, settlement_digest, isolated_json
)
SELECT id, run_id, reserved_json, committed_json, status, created_at, updated_at,
    task_id, scope_id, attempt_id, request_digest, dispatched, integrity,
    reserved_v2_json, settled_v2_json, receipt_id, settlement_revision, settlement_digest, isolated_json
FROM run_usage_reservation;
DROP INDEX IF EXISTS ix_run_usage_reservation_active;
DROP INDEX IF EXISTS ix_run_usage_reservation_task_status;
DROP INDEX IF EXISTS ix_run_usage_reservation_task_scope_status;
DROP INDEX IF EXISTS ix_run_usage_reservation_attempt;
DROP TABLE run_usage_reservation;
ALTER TABLE run_usage_reservation_0169 RENAME TO run_usage_reservation;
CREATE INDEX ix_run_usage_reservation_active ON run_usage_reservation(run_id,status);
CREATE INDEX ix_run_usage_reservation_task_status ON run_usage_reservation(task_id,status);
CREATE INDEX ix_run_usage_reservation_task_scope_status ON run_usage_reservation(task_id,scope_id,status);
CREATE UNIQUE INDEX ix_run_usage_reservation_attempt ON run_usage_reservation(attempt_id) WHERE attempt_id IS NOT NULL;
