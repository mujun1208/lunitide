-- 0062 M8 slice 2 (T-8.2.x): knowledge base documents/chunks and the
-- ontology graph pipeline (snapshot -> parse -> graph -> versioned index).
--
-- KB documents are versioned with optimistic reindex (expectedVersion CAS,
-- M8-011); failed indexing marks index_state='failed' and publishes no
-- searchable projection (M8-012). Ontology snapshots are immutable per
-- (scope_id, revision); graph nodes/edges bind provenance; index versions
-- are building -> verified -> retired and never overwritten in place.
-- node_type is a closed 15-value whitelist (FR-05/O2 contract).
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE kb_collections (
    collection_id TEXT PRIMARY KEY CHECK (length(collection_id) = 26 AND substr(collection_id, 1, 1) GLOB '[0-7]' AND collection_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    auth_policy TEXT NOT NULL CHECK (length(auth_policy) BETWEEN 1 AND 512),
    created_at TEXT NOT NULL
);

CREATE TABLE kb_documents (
    document_id TEXT NOT NULL CHECK (length(document_id) = 26 AND substr(document_id, 1, 1) GLOB '[0-7]' AND document_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    collection_id TEXT NOT NULL REFERENCES kb_collections(collection_id),
    version INTEGER NOT NULL CHECK (version >= 1),
    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 1 AND 128),
    content_ref TEXT NOT NULL CHECK (length(content_ref) BETWEEN 1 AND 512),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    source_locator TEXT NOT NULL CHECK (length(source_locator) BETWEEN 1 AND 1024),
    index_state TEXT NOT NULL CHECK (index_state IN ('pending','indexing','ready','failed')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (document_id, version)
);

CREATE TABLE kb_chunks (
    chunk_id TEXT PRIMARY KEY CHECK (length(chunk_id) = 26 AND substr(chunk_id, 1, 1) GLOB '[0-7]' AND chunk_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    document_id TEXT NOT NULL,
    document_version INTEGER NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),
    locator_json TEXT NOT NULL CHECK (length(locator_json) >= 2),
    embedding BLOB,
    created_at TEXT NOT NULL,
    UNIQUE (document_id, document_version, ordinal)
);

CREATE TABLE ontology_snapshots (
    snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) = 26 AND substr(snapshot_id, 1, 1) GLOB '[0-7]' AND snapshot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),
    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 512),
    state TEXT NOT NULL CHECK (state IN ('active','superseded','quarantined')),
    created_at TEXT NOT NULL,
    UNIQUE (scope_id, revision)
);

CREATE TABLE graph_nodes (
    node_id TEXT PRIMARY KEY CHECK (length(node_id) = 26 AND substr(node_id, 1, 1) GLOB '[0-7]' AND node_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),
    node_type TEXT NOT NULL CHECK (node_type IN ('File','Document','Artifact','Requirement','Decision','Module','Class','Function','Interface','Table','Field','UseCase','TestCase','Task','Release')),
    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 65536),
    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),
    provenance TEXT NOT NULL CHECK (length(provenance) >= 2),
    created_at TEXT NOT NULL
);

CREATE TABLE graph_edges (
    edge_id TEXT PRIMARY KEY CHECK (length(edge_id) = 26 AND substr(edge_id, 1, 1) GLOB '[0-7]' AND edge_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),
    from_node TEXT NOT NULL REFERENCES graph_nodes(node_id),
    to_node TEXT NOT NULL REFERENCES graph_nodes(node_id),
    relation TEXT NOT NULL CHECK (length(relation) BETWEEN 1 AND 128),
    provenance TEXT NOT NULL CHECK (length(provenance) >= 2),
    created_at TEXT NOT NULL
);

CREATE TABLE graph_index_versions (
    index_version TEXT PRIMARY KEY CHECK (length(index_version) = 26 AND substr(index_version, 1, 1) GLOB '[0-7]' AND index_version NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),
    alias TEXT NOT NULL CHECK (length(alias) BETWEEN 1 AND 128),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('building','verified','retired')),
    created_at TEXT NOT NULL,
    UNIQUE (alias, index_version)
);

CREATE INDEX idx_kbd_coll ON kb_documents(collection_id, index_state);
CREATE INDEX idx_kbc_doc ON kb_chunks(document_id, document_version);
CREATE INDEX idx_gn_snap ON graph_nodes(snapshot_id, node_type);
CREATE INDEX idx_ge_snap ON graph_edges(snapshot_id);
