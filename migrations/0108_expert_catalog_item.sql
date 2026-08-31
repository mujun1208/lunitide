-- 0108: persist shipped conversation-expert catalog id separately from
-- origin_bundle_id (that column is a FK to plugin_bundles and must stay
-- empty for market/local cards). Backfill same-name factory specialists
-- so a later rename still stamps kind=agent.
ALTER TABLE expert_catalog ADD COLUMN catalog_item_id TEXT;

UPDATE expert_catalog SET catalog_item_id = 'ppt-expert' WHERE name = 'PPT专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'report-writer' WHERE name = '报告编写专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'novel-writer' WHERE name = '小说编写专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'excel-maker' WHERE name = 'Excel表格制作专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'ui-designer' WHERE name = 'UI专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'pm-expert' WHERE name = '产品经理专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'architect-expert' WHERE name = '系统架构师专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'db-expert' WHERE name = '数据库设计专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'repo-expert' WHERE name = '系统项目结构规范专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'standards-expert' WHERE name = '开发规范专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'test-expert' WHERE name = '系统测试专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'hardware-expert' WHERE name = '硬件配置专家' AND IFNULL(catalog_item_id, '') = '';
UPDATE expert_catalog SET catalog_item_id = 'dev-expert' WHERE name = '开发专家' AND IFNULL(catalog_item_id, '') = '';
