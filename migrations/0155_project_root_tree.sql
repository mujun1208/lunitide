-- 0155: project filesystem spine (root + tree + default executor).
-- Historical / personal-chat rows keep empty root_path. Unique only when bound.

ALTER TABLE projects ADD COLUMN root_path TEXT NOT NULL DEFAULT '' CHECK (length(root_path) <= 1024);
ALTER TABLE projects ADD COLUMN tree_status TEXT NOT NULL DEFAULT 'none' CHECK (tree_status IN ('none','pending','ready','partial','failed'));
ALTER TABLE projects ADD COLUMN tree_digest TEXT NOT NULL DEFAULT '' CHECK (tree_digest = '' OR (length(tree_digest) = 64 AND tree_digest NOT GLOB '*[^0-9a-f]*'));
ALTER TABLE projects ADD COLUMN tree_generated_at TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN default_executor TEXT NOT NULL DEFAULT 'lunitide' CHECK (default_executor IN ('lunitide','cursor','codex'));

CREATE UNIQUE INDEX idx_projects_root_path ON projects(root_path) WHERE root_path != '';
