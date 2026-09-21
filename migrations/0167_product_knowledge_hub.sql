-- Product Knowledge Hub: isolated from 0062 ontology_nodes.
CREATE TABLE product_hub_auth (
  singleton_id TEXT PRIMARY KEY CHECK (singleton_id = 'default'),
  username TEXT NOT NULL CHECK (username = 'mujun'),
  password_hash TEXT NOT NULL CHECK (length(password_hash) BETWEEN 20 AND 128),
  updated_at TEXT NOT NULL
);
CREATE TABLE product_snapshots (
  snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) BETWEEN 1 AND 64),
  state TEXT NOT NULL CHECK (state IN ('building','verified','retired')),
  trigger TEXT NOT NULL CHECK (trigger IN ('boot','registry','manifest','upgrade','manual')),
  digest TEXT NOT NULL CHECK (length(digest) BETWEEN 1 AND 128),
  features_json TEXT NOT NULL DEFAULT '[]',
  graph_json TEXT NOT NULL DEFAULT '{}',
  findings_json TEXT NOT NULL DEFAULT '[]',
  changes_json TEXT NOT NULL DEFAULT '[]',
  report_markdown TEXT NOT NULL DEFAULT '',
  report_html TEXT NOT NULL DEFAULT '',
  card_count INTEGER NOT NULL DEFAULT 0,
  health_score INTEGER NOT NULL DEFAULT 0,
  generated_at TEXT NOT NULL
);
CREATE TABLE product_graph_nodes (
  node_id TEXT PRIMARY KEY CHECK (length(node_id) BETWEEN 1 AND 160),
  snapshot_id TEXT NOT NULL,
  stable_key TEXT NOT NULL,
  node_type TEXT NOT NULL CHECK (node_type IN ('Product','Domain','Module','Feature','Expert','Skill','Plugin','Mcp','McpTool','Chain','Step','Capability','Scenario')),
  name TEXT NOT NULL,
  domain TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE product_graph_edges (
  edge_id TEXT PRIMARY KEY CHECK (length(edge_id) BETWEEN 1 AND 160),
  snapshot_id TEXT NOT NULL,
  from_id TEXT NOT NULL,
  to_id TEXT NOT NULL,
  rel TEXT NOT NULL CHECK (rel IN ('contains','uses','calls','depends','tagged'))
);
CREATE TABLE product_feature_cards (
  stable_key TEXT NOT NULL,
  snapshot_id TEXT NOT NULL,
  card_json TEXT NOT NULL,
  PRIMARY KEY (snapshot_id, stable_key)
);
CREATE TABLE product_chain_steps (
  snapshot_id TEXT NOT NULL,
  stable_key TEXT NOT NULL,
  step_index INTEGER NOT NULL,
  name TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (snapshot_id, stable_key, step_index)
);
CREATE TABLE product_graph_index_versions (
  index_id TEXT PRIMARY KEY CHECK (index_id = 'default'),
  snapshot_id TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE product_tags (
  tag_id TEXT PRIMARY KEY CHECK (length(tag_id) BETWEEN 1 AND 64),
  vocab TEXT NOT NULL,
  value TEXT NOT NULL
);
CREATE TABLE product_node_tags (
  stable_key TEXT NOT NULL,
  vocab TEXT NOT NULL,
  value TEXT NOT NULL,
  assigned_by TEXT NOT NULL CHECK (assigned_by IN ('registry','rule','llm','manual')),
  PRIMARY KEY (stable_key, vocab, value)
);
CREATE TABLE product_enrichments (
  stable_key TEXT PRIMARY KEY,
  summary TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  marked_ai INTEGER NOT NULL DEFAULT 0 CHECK (marked_ai IN (0,1))
);
CREATE TABLE product_change_log (
  change_id TEXT PRIMARY KEY CHECK (length(change_id) BETWEEN 1 AND 64),
  snapshot_id TEXT NOT NULL,
  stable_key TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('added','updated','removed'))
);
CREATE TABLE product_diagnostic_reports (
  report_id TEXT PRIMARY KEY CHECK (length(report_id) BETWEEN 1 AND 64),
  snapshot_id TEXT NOT NULL,
  markdown TEXT NOT NULL DEFAULT '',
  html TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE product_diagnostic_findings (
  finding_id TEXT PRIMARY KEY CHECK (length(finding_id) BETWEEN 1 AND 64),
  report_id TEXT NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('info','warn','error')),
  error_code TEXT NOT NULL,
  stable_key TEXT NOT NULL,
  title TEXT NOT NULL,
  evidence TEXT NOT NULL DEFAULT '',
  root_cause TEXT NOT NULL DEFAULT '',
  fix TEXT NOT NULL DEFAULT '',
  verify TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('open','fixed','wont_fix','regressed'))
);
