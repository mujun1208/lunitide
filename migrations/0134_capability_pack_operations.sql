CREATE TABLE capability_pack_operations (
 pack_id TEXT PRIMARY KEY,
 manifest_json TEXT NOT NULL CHECK(json_valid(manifest_json) AND length(manifest_json)<=65536),
 manifest_digest TEXT NOT NULL CHECK(length(manifest_digest)=64),
 state TEXT NOT NULL CHECK(state IN ('installing','installed','uninstalling','uninstalled','failed')),
 desired TEXT NOT NULL CHECK(desired IN ('installed','uninstalled')),
 version INTEGER NOT NULL CHECK(version>=1),
 last_error TEXT NOT NULL DEFAULT '' CHECK(length(last_error)<=4096),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE TABLE capability_pack_resources (
 kind TEXT NOT NULL CHECK(kind IN ('gate','mcp','skill')),
 resource_key TEXT NOT NULL,
 target_id TEXT NOT NULL,
 managed INTEGER NOT NULL CHECK(managed IN (0,1)),
 PRIMARY KEY(kind,resource_key)
);
CREATE TABLE capability_pack_references (
 pack_id TEXT NOT NULL REFERENCES capability_pack_operations(pack_id) ON DELETE RESTRICT,
 kind TEXT NOT NULL,
 resource_key TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('planned','ready','released')),
 PRIMARY KEY(pack_id,kind,resource_key),
 FOREIGN KEY(kind,resource_key) REFERENCES capability_pack_resources(kind,resource_key) ON DELETE RESTRICT
);
CREATE INDEX idx_capability_pack_refs_resource ON capability_pack_references(kind,resource_key,state);
