-- 0084: Prompt bundle entity chain for governed prompt_bundle imports.

CREATE TABLE m6_prompt_bundle (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 256),
    status TEXT NOT NULL DEFAULT 'verified' CHECK (status IN ('verified', 'quarantined')),
    current_version_id TEXT CHECK (current_version_id IS NULL OR length(current_version_id) = 26),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);

CREATE TABLE m6_prompt_bundle_version (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    bundle_id TEXT NOT NULL REFERENCES m6_prompt_bundle(id) ON DELETE CASCADE,
    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 64),
    manifest_ref TEXT NOT NULL CHECK (length(manifest_ref) BETWEEN 1 AND 512),
    template_ref TEXT NOT NULL CHECK (length(template_ref) BETWEEN 1 AND 256),
    package_hash TEXT NOT NULL CHECK (length(package_hash) = 64 AND package_hash NOT GLOB '*[^0-9a-f]*'),
    compiled_digest TEXT NOT NULL CHECK (length(compiled_digest) = 64 AND compiled_digest NOT GLOB '*[^0-9a-f]*'),
    compiled_body TEXT NOT NULL CHECK (length(compiled_body) BETWEEN 1 AND 262144),
    signature_status TEXT NOT NULL CHECK (signature_status IN ('verified', 'unverified', 'invalid')),
    created_at TEXT NOT NULL,
    UNIQUE (bundle_id, semver)
);
