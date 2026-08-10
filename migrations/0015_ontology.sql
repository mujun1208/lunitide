CREATE TABLE ontology_nodes (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    type TEXT NOT NULL CHECK (type IN ('class', 'interface', 'function', 'module', 'table', 'file', 'requirement', 'artifact', 'component', 'endpoint', 'test')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),
    full_path TEXT NOT NULL DEFAULT '' CHECK (length(full_path) <= 1024),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(metadata_json) <= 65536),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX ix_ontology_nodes_project_type ON ontology_nodes(project_id, type);
CREATE UNIQUE INDEX ux_ontology_nodes_project_path ON ontology_nodes(project_id, full_path) WHERE full_path != '';

CREATE TABLE ontology_edges (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    source_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,
    target_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('implements', 'extends', 'depends_on', 'references', 'contains', 'tests', 'imports', 'satisfies', 'traces', 'generates', 'configures', 'authenticates', 'authorizes')),
    label TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 256),
    properties_json TEXT NOT NULL DEFAULT '{}' CHECK (length(properties_json) <= 65536),
    weight REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0.0 AND weight <= 1.0),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (source_node_id != target_node_id)
);

CREATE INDEX ix_ontology_edges_source ON ontology_edges(source_node_id);
CREATE INDEX ix_ontology_edges_target ON ontology_edges(target_node_id);