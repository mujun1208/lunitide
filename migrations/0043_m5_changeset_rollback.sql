-- M5 T-5.2.4: ChangeSet compensating rollback. m5_changeset_item gains
-- rollback_ref (CAS content address of the pre-apply file bytes for
-- modify/delete entries). Additive-only column per the migration policy
-- (M5/05 §4); staged entries keep NULL until apply captures the bytes.

ALTER TABLE m5_changeset_item ADD COLUMN rollback_ref TEXT CHECK (rollback_ref IS NULL OR (length(rollback_ref) = 64 AND rollback_ref NOT GLOB '*[^0-9a-f]*'));
