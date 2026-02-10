package db

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY,
    parent_id       TEXT REFERENCES nodes(id),
    root_id         TEXT NOT NULL,
    node_type       TEXT NOT NULL CHECK(node_type IN ('question','answer','evidence','objection','precision','correction','synthesis','llm','resolution')),
    body            TEXT NOT NULL,
    author_id       TEXT NOT NULL,
    model_id        TEXT,
    score           INTEGER DEFAULT 0,
    temperature     TEXT DEFAULT 'cold' CHECK(temperature IN ('cold','warm','hot','critical')),
    metadata        TEXT DEFAULT '{}',
    is_accepted     INTEGER DEFAULT 0,
    is_critical     INTEGER DEFAULT 0,
    child_count     INTEGER DEFAULT 0,
    depth           INTEGER DEFAULT 0,
    origin_instance TEXT DEFAULT 'local',
    signature       TEXT DEFAULT '',
    binary_hash     TEXT DEFAULT '',
    created_at      DATETIME DEFAULT (datetime('now')),
    updated_at      DATETIME DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_nodes_parent ON nodes(parent_id);
CREATE INDEX IF NOT EXISTS idx_nodes_root ON nodes(root_id);
CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(node_type);
CREATE INDEX IF NOT EXISTS idx_nodes_author ON nodes(author_id);
CREATE INDEX IF NOT EXISTS idx_nodes_temp ON nodes(temperature);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(body, content=nodes, content_rowid=rowid);

CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO nodes_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, body) VALUES('delete', old.rowid, old.body);
END;
CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, body) VALUES('delete', old.rowid, old.body);
    INSERT INTO nodes_fts(rowid, body) VALUES (new.rowid, new.body);
END;

CREATE TABLE IF NOT EXISTS users (
    id                      TEXT PRIMARY KEY,
    handle                  TEXT UNIQUE NOT NULL,
    email                   TEXT UNIQUE,
    password_hash           TEXT NOT NULL,
    reputation              INTEGER DEFAULT 0,
    bountytreescore_total   INTEGER DEFAULT 0,
    bountytreescore_tags    TEXT DEFAULT '{}',
    created_at              DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS votes (
    user_id    TEXT NOT NULL,
    node_id    TEXT NOT NULL,
    value      INTEGER NOT NULL CHECK(value IN (-1, 1)),
    created_at DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, node_id)
);

CREATE TABLE IF NOT EXISTS thanks (
    from_user  TEXT NOT NULL,
    to_node    TEXT NOT NULL,
    message    TEXT,
    created_at DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (from_user, to_node)
);

CREATE TABLE IF NOT EXISTS tags (
    node_id TEXT NOT NULL,
    tag     TEXT NOT NULL,
    PRIMARY KEY (node_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);

CREATE TABLE IF NOT EXISTS bounties (
    id         TEXT PRIMARY KEY,
    node_id    TEXT NOT NULL,
    sponsor_id TEXT NOT NULL,
    amount     INTEGER NOT NULL,
    currency   TEXT DEFAULT 'credits',
    status     TEXT DEFAULT 'active' CHECK(status IN ('active','attributed','expired','contested')),
    winner_id  TEXT,
    expires_at DATETIME,
    psp_ref    TEXT,
    created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS adversarial_flags (
    node_id    TEXT PRIMARY KEY,
    flags      TEXT NOT NULL,
    objective  TEXT,
    revealed   INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS renders (
    id              TEXT PRIMARY KEY,
    resolution_id   TEXT NOT NULL,
    format          TEXT NOT NULL,
    model_id        TEXT,
    content         TEXT,
    fidelity_score  INTEGER,
    created_at      DATETIME DEFAULT (datetime('now'))
);
`
