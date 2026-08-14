-- ChangeSet effects finalize with a durable, globally unique receipt. Prepared
-- rows intentionally keep NULL so crash reconciliation can distinguish them.
CREATE UNIQUE INDEX ux_effect_journal_receipt
    ON effect_journal(receipt_id)
    WHERE receipt_id IS NOT NULL;
