CREATE TABLE model_fit_qualification (
    id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    family TEXT NOT NULL CHECK (length(family) BETWEEN 1 AND 32),
    codec_version TEXT NOT NULL DEFAULT '' CHECK (length(codec_version) <= 64),
    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 200),
    status TEXT NOT NULL CHECK (status IN ('untested','fixture_pass','blocked')),
    evidence TEXT NOT NULL DEFAULT '' CHECK (length(evidence) <= 2000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(owner_scope, family, model_id, codec_version)
);
CREATE INDEX ix_model_fit_qualification_scope ON model_fit_qualification(owner_scope, family, model_id);
