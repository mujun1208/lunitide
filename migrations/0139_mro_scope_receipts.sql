-- MRO private ledgers keep existing records in the personal partition.
CREATE TABLE mro_aircraft_scoped (
    aircraft_id TEXT PRIMARY KEY CHECK (length(aircraft_id) = 26 AND substr(aircraft_id, 1, 1) GLOB '[0-7]' AND aircraft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    msn TEXT NOT NULL DEFAULT '' CHECK (length(msn) <= 32),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 64),
    config TEXT NOT NULL DEFAULT '' CHECK (length(config) <= 128),
    created_at TEXT NOT NULL
, org_id TEXT REFERENCES organizations(org_id));
INSERT INTO mro_aircraft_scoped SELECT *,NULL FROM mro_aircraft;
DROP TABLE mro_aircraft;
ALTER TABLE mro_aircraft_scoped RENAME TO mro_aircraft;
CREATE UNIQUE INDEX ix_mro_aircraft_scope_key ON mro_aircraft(COALESCE(org_id,''),tail_no);
CREATE INDEX ix_mro_aircraft_scope ON mro_aircraft(org_id);
CREATE TRIGGER trg_mro_aircraft_scope_immutable BEFORE UPDATE OF org_id ON mro_aircraft WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TABLE mro_alternates_scoped (
    pn_from TEXT NOT NULL CHECK (length(pn_from) BETWEEN 1 AND 64),
    pn_to TEXT NOT NULL CHECK (length(pn_to) BETWEEN 1 AND 64),
    cert_ok INTEGER NOT NULL CHECK (cert_ok IN (0, 1)),
    effectivity TEXT NOT NULL DEFAULT '' CHECK (length(effectivity) <= 128)
, org_id TEXT REFERENCES organizations(org_id));
INSERT INTO mro_alternates_scoped SELECT *,NULL FROM mro_alternates;
DROP TABLE mro_alternates;
ALTER TABLE mro_alternates_scoped RENAME TO mro_alternates;
CREATE UNIQUE INDEX ix_mro_alternates_scope_key ON mro_alternates(COALESCE(org_id,''),pn_from,pn_to);
CREATE INDEX ix_mro_alternates_scope ON mro_alternates(org_id);
CREATE TRIGGER trg_mro_alternates_scope_immutable BEFORE UPDATE OF org_id ON mro_alternates WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_aog_cases ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_aog_cases_scope ON mro_aog_cases(org_id);
CREATE TRIGGER trg_mro_aog_cases_scope_immutable BEFORE UPDATE OF org_id ON mro_aog_cases WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_capacity_slots ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_capacity_slots_scope ON mro_capacity_slots(org_id);
CREATE TRIGGER trg_mro_capacity_slots_scope_immutable BEFORE UPDATE OF org_id ON mro_capacity_slots WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_chem_lots ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_chem_lots_scope ON mro_chem_lots(org_id);
CREATE TRIGGER trg_mro_chem_lots_scope_immutable BEFORE UPDATE OF org_id ON mro_chem_lots WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_chem_uses ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_chem_uses_scope ON mro_chem_uses(org_id);
CREATE TRIGGER trg_mro_chem_uses_scope_immutable BEFORE UPDATE OF org_id ON mro_chem_uses WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_components ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_components_scope ON mro_components(org_id);
CREATE TRIGGER trg_mro_components_scope_immutable BEFORE UPDATE OF org_id ON mro_components WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_defect_drafts ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_defect_drafts_scope ON mro_defect_drafts(org_id);
CREATE TRIGGER trg_mro_defect_drafts_scope_immutable BEFORE UPDATE OF org_id ON mro_defect_drafts WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_due_items ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_due_items_scope ON mro_due_items(org_id);
CREATE TRIGGER trg_mro_due_items_scope_immutable BEFORE UPDATE OF org_id ON mro_due_items WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_interval_change_drafts ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_interval_change_drafts_scope ON mro_interval_change_drafts(org_id);
CREATE TRIGGER trg_mro_interval_change_drafts_scope_immutable BEFORE UPDATE OF org_id ON mro_interval_change_drafts WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_interval_rules ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_interval_rules_scope ON mro_interval_rules(org_id);
CREATE TRIGGER trg_mro_interval_rules_scope_immutable BEFORE UPDATE OF org_id ON mro_interval_rules WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_kit_items ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_kit_items_scope ON mro_kit_items(org_id);
CREATE TRIGGER trg_mro_kit_items_scope_immutable BEFORE UPDATE OF org_id ON mro_kit_items WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_kits ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_kits_scope ON mro_kits(org_id);
CREATE TRIGGER trg_mro_kits_scope_immutable BEFORE UPDATE OF org_id ON mro_kits WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_life_events ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_life_events_scope ON mro_life_events(org_id);
CREATE TRIGGER trg_mro_life_events_scope_immutable BEFORE UPDATE OF org_id ON mro_life_events WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_manual_docs ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_manual_docs_scope ON mro_manual_docs(org_id);
CREATE TRIGGER trg_mro_manual_docs_scope_immutable BEFORE UPDATE OF org_id ON mro_manual_docs WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_manuals ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_manuals_scope ON mro_manuals(org_id);
CREATE TRIGGER trg_mro_manuals_scope_immutable BEFORE UPDATE OF org_id ON mro_manuals WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_ops_todos ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_ops_todos_scope ON mro_ops_todos(org_id);
CREATE TRIGGER trg_mro_ops_todos_scope_immutable BEFORE UPDATE OF org_id ON mro_ops_todos WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TABLE mro_parts_stock_scoped (
    pn TEXT NOT NULL CHECK (length(pn) BETWEEN 1 AND 64),
    qty REAL NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'local' CHECK (length(source) BETWEEN 1 AND 32)
, org_id TEXT REFERENCES organizations(org_id));
INSERT INTO mro_parts_stock_scoped SELECT *,NULL FROM mro_parts_stock;
DROP TABLE mro_parts_stock;
ALTER TABLE mro_parts_stock_scoped RENAME TO mro_parts_stock;
CREATE UNIQUE INDEX ix_mro_parts_stock_scope_key ON mro_parts_stock(COALESCE(org_id,''),pn);
CREATE INDEX ix_mro_parts_stock_scope ON mro_parts_stock(org_id);
CREATE TRIGGER trg_mro_parts_stock_scope_immutable BEFORE UPDATE OF org_id ON mro_parts_stock WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_pirep_drafts ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_pirep_drafts_scope ON mro_pirep_drafts(org_id);
CREATE TRIGGER trg_mro_pirep_drafts_scope_immutable BEFORE UPDATE OF org_id ON mro_pirep_drafts WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_po_drafts ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_po_drafts_scope ON mro_po_drafts(org_id);
CREATE TRIGGER trg_mro_po_drafts_scope_immutable BEFORE UPDATE OF org_id ON mro_po_drafts WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_schedule_assignments ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_schedule_assignments_scope ON mro_schedule_assignments(org_id);
CREATE TRIGGER trg_mro_schedule_assignments_scope_immutable BEFORE UPDATE OF org_id ON mro_schedule_assignments WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_task_card_templates ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_task_card_templates_scope ON mro_task_card_templates(org_id);
CREATE TRIGGER trg_mro_task_card_templates_scope_immutable BEFORE UPDATE OF org_id ON mro_task_card_templates WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_tool_loans ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_tool_loans_scope ON mro_tool_loans(org_id);
CREATE TRIGGER trg_mro_tool_loans_scope_immutable BEFORE UPDATE OF org_id ON mro_tool_loans WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_tools ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_tools_scope ON mro_tools(org_id);
CREATE TRIGGER trg_mro_tools_scope_immutable BEFORE UPDATE OF org_id ON mro_tools WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_utilization_events ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_utilization_events_scope ON mro_utilization_events(org_id);
CREATE TRIGGER trg_mro_utilization_events_scope_immutable BEFORE UPDATE OF org_id ON mro_utilization_events WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_work_packages ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_work_packages_scope ON mro_work_packages(org_id);
CREATE TRIGGER trg_mro_work_packages_scope_immutable BEFORE UPDATE OF org_id ON mro_work_packages WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
ALTER TABLE mro_wp_tasks ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
CREATE INDEX ix_mro_wp_tasks_scope ON mro_wp_tasks(org_id);
CREATE TRIGGER trg_mro_wp_tasks_scope_immutable BEFORE UPDATE OF org_id ON mro_wp_tasks WHEN NEW.org_id IS NOT OLD.org_id BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_manual_docs_scope_parent_insert BEFORE INSERT ON mro_manual_docs WHEN NEW.manual_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_manuals p WHERE p.manual_id=NEW.manual_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_manual_docs_scope_parent_update BEFORE UPDATE ON mro_manual_docs WHEN NEW.manual_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_manuals p WHERE p.manual_id=NEW.manual_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_life_events_scope_parent_insert BEFORE INSERT ON mro_life_events WHEN NEW.component_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_components p WHERE p.component_id=NEW.component_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_life_events_scope_parent_update BEFORE UPDATE ON mro_life_events WHEN NEW.component_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_components p WHERE p.component_id=NEW.component_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_tool_loans_scope_parent_insert BEFORE INSERT ON mro_tool_loans WHEN NEW.tool_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_tools p WHERE p.tool_id=NEW.tool_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_tool_loans_scope_parent_update BEFORE UPDATE ON mro_tool_loans WHEN NEW.tool_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_tools p WHERE p.tool_id=NEW.tool_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_chem_uses_scope_parent_insert BEFORE INSERT ON mro_chem_uses WHEN NEW.lot_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_chem_lots p WHERE p.lot_id=NEW.lot_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_chem_uses_scope_parent_update BEFORE UPDATE ON mro_chem_uses WHEN NEW.lot_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_chem_lots p WHERE p.lot_id=NEW.lot_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_chem_lots_scope_parent_insert BEFORE INSERT ON mro_chem_lots WHEN NEW.parent_lot_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_chem_lots p WHERE p.lot_id=NEW.parent_lot_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_chem_lots_scope_parent_update BEFORE UPDATE ON mro_chem_lots WHEN NEW.parent_lot_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_chem_lots p WHERE p.lot_id=NEW.parent_lot_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_kit_items_scope_parent_insert BEFORE INSERT ON mro_kit_items WHEN NEW.kit_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_kits p WHERE p.kit_id=NEW.kit_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_kit_items_scope_parent_update BEFORE UPDATE ON mro_kit_items WHEN NEW.kit_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_kits p WHERE p.kit_id=NEW.kit_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_wp_tasks_scope_parent_insert BEFORE INSERT ON mro_wp_tasks WHEN NEW.package_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_work_packages p WHERE p.package_id=NEW.package_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_mro_wp_tasks_scope_parent_update BEFORE UPDATE ON mro_wp_tasks WHEN NEW.package_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM mro_work_packages p WHERE p.package_id=NEW.package_id AND p.org_id IS NEW.org_id) BEGIN SELECT RAISE(ABORT,'MRO_SCOPE_MISMATCH'); END;
CREATE TABLE mro_request_receipts (
    org_key TEXT NOT NULL,
    method TEXT NOT NULL,
    request_key TEXT NOT NULL,
    payload_digest TEXT NOT NULL CHECK(length(payload_digest)=64),
    response_json TEXT NOT NULL CHECK(json_valid(response_json)),
    created_at TEXT NOT NULL,
    PRIMARY KEY(org_key,method,request_key)
);
CREATE TABLE mro_publications (
    package_id TEXT PRIMARY KEY REFERENCES mro_work_packages(package_id),
    org_id TEXT REFERENCES organizations(org_id),
    evidence_digest TEXT NOT NULL CHECK(length(evidence_digest)=64),
    todos_json TEXT NOT NULL CHECK(json_valid(todos_json)),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_operation_audit (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES organizations(org_id),
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_mro_operation_audit_scope ON mro_operation_audit(org_id,created_at,id);
ALTER TABLE mro_work_packages ADD COLUMN source_refs_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(source_refs_json));
