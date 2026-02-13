package db

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY,
    parent_id       TEXT REFERENCES nodes(id),
    root_id         TEXT NOT NULL,
    slug            TEXT UNIQUE,
    node_type       TEXT NOT NULL CHECK(node_type IN ('question','answer','evidence','objection','precision','correction','synthesis','llm','resolution')),
    body            TEXT NOT NULL,
    author_id       TEXT NOT NULL,
    model_id        TEXT,
    score           INTEGER DEFAULT 0,
    temperature     TEXT DEFAULT 'cold' CHECK(temperature IN ('cold','warm','hot','critical')),
    status          TEXT DEFAULT 'open' CHECK(status IN ('open','answered','bounty','closed')),
    metadata        TEXT DEFAULT '{}',
    is_accepted     INTEGER DEFAULT 0,
    is_critical     INTEGER DEFAULT 0,
    child_count     INTEGER DEFAULT 0,
    view_count      INTEGER DEFAULT 0,
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
CREATE INDEX IF NOT EXISTS idx_nodes_slug ON nodes(slug) WHERE slug IS NOT NULL;

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
    role                    TEXT DEFAULT 'user' CHECK(role IN ('user','moderator','admin')),
    is_bot                  INTEGER DEFAULT 0 CHECK(is_bot IN (0, 1)),
    reputation              INTEGER DEFAULT 0,
    honor_rate              REAL DEFAULT 1.0,
    credits                 INTEGER DEFAULT 0,
    bountytreescore_total   INTEGER DEFAULT 0,
    bountytreescore_tags    TEXT DEFAULT '{}',
    created_at              DATETIME DEFAULT (datetime('now')),
    last_seen_at            DATETIME
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
    id              TEXT PRIMARY KEY,
    node_id         TEXT NOT NULL,
    sponsor_id      TEXT NOT NULL,
    amount          INTEGER NOT NULL,
    currency        TEXT DEFAULT 'credits',
    status          TEXT DEFAULT 'active' CHECK(status IN ('active','attributed','expired','contested')),
    attribution     TEXT DEFAULT 'vote' CHECK(attribution IN ('sponsor','vote','split')),
    winner_id       TEXT,
    expires_at      DATETIME,
    contested_at    DATETIME,
    contest_reason  TEXT,
    psp_ref         TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_bounties_node ON bounties(node_id);
CREATE INDEX IF NOT EXISTS idx_bounties_status ON bounties(status) WHERE status = 'active';

-- Bounty stacking: multiple sponsors can contribute to same bounty
CREATE TABLE IF NOT EXISTS bounty_contributions (
    id         TEXT PRIMARY KEY,
    bounty_id  TEXT NOT NULL REFERENCES bounties(id),
    sponsor_id TEXT NOT NULL,
    amount     INTEGER NOT NULL,
    created_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_bounty_contrib_bounty ON bounty_contributions(bounty_id);

-- Sources: URLs/documents attached to evidence nodes
CREATE TABLE IF NOT EXISTS sources (
    id          TEXT PRIMARY KEY,
    node_id     TEXT NOT NULL,
    url         TEXT NOT NULL,
    title       TEXT,
    domain      TEXT,
    content_hash TEXT,
    trust_score REAL DEFAULT 0.5,
    verified_at DATETIME,
    created_at  DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_sources_node ON sources(node_id);
CREATE INDEX IF NOT EXISTS idx_sources_domain ON sources(domain);

-- Credit ledger: bot economy transactions
CREATE TABLE IF NOT EXISTS credit_ledger (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    amount      INTEGER NOT NULL,
    balance     INTEGER NOT NULL,
    reason      TEXT NOT NULL,
    ref_type    TEXT,
    ref_id      TEXT,
    created_at  DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_credit_ledger_user ON credit_ledger(user_id);
CREATE INDEX IF NOT EXISTS idx_credit_ledger_time ON credit_ledger(created_at);

-- Reputation events: log of reputation changes
CREATE TABLE IF NOT EXISTS reputation_events (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    delta       INTEGER NOT NULL,
    reason      TEXT NOT NULL,
    ref_type    TEXT,
    ref_id      TEXT,
    created_at  DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_rep_events_user ON reputation_events(user_id);
CREATE INDEX IF NOT EXISTS idx_rep_events_time ON reputation_events(created_at);

-- Adversarial challenges: flow-based evaluation of nodes/trees
CREATE TABLE IF NOT EXISTS challenges (
    id              TEXT PRIMARY KEY,
    node_id         TEXT NOT NULL,
    flow_name       TEXT NOT NULL,
    status          TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed')),
    requested_by    TEXT NOT NULL,
    target_provider TEXT,
    target_model    TEXT,
    score           REAL,
    summary         TEXT,
    flow_id         TEXT,
    error           TEXT,
    started_at      DATETIME,
    completed_at    DATETIME,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_challenges_node ON challenges(node_id);
CREATE INDEX IF NOT EXISTS idx_challenges_status ON challenges(status);
CREATE INDEX IF NOT EXISTS idx_challenges_flow ON challenges(flow_name);

-- Moderation scores: multi-criteria quality assessment for nodes
CREATE TABLE IF NOT EXISTS moderation_scores (
    id              TEXT PRIMARY KEY,
    node_id         TEXT NOT NULL,
    evaluator       TEXT NOT NULL,
    eval_source     TEXT DEFAULT 'auto' CHECK(eval_source IN ('auto','human','challenge')),
    factual_score   REAL,
    source_score    REAL,
    argument_score  REAL,
    civility_score  REAL,
    overall_score   REAL,
    flags           TEXT DEFAULT '[]',
    notes           TEXT,
    challenge_id    TEXT REFERENCES challenges(id),
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_mod_scores_node ON moderation_scores(node_id);
CREATE INDEX IF NOT EXISTS idx_mod_scores_source ON moderation_scores(eval_source);

CREATE TABLE IF NOT EXISTS renders (
    id              TEXT PRIMARY KEY,
    resolution_id   TEXT NOT NULL,
    format          TEXT NOT NULL,
    model_id        TEXT,
    content         TEXT,
    fidelity_score  INTEGER,
    created_at      DATETIME DEFAULT (datetime('now'))
);

-- Observability: audit log
CREATE TABLE IF NOT EXISTS audit_log (
    entry_id TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL,
    action TEXT NOT NULL,
    transport TEXT NOT NULL DEFAULT 'http',
    user_id TEXT,
    request_id TEXT,
    parameters TEXT,
    result TEXT,
    error_message TEXT,
    duration_ms INTEGER,
    status TEXT NOT NULL DEFAULT 'success'
);
CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log(user_id);

-- Observability: SQL trace persistence
CREATE TABLE IF NOT EXISTS sql_traces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT,
    op TEXT NOT NULL,
    query TEXT NOT NULL,
    duration_us INTEGER NOT NULL,
    error TEXT,
    timestamp INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sql_traces_ts ON sql_traces(timestamp);
CREATE INDEX IF NOT EXISTS idx_sql_traces_tid ON sql_traces(trace_id) WHERE trace_id != '';
CREATE INDEX IF NOT EXISTS idx_sql_traces_slow ON sql_traces(duration_us) WHERE duration_us > 100000;

-- Flight control: MCP tools registry (hot-reload from SQLite)
CREATE TABLE IF NOT EXISTS mcp_tools_registry (
    tool_name TEXT PRIMARY KEY,
    tool_category TEXT NOT NULL,
    description TEXT NOT NULL,
    input_schema TEXT NOT NULL,
    handler_type TEXT NOT NULL CHECK(handler_type IN ('sql_query', 'sql_script')),
    handler_config TEXT NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1 CHECK(is_active IN (0, 1)),
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER,
    created_by TEXT,
    version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_mcp_tools_active ON mcp_tools_registry(is_active);

CREATE TABLE IF NOT EXISTS mcp_tools_history (
    history_id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name TEXT NOT NULL,
    tool_category TEXT NOT NULL,
    description TEXT NOT NULL,
    input_schema TEXT NOT NULL,
    handler_type TEXT NOT NULL,
    handler_config TEXT NOT NULL,
    version INTEGER NOT NULL,
    changed_by TEXT,
    changed_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    change_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_mcp_history_tool ON mcp_tools_history(tool_name, version DESC);

CREATE TRIGGER IF NOT EXISTS trg_mcp_tools_updated_at
AFTER UPDATE ON mcp_tools_registry
FOR EACH ROW
BEGIN
    UPDATE mcp_tools_registry SET updated_at = strftime('%s', 'now') WHERE tool_name = NEW.tool_name;
END;

-- Envelopes: persistent routing tickets for piece transit (no content stored)
CREATE TABLE IF NOT EXISTS envelopes (
    id              TEXT PRIMARY KEY,
    batch_id        TEXT,
    source_type     TEXT NOT NULL CHECK(source_type IN ('horostracker','witheout','api','mcp')),
    source_user_id  TEXT REFERENCES users(id),
    source_node_id  TEXT REFERENCES nodes(id),
    source_callback TEXT,
    piece_hash      TEXT NOT NULL,
    status          TEXT DEFAULT 'pending' CHECK(status IN ('pending','dispatched','processing','delivered','partial','failed','expired')),
    target_count    INTEGER DEFAULT 0,
    delivered_count INTEGER DEFAULT 0,
    error           TEXT,
    expires_at      DATETIME NOT NULL,
    created_at      DATETIME DEFAULT (datetime('now')),
    updated_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_envelopes_batch ON envelopes(batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_envelopes_user ON envelopes(source_user_id) WHERE source_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_envelopes_status ON envelopes(status) WHERE status NOT IN ('delivered','expired');
CREATE INDEX IF NOT EXISTS idx_envelopes_expires ON envelopes(expires_at) WHERE status NOT IN ('delivered','expired');

-- Envelope targets: multi-delivery destinations per envelope
CREATE TABLE IF NOT EXISTS envelope_targets (
    id              TEXT PRIMARY KEY,
    envelope_id     TEXT NOT NULL REFERENCES envelopes(id),
    target_type     TEXT NOT NULL CHECK(target_type IN ('horostracker','googledrive','webhook','email','s3','ipfs')),
    target_config   TEXT DEFAULT '{}',
    status          TEXT DEFAULT 'pending' CHECK(status IN ('pending','delivered','failed','skipped')),
    delivered_at    DATETIME,
    error           TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_envelope_targets_env ON envelope_targets(envelope_id);
CREATE INDEX IF NOT EXISTS idx_envelope_targets_status ON envelope_targets(status) WHERE status = 'pending';

-- Provider self-registration
CREATE TABLE IF NOT EXISTS providers (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    endpoint        TEXT,
    api_style       TEXT DEFAULT 'openai' CHECK(api_style IN ('openai','anthropic','gemini','custom')),
    models          TEXT DEFAULT '[]',
    capabilities    TEXT DEFAULT '[]',
    is_active       INTEGER DEFAULT 1,
    registered_by   TEXT,
    api_key_hash    TEXT,
    resolution_space INTEGER DEFAULT 0,
    resolution_criteria TEXT DEFAULT '{}',
    created_at      DATETIME DEFAULT (datetime('now')),
    last_seen_at    DATETIME
);
CREATE INDEX IF NOT EXISTS idx_providers_active ON providers(is_active) WHERE is_active = 1;

-- Dataset profiles: reusable export configurations
CREATE TABLE IF NOT EXISTS dataset_profiles (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    filters         TEXT DEFAULT '{}',
    options         TEXT DEFAULT '{}',
    format          TEXT DEFAULT 'jsonl' CHECK(format IN ('jsonl','csv')),
    split_ratio     TEXT,
    created_by      TEXT NOT NULL,
    created_at      DATETIME DEFAULT (datetime('now'))
);

-- Dataset runs: execution history
CREATE TABLE IF NOT EXISTS dataset_runs (
    id              TEXT PRIMARY KEY,
    profile_id      TEXT NOT NULL REFERENCES dataset_profiles(id),
    status          TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed')),
    row_count       INTEGER,
    file_size       INTEGER,
    created_at      DATETIME DEFAULT (datetime('now')),
    completed_at    DATETIME
);
CREATE INDEX IF NOT EXISTS idx_dataset_runs_profile ON dataset_runs(profile_id);

-- Preference pairs: DPO-ready training data
CREATE TABLE IF NOT EXISTS preference_pairs (
    id              TEXT PRIMARY KEY,
    question_id     TEXT NOT NULL,
    chosen_node_id  TEXT NOT NULL,
    rejected_node_id TEXT NOT NULL,
    signal_source   TEXT NOT NULL CHECK(signal_source IN ('vote','accept','bounty','challenge')),
    chosen_model    TEXT,
    rejected_model  TEXT,
    margin          REAL,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_pref_pairs_question ON preference_pairs(question_id);
CREATE INDEX IF NOT EXISTS idx_pref_pairs_models ON preference_pairs(chosen_model, rejected_model);

-- Custom benchmarks
CREATE TABLE IF NOT EXISTS benchmarks (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT,
    models          TEXT NOT NULL,
    filter_tags     TEXT DEFAULT '[]',
    filter_min_score INTEGER DEFAULT 0,
    flow_name       TEXT NOT NULL,
    metrics         TEXT DEFAULT '["fidelity","hallucination_count","source_accuracy","latency"]',
    status          TEXT DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed')),
    results         TEXT,
    replay_from     TEXT REFERENCES benchmarks(id),
    created_by      TEXT NOT NULL,
    created_at      DATETIME DEFAULT (datetime('now')),
    completed_at    DATETIME
);
CREATE INDEX IF NOT EXISTS idx_benchmarks_status ON benchmarks(status);

-- Deduplication clusters
CREATE TABLE IF NOT EXISTS dedup_clusters (
    id              TEXT PRIMARY KEY,
    canonical_id    TEXT NOT NULL REFERENCES nodes(id),
    method          TEXT DEFAULT 'embedding' CHECK(method IN ('exact','fuzzy','embedding')),
    created_at      DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS dedup_members (
    cluster_id      TEXT NOT NULL REFERENCES dedup_clusters(id),
    node_id         TEXT NOT NULL REFERENCES nodes(id),
    similarity      REAL NOT NULL,
    created_at      DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (cluster_id, node_id)
);
CREATE INDEX IF NOT EXISTS idx_dedup_members_node ON dedup_members(node_id);
`
