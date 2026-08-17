-- 0072 M10 专家场景卡：专家域新聚合——场景卡是专家在某项目阶段的
-- 结构化作战预案（上下文/目标/步骤/交付），挂在 expert_catalog 之下。
-- phase_key 复用 M7/M8 固定九阶段枚举；scenario_digest = sha256(规范
-- scenario JSON)；删除仅软状态 archived（append-only 审计在 m7 台账）。

CREATE TABLE expert_scenario_cards (
    scenario_card_id TEXT PRIMARY KEY CHECK (length(scenario_card_id) = 26 AND substr(scenario_card_id, 1, 1) GLOB '[0-7]' AND scenario_card_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    expert_id TEXT NOT NULL REFERENCES expert_catalog(expert_id),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 128),
    summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 2048),
    phase_key TEXT NOT NULL CHECK (phase_key IN ('INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION','SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE','VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY','OPERATIONS_RETROSPECTIVE')),
    scenario_json TEXT NOT NULL CHECK (length(scenario_json) BETWEEN 2 AND 65536),
    scenario_digest TEXT NOT NULL CHECK (length(scenario_digest) = 64 AND scenario_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (expert_id, title)
);

CREATE INDEX idx_esc_expert ON expert_scenario_cards(expert_id, state);
