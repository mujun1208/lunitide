-- 0020_model_context_window.sql
-- Add context_window column to provider_models for dynamic context assembly.
-- Allows each model to specify its own context window (in tokens), enabling
-- the context assembler to compute accurate token budgets per model.
-- NULL means "unknown / use default 128000".

ALTER TABLE provider_models ADD COLUMN context_window INTEGER;
