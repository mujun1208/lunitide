CREATE TABLE mcp_endpoint_security (
 endpoint_id TEXT PRIMARY KEY REFERENCES mcp_endpoint_settings(endpoint_id) ON DELETE CASCADE,
 auth_ref TEXT NOT NULL DEFAULT '' CHECK(length(auth_ref)<=256),
 env_refs_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(env_refs_json) AND length(env_refs_json)<=16384),
 pin_json TEXT NOT NULL DEFAULT '' CHECK(pin_json='' OR (json_valid(pin_json) AND length(pin_json)<=65536)),
 launch_args_json TEXT NOT NULL DEFAULT '' CHECK(launch_args_json='' OR (json_valid(launch_args_json) AND length(launch_args_json)<=65536)),
 version INTEGER NOT NULL DEFAULT 0 CHECK(version>=0),
 updated_at TEXT NOT NULL
);
