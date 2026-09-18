-- Memory retrieval indexes. FTS virtual/shadow objects are skipped by
-- sqlite_schema dump (same rule as 0107/0121).
CREATE TABLE memory_entities (
    entity_id TEXT PRIMARY KEY CHECK (length(entity_id) = 26 AND substr(entity_id, 1, 1) GLOB '[0-7]' AND entity_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    entity_type TEXT NOT NULL CHECK (length(entity_type) BETWEEN 1 AND 64),
    canonical_name TEXT NOT NULL CHECK (length(canonical_name) BETWEEN 1 AND 256),
    aliases_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(aliases_json) AND length(aliases_json) <= 4096),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (subject_id, scope_kind, scope_id, entity_type, canonical_name)
);

CREATE TABLE memory_relations (
    relation_id TEXT PRIMARY KEY CHECK (length(relation_id) = 26 AND substr(relation_id, 1, 1) GLOB '[0-7]' AND relation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    from_entity_id TEXT NOT NULL CHECK (length(from_entity_id) = 26 AND substr(from_entity_id, 1, 1) GLOB '[0-7]' AND from_entity_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    predicate TEXT NOT NULL CHECK (length(predicate) BETWEEN 1 AND 64),
    to_entity_id TEXT CHECK (to_entity_id IS NULL OR (length(to_entity_id) = 26 AND substr(to_entity_id, 1, 1) GLOB '[0-7]' AND to_entity_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    object_text TEXT CHECK (object_text IS NULL OR length(object_text) BETWEEN 1 AND 512),
    valid_from TEXT,
    valid_to TEXT,
    learned_at TEXT NOT NULL,
    invalidated_at TEXT,
    state TEXT NOT NULL CHECK (state IN ('active','invalidated')),
    CHECK ((to_entity_id IS NULL) <> (object_text IS NULL)),
    CHECK (valid_from IS NULL OR valid_to IS NULL OR valid_from < valid_to)
);

CREATE INDEX ix_memory_relations_fact ON memory_relations(fact_id, fact_version, state);

CREATE TABLE memory_embeddings (
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    embedding_space_id TEXT NOT NULL CHECK (length(embedding_space_id) BETWEEN 1 AND 128),
    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 128),
    dimensions INTEGER NOT NULL CHECK (dimensions >= 1),
    vector_blob BLOB NOT NULL,
    vector_digest TEXT NOT NULL CHECK (length(vector_digest) = 64 AND vector_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('ready','invalidated')),
    embedded_at TEXT NOT NULL,
    PRIMARY KEY (fact_id, fact_version, embedding_space_id)
);

CREATE TABLE memory_embedding_jobs (
    job_id TEXT PRIMARY KEY CHECK (length(job_id) = 26 AND substr(job_id, 1, 1) GLOB '[0-7]' AND job_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    embedding_space_id TEXT NOT NULL CHECK (length(embedding_space_id) BETWEEN 1 AND 128),
    state TEXT NOT NULL CHECK (state IN ('queued','running','deferred','succeeded','failed','cancelled')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    next_attempt_at TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (fact_id, fact_version, embedding_space_id)
);

CREATE TABLE memory_recall_hit_details (
    trace_id TEXT NOT NULL CHECK (length(trace_id) BETWEEN 1 AND 128),
    rank INTEGER NOT NULL CHECK (rank >= 1),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    keyword_score REAL,
    dense_score REAL,
    temporal_score REAL,
    relation_score REAL,
    feedback_score REAL,
    fused_score REAL NOT NULL,
    adopted INTEGER NOT NULL CHECK (adopted IN (0,1)),
    reason_code TEXT NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 64),
    token_count INTEGER NOT NULL CHECK (token_count >= 0),
    PRIMARY KEY (trace_id, rank)
);

CREATE TABLE memory_feedback_events (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    trace_id TEXT NOT NULL CHECK (length(trace_id) BETWEEN 1 AND 128),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    turn_id TEXT NOT NULL CHECK (length(turn_id) BETWEEN 1 AND 128),
    outcome TEXT NOT NULL CHECK (outcome IN ('used','unused','helpful','contradicted','user_corrected')),
    created_at TEXT NOT NULL
);

CREATE TABLE memory_search_documents (
    rowid INTEGER PRIMARY KEY,
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 8192),
    UNIQUE (fact_id, fact_version)
);

CREATE INDEX ix_memory_search_documents_scope ON memory_search_documents(subject_id, scope_kind, scope_id);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_search_fts USING fts5(
    text,
    content='memory_search_documents',
    content_rowid='rowid',
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_memory_search_fts_ai AFTER INSERT ON memory_search_documents BEGIN
  INSERT INTO memory_search_fts(rowid, text) VALUES (new.rowid, new.text);
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_search_fts_au AFTER UPDATE OF text ON memory_search_documents BEGIN
  INSERT INTO memory_search_fts(memory_search_fts, rowid, text) VALUES('delete', old.rowid, old.text);
  INSERT INTO memory_search_fts(rowid, text) VALUES (new.rowid, new.text);
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_search_fts_ad AFTER DELETE ON memory_search_documents BEGIN
  INSERT INTO memory_search_fts(memory_search_fts, rowid, text) VALUES('delete', old.rowid, old.text);
END;
