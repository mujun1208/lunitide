-- 0093 Local remark and blocklist on the existing people_contacts table.
-- Does not add a second people system. Incoming files stay pending.
ALTER TABLE people_contacts ADD COLUMN remark TEXT NOT NULL DEFAULT '';
ALTER TABLE people_contacts ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0;
