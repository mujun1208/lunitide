CREATE TABLE protocol_message_groups (
    id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    session_id TEXT NOT NULL DEFAULT '' CHECK (session_id='' OR (length(session_id)=26 AND substr(session_id, 1, 1) GLOB '[0-7]' AND session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    turn_id TEXT NOT NULL DEFAULT '' CHECK (turn_id='' OR (length(turn_id)=26 AND substr(turn_id, 1, 1) GLOB '[0-7]' AND turn_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    sequence INTEGER NOT NULL CHECK (sequence >= 0),
    complete INTEGER NOT NULL CHECK (complete IN (0,1)),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND length(payload_json) <= 1048576),
    created_at TEXT NOT NULL,
    UNIQUE(owner_scope, session_id, turn_id, sequence)
);
CREATE INDEX ix_protocol_message_groups_session ON protocol_message_groups(session_id, turn_id, sequence);
CREATE TABLE protocol_private (
    id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    ref TEXT NOT NULL CHECK (length(ref) BETWEEN 1 AND 64),
    cipher_blob BLOB NOT NULL,
    digest TEXT NOT NULL CHECK (length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
    credential_generation TEXT NOT NULL DEFAULT '' CHECK (length(credential_generation) <= 64),
    created_at TEXT NOT NULL,
    UNIQUE(owner_scope, ref)
);
