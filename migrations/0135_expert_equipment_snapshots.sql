-- Immutable per-version skill, MCP and brain binding keys.
-- Pre-upgrade historical equipment is explicitly unknown.
CREATE TABLE expert_version_equipment (
 version_id TEXT PRIMARY KEY REFERENCES expert_versions(version_id) ON DELETE RESTRICT,
 keys_json TEXT NOT NULL CHECK(json_valid(keys_json) AND json_type(keys_json)='array' AND length(keys_json)<=16384),
 known INTEGER NOT NULL CHECK(known IN (0,1))
);
INSERT INTO expert_version_equipment(version_id,keys_json,known)
SELECT v.version_id,
 CASE WHEN v.version_id=e.current_version_id THEN (SELECT json_group_array(skill_key) FROM (SELECT skill_key FROM expert_skill_bindings WHERE expert_id=v.expert_id ORDER BY ordinal,skill_key)) ELSE '[]' END,
 CASE WHEN v.version_id=e.current_version_id THEN 1 ELSE 0 END
FROM expert_versions v JOIN expert_catalog e ON e.expert_id=v.expert_id;
CREATE INDEX idx_expert_versions_persona ON expert_versions(persona_ref);
CREATE TRIGGER trg_expert_equipment_no_update BEFORE UPDATE ON expert_version_equipment BEGIN SELECT RAISE(ABORT,'expert equipment snapshot immutable'); END;
CREATE TRIGGER trg_expert_equipment_no_delete BEFORE DELETE ON expert_version_equipment BEGIN SELECT RAISE(ABORT,'expert equipment snapshot immutable'); END;
