-- 0071 M10 记忆提名工作流：nomination 是 candidate 的富化包装——
-- 提名理由与来源会话跟随候选，确认/拒绝仍走 0061 的显式 token 链路
--（FR-02/FR-11 不变量不变）。确认/拒绝在 handler 层联动 nomination
-- 状态为 decided，0061 服务保持零侵入。

CREATE TABLE memory_nominations (
    nomination_id TEXT PRIMARY KEY CHECK (length(nomination_id) = 26 AND substr(nomination_id, 1, 1) GLOB '[0-7]' AND nomination_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    candidate_id TEXT NOT NULL REFERENCES memory_candidates(candidate_id),
    nominator TEXT NOT NULL CHECK (length(nominator) BETWEEN 1 AND 128),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    source_session_id TEXT CHECK (source_session_id IS NULL OR (length(source_session_id) = 26 AND substr(source_session_id, 1, 1) GLOB '[0-7]' AND source_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    state TEXT NOT NULL DEFAULT 'nominated' CHECK (state IN ('nominated','decided','withdrawn')),
    decided_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (candidate_id)
);

CREATE INDEX idx_nom_state ON memory_nominations(state, created_at);
