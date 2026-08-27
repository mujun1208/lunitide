-- 0092 This-PC person identity and WeChat-style people threads.
-- Computer control stays local. LAN discovery is a persisted flag that
-- defaults to off; discovered peers are untrusted until paired.
CREATE TABLE local_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    subject_id TEXT NOT NULL UNIQUE CHECK (length(subject_id) = 26 AND substr(subject_id, 1, 1) GLOB '[0-7]' AND subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    public_key TEXT NOT NULL CHECK (length(public_key) = 64 AND public_key NOT GLOB '*[^0-9a-f]*'),
    private_key TEXT NOT NULL CHECK (length(private_key) = 128 AND private_key NOT GLOB '*[^0-9a-f]*'),
    nickname TEXT NOT NULL CHECK (length(nickname) BETWEEN 1 AND 64),
    avatar TEXT NOT NULL DEFAULT '' CHECK (length(avatar) <= 65536),
    status TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','away','busy','invisible')),
    department TEXT NOT NULL DEFAULT '' CHECK (length(department) <= 128),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 128),
    org_name TEXT NOT NULL DEFAULT '' CHECK (length(org_name) <= 128),
    bio TEXT NOT NULL DEFAULT '' CHECK (length(bio) <= 2000),
    password_hash TEXT NOT NULL DEFAULT '' CHECK (length(password_hash) <= 80),
    pairing_code TEXT NOT NULL CHECK (length(pairing_code) = 6 AND pairing_code GLOB '[0-9][0-9][0-9][0-9][0-9][0-9]'),
    discovery_enabled INTEGER NOT NULL DEFAULT 0 CHECK (discovery_enabled IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE people_contacts (
    subject_id TEXT PRIMARY KEY CHECK (length(subject_id) = 26 AND substr(subject_id, 1, 1) GLOB '[0-7]' AND subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    nickname TEXT NOT NULL CHECK (length(nickname) BETWEEN 1 AND 64),
    avatar TEXT NOT NULL DEFAULT '' CHECK (length(avatar) <= 65536),
    status TEXT NOT NULL DEFAULT 'online' CHECK (status IN ('online','away','busy','invisible','offline')),
    department TEXT NOT NULL DEFAULT '' CHECK (length(department) <= 128),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 128),
    org_name TEXT NOT NULL DEFAULT '' CHECK (length(org_name) <= 128),
    bio TEXT NOT NULL DEFAULT '' CHECK (length(bio) <= 2000),
    public_key TEXT NOT NULL DEFAULT '' CHECK (public_key = '' OR (length(public_key) = 64 AND public_key NOT GLOB '*[^0-9a-f]*')),
    pairing_hash TEXT NOT NULL DEFAULT '' CHECK (pairing_hash = '' OR (length(pairing_hash) = 64 AND pairing_hash NOT GLOB '*[^0-9a-f]*')),
    trust_state TEXT NOT NULL CHECK (trust_state IN ('self','trusted','discovered')),
    host_addr TEXT NOT NULL DEFAULT '' CHECK (length(host_addr) <= 128),
    last_seen_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE people_threads (
    thread_id TEXT PRIMARY KEY CHECK (length(thread_id) = 26 AND substr(thread_id, 1, 1) GLOB '[0-7]' AND thread_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    kind TEXT NOT NULL CHECK (kind IN ('direct','group')),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 128),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) = 26 AND substr(owner_subject_id, 1, 1) GLOB '[0-7]' AND owner_subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE people_thread_members (
    thread_id TEXT NOT NULL REFERENCES people_threads(thread_id) ON DELETE CASCADE,
    subject_id TEXT NOT NULL CHECK (length(subject_id) = 26 AND substr(subject_id, 1, 1) GLOB '[0-7]' AND subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    role TEXT NOT NULL CHECK (role IN ('owner','member')),
    joined_at TEXT NOT NULL,
    last_read_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (thread_id, subject_id)
);
CREATE TABLE people_messages (
    message_id TEXT PRIMARY KEY CHECK (length(message_id) = 26 AND substr(message_id, 1, 1) GLOB '[0-7]' AND message_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    thread_id TEXT NOT NULL REFERENCES people_threads(thread_id) ON DELETE CASCADE,
    sender_subject_id TEXT NOT NULL CHECK (length(sender_subject_id) = 26 AND substr(sender_subject_id, 1, 1) GLOB '[0-7]' AND sender_subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    kind TEXT NOT NULL CHECK (kind IN ('text','image','file','emoji','system')),
    body TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 16384),
    file_name TEXT NOT NULL DEFAULT '' CHECK (length(file_name) <= 256),
    file_mime TEXT NOT NULL DEFAULT '' CHECK (length(file_mime) <= 128),
    file_size INTEGER NOT NULL DEFAULT 0 CHECK (file_size >= 0),
    file_sha256 TEXT NOT NULL DEFAULT '' CHECK (file_sha256 = '' OR (length(file_sha256) = 64 AND file_sha256 NOT GLOB '*[^0-9a-f]*')),
    created_at TEXT NOT NULL
);
CREATE TABLE people_file_offers (
    offer_id TEXT PRIMARY KEY CHECK (length(offer_id) = 26 AND substr(offer_id, 1, 1) GLOB '[0-7]' AND offer_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    message_id TEXT NOT NULL UNIQUE REFERENCES people_messages(message_id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL REFERENCES people_threads(thread_id) ON DELETE CASCADE,
    from_subject_id TEXT NOT NULL CHECK (length(from_subject_id) = 26 AND substr(from_subject_id, 1, 1) GLOB '[0-7]' AND from_subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    to_subject_id TEXT NOT NULL CHECK (length(to_subject_id) = 26 AND substr(to_subject_id, 1, 1) GLOB '[0-7]' AND to_subject_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    status TEXT NOT NULL CHECK (status IN ('pending','accepted','rejected')),
    file_name TEXT NOT NULL CHECK (length(file_name) BETWEEN 1 AND 256),
    file_mime TEXT NOT NULL DEFAULT '' CHECK (length(file_mime) <= 128),
    file_size INTEGER NOT NULL CHECK (file_size >= 0),
    file_sha256 TEXT NOT NULL CHECK (length(file_sha256) = 64 AND file_sha256 NOT GLOB '*[^0-9a-f]*'),
    staging_path TEXT NOT NULL DEFAULT '' CHECK (length(staging_path) <= 1024),
    dest_path TEXT NOT NULL DEFAULT '' CHECK (length(dest_path) <= 1024),
    created_at TEXT NOT NULL,
    decided_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX ix_people_contacts_trust ON people_contacts(trust_state, department, nickname);
CREATE INDEX ix_people_members_subject ON people_thread_members(subject_id);
CREATE INDEX ix_people_messages_thread ON people_messages(thread_id, created_at, message_id);
CREATE INDEX ix_people_offers_status ON people_file_offers(status, created_at);
