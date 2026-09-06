CREATE TABLE people_delivery_outbox (
    message_id TEXT NOT NULL REFERENCES people_messages(message_id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    message_json TEXT NOT NULL CHECK (json_valid(message_json)),
    state TEXT NOT NULL CHECK (state IN ('pending','delivered')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TEXT NOT NULL,
    delivered_at TEXT,
    PRIMARY KEY (message_id, recipient_id)
);
CREATE INDEX ix_people_delivery_due ON people_delivery_outbox(state, next_attempt_at, message_id);
CREATE TABLE people_send_requests (
    request_key TEXT PRIMARY KEY,
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64),
    message_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);
