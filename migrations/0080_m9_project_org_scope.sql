-- 0080 M9 slice 1.5: attach projects to org scope (nullable for legacy rows).
-- org_id is stamped at create time from the bound operator org; rebinding
-- across organizations is forbidden (M9-003). space_id remains optional.

ALTER TABLE projects ADD COLUMN org_id TEXT REFERENCES organizations(org_id);
ALTER TABLE projects ADD COLUMN space_id TEXT REFERENCES team_spaces(space_id);

CREATE TRIGGER trg_proj_org_immutable BEFORE UPDATE ON projects
    WHEN NEW.org_id IS NOT NULL AND OLD.org_id IS NOT NULL AND NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;

CREATE INDEX idx_projects_org ON projects(org_id, status, created_at, id);
