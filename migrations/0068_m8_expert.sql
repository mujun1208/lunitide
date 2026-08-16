-- 0068 M8 FR-19 (T-8.10.x): the expert center and project-phase expert
-- collaboration. Experts are the productized form of the M7 AgentPersona
-- job-description; the runtime reuses M7 subagent.spawn read-only dispatch
-- and the M5/M6 execution kernel - no execution table is created here.
--
-- ExpertCatalog: enabled<->disabled->archived, division is the M7 closed
-- eight-skeleton whitelist, source pack|local|builtin; pack-sourced experts
-- follow the plugin lifecycle. ExpertVersion is an append-only WORM chain
-- (UPDATE/DELETE trips M8-043): six_section_digest = sha256(canonical
-- six-section body). ProjectPhaseExpertMounting pins a version at mount
-- time; at most 4 mounted experts per (project, phase) - enforced both by
-- the app pre-check and the trg_mount_limit trigger (M8-044).
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE expert_catalog (
    expert_id TEXT PRIMARY KEY CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    division TEXT NOT NULL CHECK (division IN ('engineering','design','product','project-management','testing','security','operations','data')),
    source TEXT NOT NULL CHECK (source IN ('pack','local','builtin')),
    origin_bundle_id TEXT REFERENCES plugin_bundles(bundle_id),
    current_version_id TEXT NOT NULL CHECK (length(current_version_id) = 26 AND substr(current_version_id, 1, 1) GLOB '[0-7]' AND current_version_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    state TEXT NOT NULL CHECK (state IN ('enabled','disabled','archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (subject_id, name)
);

CREATE TABLE expert_versions (
    version_id TEXT PRIMARY KEY CHECK (length(version_id) = 26 AND substr(version_id, 1, 1) GLOB '[0-7]' AND version_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    expert_id TEXT NOT NULL REFERENCES expert_catalog(expert_id),
    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 32),
    persona_ref TEXT NOT NULL CHECK (length(persona_ref) = 64 AND persona_ref NOT GLOB '*[^0-9a-f]*'),
    six_section_digest TEXT NOT NULL CHECK (length(six_section_digest) = 64 AND six_section_digest NOT GLOB '*[^0-9a-f]*'),
    change_note TEXT NOT NULL CHECK (length(change_note) BETWEEN 1 AND 2000),
    created_at TEXT NOT NULL,
    UNIQUE (expert_id, semver)
);

-- Expert versions are append-only (design DDL: UPDATE/DELETE -> M8-043).
CREATE TRIGGER trg_ev_append_only BEFORE UPDATE ON expert_versions
    BEGIN SELECT RAISE(ABORT, 'M8-043'); END;
CREATE TRIGGER trg_ev_nodelete BEFORE DELETE ON expert_versions
    BEGIN SELECT RAISE(ABORT, 'M8-043'); END;

CREATE TABLE project_phase_expert_mounting (
    mounting_id TEXT PRIMARY KEY CHECK (length(mounting_id) = 26 AND substr(mounting_id, 1, 1) GLOB '[0-7]' AND mounting_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL CHECK (length(project_id) = 26 AND substr(project_id, 1, 1) GLOB '[0-7]' AND project_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    phase_key TEXT NOT NULL CHECK (phase_key IN ('INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION','SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE','VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY','OPERATIONS_RETROSPECTIVE')),
    expert_id TEXT NOT NULL REFERENCES expert_catalog(expert_id),
    version_id TEXT NOT NULL REFERENCES expert_versions(version_id),
    state TEXT NOT NULL CHECK (state IN ('mounted','unmounted')),
    mounted_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (project_id, phase_key, expert_id)
);

-- At most 4 mounted experts per (project, phase) (design DDL: -> M8-044).
CREATE TRIGGER trg_mount_limit BEFORE INSERT ON project_phase_expert_mounting
    WHEN NEW.state = 'mounted' AND (SELECT COUNT(*) FROM project_phase_expert_mounting
        WHERE project_id = NEW.project_id AND phase_key = NEW.phase_key AND state = 'mounted') >= 4
    BEGIN SELECT RAISE(ABORT, 'M8-044'); END;

CREATE INDEX idx_ec_subject ON expert_catalog(subject_id, state);
CREATE INDEX idx_pem_project ON project_phase_expert_mounting(project_id, phase_key, state);
