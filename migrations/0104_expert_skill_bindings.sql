-- 0104: expert skill bindings (skills hang on the expert, not the composer)
-- and conversation-specialist kind is derived in app code (agent vs prompt_skill).

CREATE TABLE expert_skill_bindings (
    expert_id TEXT NOT NULL CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_key TEXT NOT NULL CHECK (length(skill_key) BETWEEN 1 AND 64),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 31),
    created_at TEXT NOT NULL,
    PRIMARY KEY (expert_id, skill_key)
);
CREATE INDEX ix_expert_skill_bindings_expert ON expert_skill_bindings(expert_id, ordinal, skill_key);
