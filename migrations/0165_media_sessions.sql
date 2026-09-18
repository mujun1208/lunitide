-- Media sessions, operations, queue, settings, player leases, and audio focus.
-- media_session_v2 and activity_center_v2 default off.

CREATE TABLE media_assets (
    asset_id TEXT PRIMARY KEY CHECK (length(asset_id) = 26 AND substr(asset_id, 1, 1) GLOB '[0-7]' AND asset_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('user_selected','artifact','workspace')),
    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 1024),
    file_identity TEXT NOT NULL CHECK (length(file_identity) BETWEEN 1 AND 512),
    content_digest TEXT CHECK (content_digest IS NULL OR (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*')),
    mime TEXT NOT NULL CHECK (length(mime) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK (kind IN ('audio','video')),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),
    size INTEGER NOT NULL CHECK (size >= 0),
    state TEXT NOT NULL CHECK (state IN ('ready','missing','changed','revoked')),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE INDEX ix_media_assets_scope ON media_assets(owner_subject_id, scope_kind, scope_id, created_at);

CREATE TABLE media_sessions (
    media_session_id TEXT PRIMARY KEY CHECK (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    origin TEXT NOT NULL CHECK (origin IN ('owned','external')),
    phase TEXT NOT NULL CHECK (phase IN ('idle','playing','paused','stalled','ended','uncertain','failed','stopped')),
    verification_status TEXT NOT NULL CHECK (verification_status IN ('none','command_dispatched','verified_playing','verified_paused','verified_ended','verified_stopped')),
    verification_source TEXT NOT NULL CHECK (verification_source IN ('none','smtc','owned_runtime')),
    asset_id TEXT CHECK (asset_id IS NULL OR (length(asset_id) = 26 AND substr(asset_id, 1, 1) GLOB '[0-7]' AND asset_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    playback_epoch INTEGER NOT NULL DEFAULT 0 CHECK (playback_epoch >= 0),
    auto_advance INTEGER NOT NULL DEFAULT 1 CHECK (auto_advance IN (0,1)),
    position_ms INTEGER NOT NULL DEFAULT 0 CHECK (position_ms >= 0),
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    volume INTEGER NOT NULL DEFAULT 100 CHECK (volume >= 0 AND volume <= 100),
    muted INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0,1)),
    external_app_key TEXT NOT NULL DEFAULT '' CHECK (length(external_app_key) <= 128),
    external_session_key TEXT NOT NULL DEFAULT '' CHECK (length(external_session_key) <= 128),
    queue_revision INTEGER NOT NULL DEFAULT 1 CHECK (queue_revision >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE INDEX ix_media_sessions_scope ON media_sessions(owner_subject_id, scope_kind, scope_id, updated_at);

CREATE TABLE media_operations (
    operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 26 AND substr(operation_id, 1, 1) GLOB '[0-7]' AND operation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    media_session_id TEXT NOT NULL CHECK (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    parent_operation_id TEXT CHECK (parent_operation_id IS NULL OR (length(parent_operation_id) = 26 AND substr(parent_operation_id, 1, 1) GLOB '[0-7]' AND parent_operation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    action TEXT NOT NULL CHECK (action IN ('play','pause','toggle','stop','previous','next','seek','set_volume','mute','unmute','create','move','remove','clear','jump')),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    phase TEXT NOT NULL CHECK (phase IN ('requested','awaiting_approval','dispatching','verifying','succeeded','uncertain','failed','cancelled')),
    verification_status TEXT NOT NULL CHECK (verification_status IN ('not_applicable','not_started','pending','confirmed','unconfirmed')),
    verification_source TEXT NOT NULL CHECK (verification_source IN ('none','process','window','uia','smtc','owned_runtime','artifact')),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 128),
    evidence_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(evidence_json) AND length(evidence_json) <= 65536),
    accepted_at TEXT,
    dispatched_at TEXT,
    completed_at TEXT,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (media_session_id, idempotency_key),
    UNIQUE (media_session_id, request_digest)
);

CREATE INDEX ix_media_operations_session ON media_operations(media_session_id, created_at);

CREATE TABLE media_queue_items (
    item_id TEXT PRIMARY KEY CHECK (length(item_id) = 26 AND substr(item_id, 1, 1) GLOB '[0-7]' AND item_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    media_session_id TEXT NOT NULL CHECK (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    asset_id TEXT NOT NULL CHECK (length(asset_id) = 26 AND substr(asset_id, 1, 1) GLOB '[0-7]' AND asset_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    order_index INTEGER NOT NULL CHECK (order_index >= 0),
    state TEXT NOT NULL CHECK (state IN ('queued','current','played','removed','failed')),
    UNIQUE (media_session_id, order_index)
);

CREATE UNIQUE INDEX ux_media_queue_current ON media_queue_items(media_session_id) WHERE state = 'current';

CREATE TABLE media_bookmarks (
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    asset_id TEXT NOT NULL CHECK (length(asset_id) = 26 AND substr(asset_id, 1, 1) GLOB '[0-7]' AND asset_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    position_ms INTEGER NOT NULL CHECK (position_ms >= 0),
    duration_ms INTEGER NOT NULL CHECK (duration_ms >= 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_subject_id, asset_id)
);

CREATE TABLE media_settings (
    singleton_id TEXT PRIMARY KEY CHECK (singleton_id = 'default'),
    media_session_v2 INTEGER NOT NULL DEFAULT 0 CHECK (media_session_v2 IN (0,1)),
    activity_center_v2 INTEGER NOT NULL DEFAULT 0 CHECK (activity_center_v2 IN (0,1)),
    auto_advance INTEGER NOT NULL DEFAULT 1 CHECK (auto_advance IN (0,1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);

CREATE TABLE media_player_leases (
    media_session_id TEXT PRIMARY KEY CHECK (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    window_instance_id TEXT NOT NULL CHECK (length(window_instance_id) BETWEEN 1 AND 128),
    navigation_epoch INTEGER NOT NULL CHECK (navigation_epoch >= 0),
    lease_token_digest TEXT NOT NULL CHECK (length(lease_token_digest) = 64 AND lease_token_digest NOT GLOB '*[^0-9a-f]*'),
    lease_expires_at TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK (generation >= 1)
);

CREATE TABLE media_audio_focus (
    singleton_id TEXT PRIMARY KEY CHECK (singleton_id = 'default'),
    owner_subject_id TEXT NOT NULL DEFAULT '' CHECK (length(owner_subject_id) <= 128),
    media_session_id TEXT CHECK (media_session_id IS NULL OR (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    playback_epoch INTEGER NOT NULL DEFAULT 0 CHECK (playback_epoch >= 0),
    focus_token_digest TEXT NOT NULL DEFAULT '' CHECK (focus_token_digest = '' OR (length(focus_token_digest) = 64 AND focus_token_digest NOT GLOB '*[^0-9a-f]*')),
    reason TEXT NOT NULL CHECK (reason IN ('none','owned_playback','external','tts')),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);

CREATE TABLE media_player_commands (
    operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 26 AND substr(operation_id, 1, 1) GLOB '[0-7]' AND operation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    media_session_id TEXT NOT NULL CHECK (length(media_session_id) = 26 AND substr(media_session_id, 1, 1) GLOB '[0-7]' AND media_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    lease_generation INTEGER NOT NULL CHECK (lease_generation >= 1),
    asset_id TEXT CHECK (asset_id IS NULL OR (length(asset_id) = 26 AND substr(asset_id, 1, 1) GLOB '[0-7]' AND asset_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    playback_epoch INTEGER NOT NULL CHECK (playback_epoch >= 0),
    desired_state_json TEXT NOT NULL CHECK (json_valid(desired_state_json) AND length(desired_state_json) <= 65536),
    claimed_at TEXT,
    acknowledged_at TEXT,
    state TEXT NOT NULL CHECK (state IN ('pending','claimed','acknowledged','expired','cancelled','failed'))
);

INSERT INTO media_settings(singleton_id, media_session_v2, activity_center_v2, auto_advance, revision, updated_at)
VALUES ('default', 0, 0, 1, 1, '1970-01-01T00:00:00Z');

INSERT INTO media_audio_focus(singleton_id, owner_subject_id, media_session_id, playback_epoch, focus_token_digest, reason, revision, updated_at)
VALUES ('default', '', NULL, 0, '', 'none', 1, '1970-01-01T00:00:00Z');
