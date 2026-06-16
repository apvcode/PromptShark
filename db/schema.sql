CREATE TABLE IF NOT EXISTS Sessions (
    id TEXT PRIMARY KEY,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    replay_target INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS Steps (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    step_index INTEGER NOT NULL,
    request_payload TEXT,
    response_payload TEXT,
    tokens_used INTEGER,
    FOREIGN KEY(session_id) REFERENCES Sessions(id) ON DELETE CASCADE
);
