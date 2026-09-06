CREATE TABLE kb_sources (
    source_id TEXT PRIMARY KEY CHECK (length(source_id)=26),
    collection_id TEXT NOT NULL REFERENCES kb_collections(collection_id),
    path_key TEXT NOT NULL CHECK (length(path_key) BETWEEN 1 AND 512),
    path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 512),
    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 1 AND 128),
    source_locator TEXT NOT NULL CHECK (length(source_locator) BETWEEN 1 AND 1024),
    sha256 TEXT NOT NULL CHECK (sha256='' OR (length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
    state TEXT NOT NULL CHECK (state IN ('fresh','refreshing','stale','missing','failed')),
    version INTEGER NOT NULL CHECK (version >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    checked_at TEXT NOT NULL,
    error TEXT NOT NULL CHECK (length(error)<=400),
    created_at TEXT NOT NULL,
    UNIQUE(collection_id,path_key)
);
CREATE TABLE kb_source_versions (
    source_id TEXT NOT NULL REFERENCES kb_sources(source_id),
    version INTEGER NOT NULL CHECK (version >= 1),
    sha256 TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('fresh','failed')),
    error TEXT NOT NULL CHECK (length(error)<=400),
    created_at TEXT NOT NULL,
    PRIMARY KEY(source_id,version)
);
CREATE TABLE kb_source_documents (
    source_id TEXT NOT NULL REFERENCES kb_sources(source_id),
    source_version INTEGER NOT NULL CHECK (source_version >= 1),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    document_id TEXT NOT NULL,
    document_version INTEGER NOT NULL,
    PRIMARY KEY(source_id,source_version,ordinal),
    UNIQUE(document_id,document_version),
    FOREIGN KEY(document_id,document_version) REFERENCES kb_documents(document_id,version)
);
CREATE INDEX ix_kb_source_document ON kb_source_documents(document_id);
-- Legacy local files require a verified refresh. Historical versions remain
-- available as provenance but cannot silently reappear in current retrieval.
INSERT INTO kb_sources
SELECT MIN(document_id),collection_id,
    CASE WHEN substr(content_ref,2,1)=':' THEN lower(replace(content_ref,char(92),'/')) ELSE content_ref END,
    MIN(content_ref),MIN(media_type),MIN(source_locator),'','stale',1,1,'','旧来源待刷新验证',MIN(created_at)
FROM kb_documents
WHERE substr(content_ref,1,1)='/' OR substr(content_ref,2,1)=':' OR substr(content_ref,1,2)=char(92)||char(92)
GROUP BY collection_id,CASE WHEN substr(content_ref,2,1)=':' THEN lower(replace(content_ref,char(92),'/')) ELSE content_ref END;
INSERT INTO kb_source_documents
SELECT s.source_id,1,ROW_NUMBER() OVER(PARTITION BY s.source_id ORDER BY d.document_id,d.version)-1,d.document_id,d.version
FROM kb_documents d JOIN kb_sources s ON s.collection_id=d.collection_id AND s.path_key=
    CASE WHEN substr(d.content_ref,2,1)=':' THEN lower(replace(d.content_ref,char(92),'/')) ELSE d.content_ref END
WHERE d.version=(SELECT MAX(current.version) FROM kb_documents current WHERE current.document_id=d.document_id);
INSERT INTO kb_source_versions SELECT source_id,1,'','failed','旧来源待刷新验证',created_at FROM kb_sources;
