CREATE TABLE agent_hub_threads (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  harness_id TEXT NOT NULL CHECK (length(harness_id) BETWEEN 1 AND 64 AND harness_id NOT GLOB '*[^a-z0-9-]*'),
  native_session_id TEXT NOT NULL DEFAULT '' CHECK (length(native_session_id) <= 256),
  title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),
  pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
  workspace_root TEXT NOT NULL CHECK (length(workspace_root) BETWEEN 1 AND 1024),
  export_dir TEXT NOT NULL DEFAULT '' CHECK (length(export_dir) <= 1024),
  scene TEXT NOT NULL CHECK (scene IN ('write_project','fix','ppt','free')),
  status TEXT NOT NULL CHECK (status IN ('idle','running','waiting_user','success','failed','cancelled','faulted')),
  access_mode TEXT NOT NULL DEFAULT 'approval' CHECK (access_mode IN ('approval','auto-edit','full-access')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE agent_hub_messages (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('user','assistant','system','notice')),
  content TEXT NOT NULL CHECK (length(content) <= 200000),
  created_at TEXT NOT NULL,
  UNIQUE(thread_id, seq)
);
CREATE TABLE agent_hub_thread_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL CHECK (length(type) BETWEEN 1 AND 64),
  title TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload_json) AND length(payload_json) <= 1048576),
  ts TEXT NOT NULL,
  UNIQUE(thread_id, seq)
);
CREATE TABLE agent_hub_thread_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  rel_path TEXT NOT NULL CHECK (length(rel_path) BETWEEN 1 AND 1024),
  abs_path TEXT NOT NULL CHECK (length(abs_path) BETWEEN 1 AND 1024),
  size INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL CHECK (source IN ('event','scan','export')),
  UNIQUE(thread_id, rel_path)
);
CREATE TABLE agent_hub_prompts (
  thread_id TEXT NOT NULL REFERENCES agent_hub_threads(id) ON DELETE CASCADE,
  call_id TEXT NOT NULL CHECK (length(call_id) BETWEEN 1 AND 128),
  prompt TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 4000),
  options_json TEXT NOT NULL CHECK (json_valid(options_json) AND length(options_json) <= 8192),
  status TEXT NOT NULL CHECK (status IN ('open','answered','cancelled')),
  PRIMARY KEY(thread_id, call_id)
);
CREATE INDEX ix_agent_hub_threads_harness ON agent_hub_threads(harness_id, pinned DESC, updated_at DESC, id);
CREATE INDEX ix_agent_hub_messages_thread ON agent_hub_messages(thread_id, seq);
