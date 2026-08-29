-- 0098: IM channel settings for Feishu / WeCom / DingTalk webhooks and
-- WeChat / QQ desktop clients. Seeded empty; the engine fills the five
-- known kinds on first read so Settings can toggle them independently.
CREATE TABLE im_channels (
    kind TEXT PRIMARY KEY CHECK (kind IN ('feishu','wecom','dingtalk','wechat','qq')),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    webhook_url TEXT NOT NULL DEFAULT '' CHECK (length(webhook_url) <= 512),
    updated_at TEXT NOT NULL
);
