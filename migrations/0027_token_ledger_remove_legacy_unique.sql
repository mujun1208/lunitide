-- Migration 0027: remove the legacy message-only table UNIQUE constraint.
--
-- The migration runner holds an exclusive transaction with foreign_keys=OFF,
-- then runs foreign_key_check and restores foreign_keys=ON after commit. That
-- runner-level handling is required because SQLite cannot change foreign_keys
-- while a transaction is active.
ALTER TABLE token_ledger RENAME TO token_ledger_legacy;

CREATE TABLE token_ledger (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),
    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),
    tokenizer_revision TEXT NOT NULL DEFAULT '' CHECK (length(tokenizer_revision) <= 64),
    token_count INTEGER NOT NULL CHECK (token_count >= 0),
    estimation_method TEXT NOT NULL CHECK (estimation_method IN ('char-ratio', 'tiktoken', 'provider-reported', 'manual')),
    utf8_bytes INTEGER NOT NULL CHECK (utf8_bytes >= 0),
    computed_at TEXT NOT NULL,
    subject_type TEXT NOT NULL DEFAULT 'message'
        CHECK (subject_type IN ('message', 'message_part', 'tool_result', 'summary', 'injected_instruction')),
    subject_id TEXT NOT NULL DEFAULT '',
    tokenizer_id TEXT NOT NULL DEFAULT 'lunitide-canonical-v1'
        CHECK (length(tokenizer_id) > 0 AND length(tokenizer_id) <= 128),
    invalidated_at TEXT
);

INSERT INTO token_ledger (
    id, message_id, provider, model, tokenizer_revision, token_count,
    estimation_method, utf8_bytes, computed_at, subject_type, subject_id,
    tokenizer_id, invalidated_at
)
SELECT
    id, message_id, provider, model, tokenizer_revision, token_count,
    estimation_method, utf8_bytes, computed_at, subject_type, subject_id,
    tokenizer_id, invalidated_at
FROM token_ledger_legacy;

DROP TABLE token_ledger_legacy;

CREATE INDEX ix_token_ledger_message ON token_ledger(message_id);
CREATE INDEX ix_token_ledger_computed ON token_ledger(computed_at);
CREATE INDEX ix_token_ledger_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model);
CREATE INDEX ix_token_ledger_invalidation ON token_ledger(tokenizer_id, invalidated_at);
CREATE UNIQUE INDEX ux_token_ledger_subject_identity_revision
    ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model, tokenizer_revision);
