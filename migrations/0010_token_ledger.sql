-- Token ledger: caches conservative token estimates for every message part.
-- Counts are estimates, not message truth. Model/tokenizer change invalidates them.
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
    UNIQUE (message_id, provider, model, tokenizer_revision)
);
CREATE INDEX ix_token_ledger_message ON token_ledger(message_id);
CREATE INDEX ix_token_ledger_computed ON token_ledger(computed_at);

-- Token estimation for existing messages (conservative char-ratio estimate).
INSERT INTO token_ledger(id, message_id, provider, model, tokenizer_revision, token_count, estimation_method, utf8_bytes, computed_at)
SELECT
    substr(hex(randomblob(16)), 1, 26) AS id,
    m.id AS message_id,
    '' AS provider,
    '' AS model,
    '' AS tokenizer_revision,
    CASE
        WHEN length(CAST(mp.text AS BLOB)) = 0 THEN 0
        ELSE max(1, (length(CAST(mp.text AS BLOB)) + 3) / 4)
    END AS token_count,
    'char-ratio' AS estimation_method,
    length(CAST(mp.text AS BLOB)) AS utf8_bytes,
    datetime('now') AS computed_at
FROM messages m
JOIN message_parts mp ON mp.message_id = m.id
WHERE NOT EXISTS (
    SELECT 1 FROM token_ledger tl WHERE tl.message_id = m.id
);