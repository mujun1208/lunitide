-- 0105: colleague-chat task claims (one owner per thread + task key)

CREATE TABLE expert_task_claims (
    thread_id TEXT NOT NULL CHECK (length(thread_id) = 26 AND substr(thread_id, 1, 1) GLOB '[0-7]' AND thread_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_key TEXT NOT NULL CHECK (length(task_key) BETWEEN 1 AND 128),
    expert_id TEXT NOT NULL CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    claimed_at TEXT NOT NULL,
    PRIMARY KEY (thread_id, task_key)
);
CREATE INDEX ix_expert_task_claims_expert ON expert_task_claims(expert_id, claimed_at);
