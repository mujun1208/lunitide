-- M4 P0: durable, action-bound and one-shot review authorization.
ALTER TABLE run_review ADD COLUMN action TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN resource_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN base_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN config_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN policy_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN descriptor_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE run_review ADD COLUMN consumed_at TEXT;
CREATE UNIQUE INDEX ux_run_review_approval_consume ON run_review(run_id, approval_digest, action) WHERE decision='approved';
