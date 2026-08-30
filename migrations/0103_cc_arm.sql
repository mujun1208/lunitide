-- 0103 computer-control optional arm TTL (OpenClaw-style).
-- armed_until is RFC3339; NULL/empty means permanently enabled until the
-- operator disables it. GET and execute expire the latch and persist enabled=0.
ALTER TABLE cc_security_config ADD COLUMN armed_until TEXT;
