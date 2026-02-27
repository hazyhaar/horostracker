// CLAUDE:SUMMARY FlowsDB — separate SQLite database for LLM forensic traces (flow steps, replays, dispatches)
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// FlowsDB wraps the flows.db SQLite database for LLM forensic traces.
type FlowsDB struct {
	*sql.DB
}

func OpenFlows(path string) (*FlowsDB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating flows data dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening flows database: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging flows database: %w", err)
	}

	db := &FlowsDB{sqlDB}
	if err := db.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating flows database: %w", err)
	}

	return db, nil
}

func (db *FlowsDB) migrate() error {
	if _, err := db.Exec(flowsSchema); err != nil {
		return err
	}
	// Safe column additions (ignore errors if columns already exist)
	db.Exec(`ALTER TABLE flow_steps ADD COLUMN replay_of_id TEXT REFERENCES flow_steps(id)`)
	db.Exec(`ALTER TABLE flow_steps ADD COLUMN dispatch_id TEXT`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_flow_steps_replay ON flow_steps(replay_of_id) WHERE replay_of_id IS NOT NULL`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_flow_steps_dispatch ON flow_steps(dispatch_id) WHERE dispatch_id IS NOT NULL`)

	// v2: owner_id on available_models (NULL = auto-discovered, non-NULL = provider-registered)
	db.Exec(`ALTER TABLE available_models ADD COLUMN owner_id TEXT`)
	return nil
}

const flowsSchema = `
-- flow_steps: forensic trace for every LLM call
CREATE TABLE IF NOT EXISTS flow_steps (
    id              TEXT PRIMARY KEY,
    flow_id         TEXT NOT NULL,
    step_index      INTEGER NOT NULL,
    node_id         TEXT,
    model_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    system_prompt   TEXT,
    context_ids     TEXT DEFAULT '[]',
    response_raw    TEXT,
    response_parsed TEXT,
    tokens_in       INTEGER,
    tokens_out      INTEGER,
    latency_ms      INTEGER,
    finish_reason   TEXT,
    eval_score      REAL,
    error           TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_flow_steps_flow ON flow_steps(flow_id);
CREATE INDEX IF NOT EXISTS idx_flow_steps_node ON flow_steps(node_id) WHERE node_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_flow_steps_model ON flow_steps(model_id);
CREATE INDEX IF NOT EXISTS idx_flow_steps_time ON flow_steps(created_at);

-- llm_responses: structured LLM output linked to nodes
CREATE TABLE IF NOT EXISTS llm_responses (
    id              TEXT PRIMARY KEY,
    node_id         TEXT NOT NULL,
    flow_step_id    TEXT REFERENCES flow_steps(id),
    model_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    response_type   TEXT NOT NULL CHECK(response_type IN ('answer','objection','synthesis','resolution','correction','evidence')),
    content         TEXT NOT NULL,
    confidence      REAL,
    tokens_total    INTEGER,
    latency_ms      INTEGER,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_llm_responses_node ON llm_responses(node_id);
CREATE INDEX IF NOT EXISTS idx_llm_responses_model ON llm_responses(model_id);

-- llm_evals: evaluation of LLM outputs (fidelity, accuracy, etc.)
CREATE TABLE IF NOT EXISTS llm_evals (
    id              TEXT PRIMARY KEY,
    response_id     TEXT NOT NULL REFERENCES llm_responses(id),
    evaluator       TEXT NOT NULL,
    eval_type       TEXT NOT NULL CHECK(eval_type IN ('auto','human','cross_model')),
    score           REAL NOT NULL,
    dimensions      TEXT DEFAULT '{}',
    notes           TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_llm_evals_response ON llm_evals(response_id);
CREATE INDEX IF NOT EXISTS idx_llm_evals_type ON llm_evals(eval_type);

-- Replay: replay_of_id column on flow_steps (added via migration-safe approach)
-- Note: SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN,
-- so we create a separate migration table and use it as a flag.
CREATE TABLE IF NOT EXISTS _migrations (key TEXT PRIMARY KEY, applied_at DATETIME DEFAULT (datetime('now')));

-- Replay batches: bulk replay tracking
CREATE TABLE IF NOT EXISTS replay_batches (
    id                  TEXT PRIMARY KEY,
    original_model      TEXT NOT NULL,
    replay_model        TEXT NOT NULL,
    scope               TEXT NOT NULL,
    filter_tag          TEXT,
    total_steps         INTEGER,
    improvements        INTEGER,
    regressions         INTEGER,
    unchanged           INTEGER,
    status              TEXT DEFAULT 'running' CHECK(status IN ('running','completed','failed')),
    started_at          DATETIME DEFAULT (datetime('now')),
    completed_at        DATETIME
);
CREATE INDEX IF NOT EXISTS idx_replay_batches_status ON replay_batches(status);

-- Dispatches: parallel multi-model inference
CREATE TABLE IF NOT EXISTS dispatches (
    id          TEXT PRIMARY KEY,
    prompt_hash TEXT NOT NULL,
    models      TEXT NOT NULL,
    status      TEXT DEFAULT 'running',
    created_at  DATETIME DEFAULT (datetime('now')),
    completed_at DATETIME
);

-- ============================================================
-- VACF Dynamic Workflows System
-- ============================================================

-- workflows: definition of each workflow
CREATE TABLE IF NOT EXISTS workflows (
    workflow_id   TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    description   TEXT,
    workflow_type TEXT NOT NULL CHECK(workflow_type IN (
        'decompose','critique','source','factcheck','analyse','synthese',
        'reformulation','media_export','contradiction_detection','completude',
        'traduction','classification_epistemique','workflow_validation','model_discovery'
    )),
    owner_id      TEXT NOT NULL,
    owner_role    TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'draft' CHECK(status IN (
        'draft','pending_validation','validated','rejected','active','archived'
    )),
    version       INTEGER DEFAULT 1,
    pre_prompt_template TEXT,
    created_at    DATETIME DEFAULT (datetime('now')),
    updated_at    DATETIME DEFAULT (datetime('now')),
    validated_by  TEXT,
    validated_at  DATETIME,
    rejection_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status);
CREATE INDEX IF NOT EXISTS idx_workflows_owner ON workflows(owner_id);
CREATE INDEX IF NOT EXISTS idx_workflows_type ON workflows(workflow_type);

-- workflow_steps: each row is one VACF step
CREATE TABLE IF NOT EXISTS workflow_steps (
    step_id       TEXT PRIMARY KEY,
    workflow_id   TEXT NOT NULL REFERENCES workflows(workflow_id),
    step_order    INTEGER NOT NULL,
    step_name     TEXT NOT NULL,
    step_type     TEXT NOT NULL CHECK(step_type IN ('llm','sql','http','check')),
    provider      TEXT,
    model         TEXT,
    prompt_template  TEXT,
    system_prompt    TEXT,
    config_json      TEXT DEFAULT '{}',
    criteria_list_id TEXT,
    timeout_ms       INTEGER DEFAULT 30000,
    retry_max        INTEGER DEFAULT 2,
    fan_group        TEXT,
    created_at       DATETIME DEFAULT (datetime('now')),
    UNIQUE(workflow_id, step_order, step_name)
);
CREATE INDEX IF NOT EXISTS idx_wf_steps_workflow ON workflow_steps(workflow_id);
CREATE INDEX IF NOT EXISTS idx_wf_steps_order ON workflow_steps(workflow_id, step_order);

-- criteria_lists: reusable criteria referenced by check steps and prompt templates
CREATE TABLE IF NOT EXISTS criteria_lists (
    list_id     TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    items_json  TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    created_at  DATETIME DEFAULT (datetime('now')),
    updated_at  DATETIME DEFAULT (datetime('now'))
);

-- available_models: dynamic catalogue populated by discovery
CREATE TABLE IF NOT EXISTS available_models (
    model_id          TEXT PRIMARY KEY,
    provider          TEXT NOT NULL,
    model_name        TEXT NOT NULL,
    display_name      TEXT,
    context_window    INTEGER,
    is_available      INTEGER DEFAULT 1,
    last_check_at     DATETIME,
    last_error        TEXT,
    capabilities_json TEXT DEFAULT '{}',
    discovered_at     DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_avail_models_provider ON available_models(provider);
CREATE INDEX IF NOT EXISTS idx_avail_models_available ON available_models(is_available);

-- workflow_runs: each execution of a workflow
CREATE TABLE IF NOT EXISTS workflow_runs (
    run_id          TEXT PRIMARY KEY,
    workflow_id     TEXT NOT NULL REFERENCES workflows(workflow_id),
    node_id         TEXT,
    initiated_by    TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN (
        'pending','running','completed','failed','cancelled'
    )),
    pre_prompt      TEXT,
    batch_id        TEXT,
    total_steps     INTEGER,
    completed_steps INTEGER DEFAULT 0,
    result_json     TEXT,
    error           TEXT,
    started_at      DATETIME,
    completed_at    DATETIME,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_wf_runs_workflow ON workflow_runs(workflow_id);
CREATE INDEX IF NOT EXISTS idx_wf_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_wf_runs_batch ON workflow_runs(batch_id) WHERE batch_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_wf_runs_initiated ON workflow_runs(initiated_by);

-- workflow_step_runs: each execution of a step — ACID per transaction
CREATE TABLE IF NOT EXISTS workflow_step_runs (
    step_run_id   TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES workflow_runs(run_id),
    step_id       TEXT NOT NULL REFERENCES workflow_steps(step_id),
    step_order    INTEGER NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK(status IN (
        'pending','running','completed','failed','skipped'
    )),
    input_json    TEXT,
    output_json   TEXT,
    model_used    TEXT,
    provider_used TEXT,
    tokens_in     INTEGER,
    tokens_out    INTEGER,
    latency_ms    INTEGER,
    error         TEXT,
    attempt       INTEGER DEFAULT 1,
    started_at    DATETIME,
    completed_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_wf_step_runs_run ON workflow_step_runs(run_id, step_order);
CREATE INDEX IF NOT EXISTS idx_wf_step_runs_status ON workflow_step_runs(status);

-- workflow_audit_log: maximum observability — every workflow event is traced
CREATE TABLE IF NOT EXISTS workflow_audit_log (
    log_id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id          TEXT,
    step_run_id     TEXT,
    event_type      TEXT NOT NULL,
    event_data_json TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_wf_audit_run ON workflow_audit_log(run_id);
CREATE INDEX IF NOT EXISTS idx_wf_audit_type ON workflow_audit_log(event_type);

-- model_grants: granular access control for provider/model per user or role
CREATE TABLE IF NOT EXISTS model_grants (
    grant_id     TEXT PRIMARY KEY,
    grantee_type TEXT NOT NULL CHECK(grantee_type IN ('user','role')),
    grantee_id   TEXT NOT NULL,
    model_id     TEXT NOT NULL,
    step_type    TEXT NOT NULL CHECK(step_type IN ('llm','sql','http','check','*')),
    effect       TEXT NOT NULL DEFAULT 'allow' CHECK(effect IN ('allow','deny')),
    created_by   TEXT NOT NULL,
    created_at   DATETIME DEFAULT (datetime('now')),
    UNIQUE(grantee_type, grantee_id, model_id, step_type)
);
CREATE INDEX IF NOT EXISTS idx_model_grants_grantee ON model_grants(grantee_type, grantee_id);
CREATE INDEX IF NOT EXISTS idx_model_grants_model ON model_grants(model_id);

-- operator_groups: provider-scoped groups for bulk grant management
CREATE TABLE IF NOT EXISTS operator_groups (
    group_id    TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  DATETIME DEFAULT (datetime('now')),
    UNIQUE(provider_id, name)
);
CREATE INDEX IF NOT EXISTS idx_operator_groups_provider ON operator_groups(provider_id);

-- operator_group_members: membership of operators in provider groups
CREATE TABLE IF NOT EXISTS operator_group_members (
    group_id    TEXT NOT NULL REFERENCES operator_groups(group_id) ON DELETE CASCADE,
    operator_id TEXT NOT NULL,
    added_at    DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY(group_id, operator_id)
);
CREATE INDEX IF NOT EXISTS idx_operator_group_members_operator ON operator_group_members(operator_id);
`
