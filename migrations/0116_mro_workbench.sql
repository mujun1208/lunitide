-- 0116 MRO workbench: fleet, manuals with sections, defect drafts.
CREATE TABLE mro_aircraft (
    aircraft_id TEXT PRIMARY KEY CHECK (length(aircraft_id) = 26 AND substr(aircraft_id, 1, 1) GLOB '[0-7]' AND aircraft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL UNIQUE CHECK (length(tail_no) BETWEEN 1 AND 32),
    msn TEXT NOT NULL DEFAULT '' CHECK (length(msn) <= 32),
    model TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 64),
    config TEXT NOT NULL DEFAULT '' CHECK (length(config) <= 128),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_manuals (
    manual_id TEXT PRIMARY KEY CHECK (length(manual_id) = 26 AND substr(manual_id, 1, 1) GLOB '[0-7]' AND manual_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 256),
    doc_type TEXT NOT NULL CHECK (doc_type IN ('AMM','IPC','TSM','FIM','WDM','CMM','MEL','SB','AD','EO','POLICY')),
    revision TEXT NOT NULL CHECK (length(revision) BETWEEN 1 AND 64),
    status TEXT NOT NULL CHECK (status IN ('controlled','uncontrolled','superseded')),
    ata TEXT NOT NULL DEFAULT '' CHECK (length(ata) <= 16),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_manual_docs (
    manual_id TEXT NOT NULL REFERENCES mro_manuals(manual_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL CHECK (length(document_id) = 26 AND substr(document_id, 1, 1) GLOB '[0-7]' AND document_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    part_no INTEGER NOT NULL CHECK (part_no >= 1),
    PRIMARY KEY (manual_id, document_id),
    UNIQUE (manual_id, part_no)
);
CREATE TABLE mro_defect_drafts (
    draft_id TEXT PRIMARY KEY CHECK (length(draft_id) = 26 AND substr(draft_id, 1, 1) GLOB '[0-7]' AND draft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    symptoms_json TEXT NOT NULL CHECK (length(symptoms_json) >= 2),
    state TEXT NOT NULL CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
