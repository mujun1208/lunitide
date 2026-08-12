-- Migration 0026: make tokenizer revision part of ledger uniqueness.
-- Multiple revisions for one tokenizer/provider/model are intentionally kept.
DROP INDEX ux_token_ledger_subject_identity;
CREATE UNIQUE INDEX ux_token_ledger_subject_identity_revision
    ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model, tokenizer_revision);
