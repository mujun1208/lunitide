-- 0118 MRO ops ledgers: due, tools/chems, parts, plan, collaboration todos.
CREATE TABLE mro_due_items (
    due_id TEXT PRIMARY KEY CHECK (length(due_id) = 26 AND substr(due_id, 1, 1) GLOB '[0-7]' AND due_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 64),
    kind TEXT NOT NULL CHECK (kind IN ('CAL','FH','FC','BC','LLP','CHECK','AD','MEL')),
    limit_value REAL NOT NULL DEFAULT 0,
    used_value REAL,
    due_at TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '' CHECK (length(source) <= 128),
    updated_at TEXT NOT NULL
);
CREATE TABLE mro_utilization_events (
    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 64),
    hours REAL NOT NULL DEFAULT 0,
    cycles REAL NOT NULL DEFAULT 0,
    battery_cycles REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE TABLE mro_components (
    component_id TEXT PRIMARY KEY CHECK (length(component_id) = 26 AND substr(component_id, 1, 1) GLOB '[0-7]' AND component_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    sn TEXT NOT NULL CHECK (length(sn) BETWEEN 1 AND 64),
    pn TEXT NOT NULL CHECK (length(pn) BETWEEN 1 AND 64),
    life_count REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE TABLE mro_life_events (
    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    component_id TEXT NOT NULL REFERENCES mro_components(component_id),
    kind TEXT NOT NULL CHECK (kind IN ('install','remove','transfer','repair','scrap')),
    occurred_at TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 512)
);
CREATE TABLE mro_pirep_drafts (
    draft_id TEXT PRIMARY KEY CHECK (length(draft_id) = 26 AND substr(draft_id, 1, 1) GLOB '[0-7]' AND draft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    body_json TEXT NOT NULL CHECK (length(body_json) >= 2),
    state TEXT NOT NULL CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_tools (
    tool_id TEXT PRIMARY KEY CHECK (length(tool_id) = 26 AND substr(tool_id, 1, 1) GLOB '[0-7]' AND tool_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tool_no TEXT NOT NULL CHECK (length(tool_no) BETWEEN 1 AND 64),
    sn TEXT NOT NULL DEFAULT '' CHECK (length(sn) <= 64),
    location TEXT NOT NULL DEFAULT '' CHECK (length(location) <= 64),
    holder TEXT NOT NULL DEFAULT '' CHECK (length(holder) <= 64),
    calib_due TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ready' CHECK (length(status) BETWEEN 1 AND 32),
    updated_at TEXT NOT NULL
);
CREATE TABLE mro_tool_loans (
    loan_id TEXT PRIMARY KEY CHECK (length(loan_id) = 26 AND substr(loan_id, 1, 1) GLOB '[0-7]' AND loan_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tool_id TEXT NOT NULL REFERENCES mro_tools(tool_id),
    holder TEXT NOT NULL CHECK (length(holder) BETWEEN 1 AND 64),
    out_at TEXT NOT NULL,
    in_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE mro_chem_lots (
    lot_id TEXT PRIMARY KEY CHECK (length(lot_id) = 26 AND substr(lot_id, 1, 1) GLOB '[0-7]' AND lot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    lot_no TEXT NOT NULL CHECK (length(lot_no) BETWEEN 1 AND 64),
    parent_lot_id TEXT REFERENCES mro_chem_lots(lot_id),
    qty REAL NOT NULL DEFAULT 0,
    expires TEXT NOT NULL DEFAULT '',
    sds_doc TEXT NOT NULL DEFAULT '' CHECK (length(sds_doc) <= 128)
);
CREATE TABLE mro_chem_uses (
    use_id TEXT PRIMARY KEY CHECK (length(use_id) = 26 AND substr(use_id, 1, 1) GLOB '[0-7]' AND use_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    lot_id TEXT NOT NULL REFERENCES mro_chem_lots(lot_id),
    tail_no TEXT NOT NULL DEFAULT '' CHECK (length(tail_no) <= 32),
    wo TEXT NOT NULL DEFAULT '' CHECK (length(wo) <= 64),
    tech TEXT NOT NULL DEFAULT '' CHECK (length(tech) <= 64)
);
CREATE TABLE mro_kits (
    kit_id TEXT PRIMARY KEY CHECK (length(kit_id) = 26 AND substr(kit_id, 1, 1) GLOB '[0-7]' AND kit_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128)
);
CREATE TABLE mro_kit_items (
    kit_id TEXT NOT NULL REFERENCES mro_kits(kit_id) ON DELETE CASCADE,
    pn TEXT NOT NULL CHECK (length(pn) BETWEEN 1 AND 64),
    required REAL NOT NULL DEFAULT 0,
    on_hand REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (kit_id, pn)
);
CREATE TABLE mro_parts_stock (
    pn TEXT PRIMARY KEY CHECK (length(pn) BETWEEN 1 AND 64),
    qty REAL NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT 'local' CHECK (length(source) BETWEEN 1 AND 32)
);
CREATE TABLE mro_alternates (
    pn_from TEXT NOT NULL CHECK (length(pn_from) BETWEEN 1 AND 64),
    pn_to TEXT NOT NULL CHECK (length(pn_to) BETWEEN 1 AND 64),
    cert_ok INTEGER NOT NULL CHECK (cert_ok IN (0, 1)),
    effectivity TEXT NOT NULL DEFAULT '' CHECK (length(effectivity) <= 128),
    PRIMARY KEY (pn_from, pn_to)
);
CREATE TABLE mro_aog_cases (
    case_id TEXT PRIMARY KEY CHECK (length(case_id) = 26 AND substr(case_id, 1, 1) GLOB '[0-7]' AND case_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    pn TEXT NOT NULL DEFAULT '' CHECK (length(pn) <= 64),
    qty TEXT NOT NULL DEFAULT '' CHECK (length(qty) <= 32),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 512),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_po_drafts (
    draft_id TEXT PRIMARY KEY CHECK (length(draft_id) = 26 AND substr(draft_id, 1, 1) GLOB '[0-7]' AND draft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    pn TEXT NOT NULL CHECK (length(pn) BETWEEN 1 AND 64),
    qty TEXT NOT NULL DEFAULT '' CHECK (length(qty) <= 32),
    price TEXT NOT NULL DEFAULT '' CHECK (length(price) <= 32),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_interval_rules (
    rule_id TEXT PRIMARY KEY CHECK (length(rule_id) = 26 AND substr(rule_id, 1, 1) GLOB '[0-7]' AND rule_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_key TEXT NOT NULL CHECK (length(task_key) BETWEEN 1 AND 64),
    interval_value REAL NOT NULL,
    unit TEXT NOT NULL CHECK (length(unit) BETWEEN 1 AND 16),
    version TEXT NOT NULL DEFAULT '1' CHECK (length(version) <= 32),
    effective_from TEXT NOT NULL DEFAULT '',
    source_cite TEXT NOT NULL DEFAULT '' CHECK (length(source_cite) <= 256)
);
CREATE TABLE mro_task_card_templates (
    template_id TEXT PRIMARY KEY CHECK (length(template_id) = 26 AND substr(template_id, 1, 1) GLOB '[0-7]' AND template_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 128),
    hours REAL NOT NULL DEFAULT 0
);
CREATE TABLE mro_work_packages (
    package_id TEXT PRIMARY KEY CHECK (length(package_id) = 26 AND substr(package_id, 1, 1) GLOB '[0-7]' AND package_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 128),
    sources_json TEXT NOT NULL CHECK (length(sources_json) >= 2),
    hours REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE TABLE mro_wp_tasks (
    package_id TEXT NOT NULL REFERENCES mro_work_packages(package_id) ON DELETE CASCADE,
    task_key TEXT NOT NULL CHECK (length(task_key) BETWEEN 1 AND 64),
    PRIMARY KEY (package_id, task_key)
);
CREATE TABLE mro_schedule_assignments (
    assign_id TEXT PRIMARY KEY CHECK (length(assign_id) = 26 AND substr(assign_id, 1, 1) GLOB '[0-7]' AND assign_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    tail_no TEXT NOT NULL CHECK (length(tail_no) BETWEEN 1 AND 32),
    check_name TEXT NOT NULL CHECK (length(check_name) BETWEEN 1 AND 64),
    start_on TEXT NOT NULL DEFAULT '',
    end_on TEXT NOT NULL DEFAULT '',
    hours REAL NOT NULL DEFAULT 0,
    skill TEXT NOT NULL DEFAULT '' CHECK (length(skill) <= 64)
);
CREATE TABLE mro_capacity_slots (
    slot_id TEXT PRIMARY KEY CHECK (length(slot_id) = 26 AND substr(slot_id, 1, 1) GLOB '[0-7]' AND slot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill TEXT NOT NULL CHECK (length(skill) BETWEEN 1 AND 64),
    hours REAL NOT NULL DEFAULT 0
);
CREATE TABLE mro_interval_change_drafts (
    draft_id TEXT PRIMARY KEY CHECK (length(draft_id) = 26 AND substr(draft_id, 1, 1) GLOB '[0-7]' AND draft_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_key TEXT NOT NULL CHECK (length(task_key) BETWEEN 1 AND 64),
    mpd_cite TEXT NOT NULL CHECK (length(mpd_cite) BETWEEN 1 AND 256),
    fleet_cite TEXT NOT NULL CHECK (length(fleet_cite) BETWEEN 1 AND 256),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','confirmed','rejected')),
    created_at TEXT NOT NULL
);
CREATE TABLE mro_ops_todos (
    todo_id TEXT PRIMARY KEY CHECK (length(todo_id) = 26 AND substr(todo_id, 1, 1) GLOB '[0-7]' AND todo_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    kind TEXT NOT NULL CHECK (kind IN ('kit_staging','parts_request','due_recompute')),
    ref TEXT NOT NULL CHECK (length(ref) BETWEEN 1 AND 64),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','cancelled')),
    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 256),
    created_at TEXT NOT NULL
);
