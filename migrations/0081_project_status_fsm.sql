-- 0081: Expand project lifecycle FSM (立项/需求架构/实施中/集成测试/上线准备/系统上线)
-- and persist close/reopen audit fields.
PRAGMA legacy_alter_table=ON;
ALTER TABLE projects RENAME TO projects_0081_old;
CREATE TABLE projects (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200 AND name = trim(name)),
    project_code TEXT NOT NULL CHECK (length(project_code) BETWEEN 4 AND 16 AND project_code GLOB 'ITM[0-9]*'),
    project_type TEXT NOT NULL DEFAULT 'implementation' CHECK (project_type IN ('implementation', 'operations', 'enhancement')),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 500),
    objective TEXT NOT NULL DEFAULT '' CHECK (length(objective) <= 2000),
    client TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),
    contract_no TEXT NOT NULL DEFAULT '' CHECK (length(contract_no) <= 100),
    amount REAL NOT NULL DEFAULT 0 CHECK (amount >= 0 AND amount <= 999999999999),
    budget REAL NOT NULL DEFAULT 0 CHECK (budget >= 0 AND budget <= 999999999999),
    plan_start TEXT NOT NULL DEFAULT '' CHECK (plan_start = '' OR (length(plan_start) = 10 AND plan_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),
    plan_end TEXT NOT NULL DEFAULT '' CHECK (plan_end = '' OR (length(plan_end) = 10 AND plan_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),
    remark TEXT NOT NULL DEFAULT '' CHECK (length(remark) <= 2000),
    close_reason TEXT NOT NULL DEFAULT '' CHECK (length(close_reason) <= 500),
    status_before_close TEXT NOT NULL DEFAULT '' CHECK (length(status_before_close) <= 64),
    reopen_reason TEXT NOT NULL DEFAULT '' CHECK (length(reopen_reason) <= 500),
    status TEXT NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'chartered', 'req_architecture', 'req_assessment', 'in_progress', 'integration_test', 'go_live_prep', 'live', 'closed', 'archived', 'active')),
    org_id TEXT REFERENCES organizations(org_id),
    space_id TEXT REFERENCES team_spaces(space_id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);
INSERT INTO projects(
    id,name,project_code,project_type,description,summary,objective,client,contract_no,
    amount,budget,plan_start,plan_end,remark,close_reason,status_before_close,reopen_reason,
    status,org_id,space_id,created_at,updated_at,version
)
SELECT
    id,name,project_code,project_type,description,summary,objective,client,contract_no,
    amount,budget,plan_start,plan_end,remark,close_reason,'','',
    CASE status WHEN 'active' THEN 'chartered' ELSE status END,
    org_id,space_id,created_at,updated_at,version
FROM projects_0081_old;
DROP TABLE projects_0081_old;
PRAGMA legacy_alter_table=OFF;
CREATE UNIQUE INDEX IF NOT EXISTS ix_projects_code ON projects(project_code);
CREATE INDEX IF NOT EXISTS ix_projects_status_created ON projects(status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_projects_org ON projects(org_id, status, created_at, id);

CREATE TRIGGER IF NOT EXISTS trg_proj_org_immutable BEFORE UPDATE ON projects
    WHEN NEW.org_id IS NOT NULL AND OLD.org_id IS NOT NULL AND NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
