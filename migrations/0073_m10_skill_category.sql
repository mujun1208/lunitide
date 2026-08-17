-- 0073 M10 技能分类映射：技能中心 12 类分类的本地旁挂表。
-- 设计合同：分类解析优先级 manual > manifest > keyword，兜底 other；
-- 本表只存映射结果（手动指定或创建/安装时写入的计算值），不改 M6
-- skills 权威表结构。12 类固定枚举（UI 文案：效率/写作/开发/数据/设计/
-- 研究/生活/教育/商业/自动化/安全/其他）；UNIQUE(skill_id) 保证单技能
-- 单映射，技能删除时同步清理。

CREATE TABLE sk_category_map (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_id TEXT NOT NULL CHECK (length(skill_id) = 26 AND substr(skill_id, 1, 1) GLOB '[0-7]' AND skill_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    category TEXT NOT NULL CHECK (category IN ('efficiency','writing','development','data','design','research','lifestyle','education','business','automation','security','other')),
    match_source TEXT NOT NULL CHECK (match_source IN ('manifest','keyword','manual')),
    updated_at TEXT NOT NULL,
    UNIQUE (skill_id)
);
