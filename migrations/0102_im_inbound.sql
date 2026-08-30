-- 0102 Feishu / WeCom inbound on this PC: default off, sender allowlist.
-- No public listen port. Outbound long-connection uses inbound_app_id/secret.
-- SQLite cannot ADD several CHECKed columns cleanly for dump tests, so rebuild.
CREATE TABLE im_channels_0102 (
    kind TEXT PRIMARY KEY CHECK (kind IN ('feishu','wecom','dingtalk','wechat','qq')),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    webhook_url TEXT NOT NULL DEFAULT '' CHECK (length(webhook_url) <= 512),
    inbound_enabled INTEGER NOT NULL DEFAULT 0 CHECK (inbound_enabled IN (0,1)),
    inbound_allowlist TEXT NOT NULL DEFAULT '' CHECK (length(inbound_allowlist) <= 2000),
    inbound_auto_run INTEGER NOT NULL DEFAULT 0 CHECK (inbound_auto_run IN (0,1)),
    inbound_app_id TEXT NOT NULL DEFAULT '' CHECK (length(inbound_app_id) <= 64),
    inbound_app_secret TEXT NOT NULL DEFAULT '' CHECK (length(inbound_app_secret) <= 256),
    updated_at TEXT NOT NULL
);
INSERT INTO im_channels_0102(kind, enabled, webhook_url, inbound_enabled, inbound_allowlist, inbound_auto_run, inbound_app_id, inbound_app_secret, updated_at)
SELECT kind, enabled, webhook_url, 0, '', 0, '', '', updated_at FROM im_channels;
DROP TABLE im_channels;
ALTER TABLE im_channels_0102 RENAME TO im_channels;
