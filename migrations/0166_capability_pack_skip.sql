-- Persist skipped MCP probes. Playwright (and similar) may fail locally
-- without aborting the rest of a capability pack.
PRAGMA legacy_alter_table=ON;
ALTER TABLE capability_pack_references RENAME TO capability_pack_references_0166_old;
CREATE TABLE capability_pack_references (
 pack_id TEXT NOT NULL REFERENCES capability_pack_operations(pack_id) ON DELETE RESTRICT,
 kind TEXT NOT NULL,
 resource_key TEXT NOT NULL,
 ordinal INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('planned','ready','released','skipped')),
 PRIMARY KEY(pack_id,kind,resource_key),
 FOREIGN KEY(kind,resource_key) REFERENCES capability_pack_resources(kind,resource_key) ON DELETE RESTRICT
);
INSERT INTO capability_pack_references(pack_id,kind,resource_key,ordinal,state)
SELECT pack_id,kind,resource_key,ordinal,state FROM capability_pack_references_0166_old;
DROP TABLE capability_pack_references_0166_old;
CREATE INDEX idx_capability_pack_refs_resource ON capability_pack_references(kind,resource_key,state);
PRAGMA legacy_alter_table=OFF;
