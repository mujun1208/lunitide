CREATE TABLE queue_deliveries (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    consumer TEXT NOT NULL CHECK (consumer='renderer' OR length(consumer)=26),
    state TEXT NOT NULL CHECK (state IN ('claimed','prepared','started','confirmed','unknown')),
    stream_id TEXT NOT NULL DEFAULT '' CHECK (stream_id='' OR length(stream_id)=26),
    message_ids_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(message_ids_json) AND length(message_ids_json)<=2048),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_queue_delivery_session ON queue_deliveries(session_id,state,created_at);
CREATE TABLE queue_delivery_items (
    delivery_id TEXT NOT NULL REFERENCES queue_deliveries(id) ON DELETE CASCADE,
    queued_id TEXT NOT NULL UNIQUE REFERENCES queued_user_messages(id) ON DELETE CASCADE,
    PRIMARY KEY(delivery_id,queued_id)
);
