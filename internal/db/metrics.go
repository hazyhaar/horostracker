// CLAUDE:SUMMARY MetricsDB — separate SQLite database for Go native metrics (HTTP requests, runtime stats)
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// MetricsDB wraps the metrics.db SQLite database for Go native metrics.
type MetricsDB struct {
	*sql.DB
}

func OpenMetrics(path string) (*MetricsDB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating metrics data dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening metrics database: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging metrics database: %w", err)
	}

	db := &MetricsDB{sqlDB}
	if err := db.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating metrics database: %w", err)
	}

	return db, nil
}

func (db *MetricsDB) migrate() error {
	_, err := db.Exec(metricsSchema)
	return err
}

const metricsSchema = `
-- HTTP request metrics
CREATE TABLE IF NOT EXISTS http_requests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    method      TEXT NOT NULL,
    path        TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    user_id     TEXT,
    timestamp   INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_http_req_ts ON http_requests(timestamp);
CREATE INDEX IF NOT EXISTS idx_http_req_path ON http_requests(path);

-- MCP tool call metrics
CREATE TABLE IF NOT EXISTS mcp_calls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_name   TEXT NOT NULL,
    duration_ms INTEGER NOT NULL,
    success     INTEGER NOT NULL DEFAULT 1,
    user_id     TEXT,
    transport   TEXT NOT NULL DEFAULT 'quic',
    timestamp   INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_mcp_calls_ts ON mcp_calls(timestamp);
CREATE INDEX IF NOT EXISTS idx_mcp_calls_tool ON mcp_calls(tool_name);

-- LLM provider call metrics
CREATE TABLE IF NOT EXISTS llm_calls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider    TEXT NOT NULL,
    model       TEXT NOT NULL,
    tokens_in   INTEGER,
    tokens_out  INTEGER,
    latency_ms  INTEGER NOT NULL,
    success     INTEGER NOT NULL DEFAULT 1,
    error       TEXT,
    timestamp   INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_llm_calls_ts ON llm_calls(timestamp);
CREATE INDEX IF NOT EXISTS idx_llm_calls_provider ON llm_calls(provider);

-- Daily aggregates (computed by background job)
CREATE TABLE IF NOT EXISTS daily_stats (
    date        TEXT NOT NULL,
    metric      TEXT NOT NULL,
    value       REAL NOT NULL,
    PRIMARY KEY (date, metric)
);
`

// RecordHTTPRequest logs an HTTP request metric.
func (db *MetricsDB) RecordHTTPRequest(method, path string, statusCode, durationMs int, userID string) {
	_, _ = db.Exec(`INSERT INTO http_requests (method, path, status_code, duration_ms, user_id)
		VALUES (?, ?, ?, ?, ?)`, method, path, statusCode, durationMs, userID)
}

// RecordLLMCall logs an LLM provider call metric.
func (db *MetricsDB) RecordLLMCall(provider, model string, tokensIn, tokensOut, latencyMs int, success bool, errMsg string) {
	s := 1
	if !success {
		s = 0
	}
	_, _ = db.Exec(`INSERT INTO llm_calls (provider, model, tokens_in, tokens_out, latency_ms, success, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, provider, model, tokensIn, tokensOut, latencyMs, s, errMsg)
}
