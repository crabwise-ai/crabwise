CREATE TABLE IF NOT EXISTS events (
    id              TEXT PRIMARY KEY,
    timestamp       TEXT NOT NULL,
    agent_id        TEXT NOT NULL,
    agent_pid       INTEGER,
    action_type     TEXT NOT NULL,
    action          TEXT,
    arguments       TEXT,
    session_id      TEXT,
    parent_session_id TEXT,
    working_dir     TEXT,
    parser_version  TEXT,
    outcome         TEXT NOT NULL,
    commandments_evaluated TEXT,
    commandments_triggered TEXT,
    provider        TEXT,
    model           TEXT,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cost_usd        REAL,
    adapter_id      TEXT,
    adapter_type    TEXT,
    raw_payload_ref TEXT,
    prev_hash       TEXT,
    event_hash      TEXT NOT NULL,
    redacted        INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_agent_id ON events(agent_id);
CREATE INDEX IF NOT EXISTS idx_events_action_type ON events(action_type);
CREATE INDEX IF NOT EXISTS idx_events_outcome ON events(outcome);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS file_offsets (
    file_path  TEXT PRIMARY KEY,
    offset     INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chain_anchors (
    epoch      TEXT PRIMARY KEY,
    event_id   TEXT NOT NULL,
    event_hash TEXT NOT NULL,
    created_at TEXT NOT NULL
);

INSERT OR IGNORE INTO schema_version (version, applied_at)
VALUES (1, datetime('now'));
