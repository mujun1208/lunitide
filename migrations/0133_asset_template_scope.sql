ALTER TABLE asset_templates ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_asset_templates_scope_created ON asset_templates(org_id, created_at DESC, id DESC);
