-- 0106: colleague thread → real workspace session (tools / memory / audit)

CREATE TABLE people_thread_session (
    thread_id TEXT PRIMARY KEY CHECK (length(thread_id) = 26 AND substr(thread_id, 1, 1) GLOB '[0-7]' AND thread_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL UNIQUE CHECK (length(session_id) = 26 AND substr(session_id, 1, 1) GLOB '[0-7]' AND session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    created_at TEXT NOT NULL,
    FOREIGN KEY (thread_id) REFERENCES people_threads(thread_id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX ix_people_thread_session_session ON people_thread_session(session_id);
