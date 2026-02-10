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
	_, err := db.Exec(flowsSchema)
	return err
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
`
