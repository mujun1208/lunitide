-- Migration 0021: Token ledger identity, subject scope, and invalidation.
--
-- ADR-005 §2 requires that every Message, content part, tool result, summary
-- and injected instruction has token-accounting metadata including:
--   - tokenizer/provider/model identity AND tokenizer revision
--   - exact-or-estimated count and estimation method
--   - computed timestamp
--   - invalidation behavior on model/tokenizer change
--
-- Migration 0010 created the ledger with only `message_id` subject scope, no
-- independent `tokenizer_id` (only `tokenizer_revision`), and no invalidation
-- tracking. This migration is forward-compatible: it adds columns with
-- defaults so existing rows remain valid, and widens the subject scope so the
-- ledger can cover MessagePart / ToolResult / Summary / injected instruction
-- in addition to Message.
--
-- Schema design notes:
--   - `subject_type` defaults to 'message' so existing rows (which all
--     reference messages.id via message_id) are correctly classified without
--     a backfill UPDATE.
--   - `subject_id` defaults to message_id so existing rows have a non-null
--     subject pointer. New writes SHOULD set subject_id to the actual subject
--     (message_part.id, tool_result.id, compaction_checkpoint.id, etc.).
--   - `tokenizer_id` defaults to 'lunitide-canonical-v1' (the frozen canonical
--     tokenizer, see internal/domain/token/token.go) so existing rows are
--     attributable. Provider-specific entries should set tokenizer_id to the
--     provider's tokenizer name (e.g. 'cl100k_base', 'o200k_base').
--   - `invalidated_at` is NULLABLE: NULL means the entry is currently valid.
--     A non-NULL timestamp records when the entry was logically invalidated
--     (e.g. model/tokenizer switch). Invalidated entries are retained for
--     audit/calibration but excluded from sum queries by the application layer.
--   - The old UNIQUE(message_id, provider, model, tokenizer_revision) is
--     preserved. A new UNIQUE(subject_type, subject_id, tokenizer_id, provider,
--     model) is added to support the wider subject scope. The application layer
--     must keep both keys consistent for message-scoped rows.

-- Step 1: Add new columns with safe defaults.
ALTER TABLE token_ledger ADD COLUMN subject_type TEXT NOT NULL DEFAULT 'message'
    CHECK (subject_type IN ('message', 'message_part', 'tool_result', 'summary', 'injected_instruction'));
ALTER TABLE token_ledger ADD COLUMN subject_id TEXT NOT NULL DEFAULT '';
ALTER TABLE token_ledger ADD COLUMN tokenizer_id TEXT NOT NULL DEFAULT 'lunitide-canonical-v1'
    CHECK (length(tokenizer_id) > 0 AND length(tokenizer_id) <= 128);
ALTER TABLE token_ledger ADD COLUMN invalidated_at TEXT;

-- Step 2: Backfill subject_id for existing rows so the new UNIQUE key is
-- well-formed. Existing rows are all message-scoped, so subject_id = message_id.
UPDATE token_ledger SET subject_id = message_id WHERE subject_id = '';

-- Step 3: Add a composite index for the new identity tuple, used by
-- invalidation and re-estimation lookups.
CREATE INDEX ix_token_ledger_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model);

-- Step 4: Add an index for invalidation queries (find all entries for a
-- given tokenizer_id that are not yet invalidated).
CREATE INDEX ix_token_ledger_invalidation ON token_ledger(tokenizer_id, invalidated_at);

-- Step 5: Add the new UNIQUE constraint. SQLite 3.37+ supports adding UNIQUE
-- constraints via CREATE UNIQUE INDEX (ALTER TABLE ADD CONSTRAINT is not
-- supported). We use a unique index to enforce the new identity tuple.
-- The old UNIQUE(message_id, provider, model, tokenizer_revision) remains as
-- the table-level constraint from migration 0010; it is redundant for
-- message-scoped rows but harmless and kept for backward compatibility.
CREATE UNIQUE INDEX ux_token_ledger_subject_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model);
