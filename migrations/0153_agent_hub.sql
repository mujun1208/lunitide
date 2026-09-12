CREATE TABLE agent_hub_tasks (
  id TEXT PRIMARY KEY CHECK (length(id)=26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
  agent TEXT NOT NULL CHECK (agent IN ('codex','cursor','kimi')),
  prompt TEXT NOT NULL CHECK (length(prompt) BETWEEN 1 AND 100000),
  work_dir TEXT NOT NULL,
  sandbox TEXT,
  status TEXT NOT NULL CHECK (status IN ('queued','running','success','failed','timeout','cancelled')),
  exit_code INTEGER,
  tokens_used INTEGER NOT NULL DEFAULT 0,
  error_msg TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
  UNIQUE(idempotency_key)
);
CREATE TABLE agent_hub_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES agent_hub_tasks(id),
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  ts TEXT NOT NULL,
  UNIQUE(task_id, seq)
);
CREATE TABLE agent_hub_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES agent_hub_tasks(id),
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  mime TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL CHECK (source IN ('event','scan','outside')),
  UNIQUE(task_id, path)
);
CREATE INDEX ix_agent_hub_tasks_agent_status ON agent_hub_tasks(agent, status, created_at);
CREATE INDEX ix_agent_hub_events_task_seq ON agent_hub_events(task_id, seq);
CREATE INDEX ix_agent_hub_artifacts_task ON agent_hub_artifacts(task_id);
