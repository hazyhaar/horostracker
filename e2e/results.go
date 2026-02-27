// CLAUDE:SUMMARY SQLite-backed test results logger — records pass/fail/skip with request/response for E2E test runs
package e2e

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// ResultsDB logs test execution results into a SQLite database.
type ResultsDB struct {
	db *sql.DB
	mu sync.Mutex
}

var (
	globalResults   *ResultsDB
	globalResultsMu sync.Mutex
)

const resultsSchema = `
CREATE TABLE IF NOT EXISTS test_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    test_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pass','fail','skip')),
    duration_ms INTEGER NOT NULL,
    request TEXT,
    response TEXT,
    error TEXT,
    created_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_test_results_name ON test_results(test_name);
CREATE INDEX IF NOT EXISTS idx_test_results_status ON test_results(status);
`

// OpenResultsDB opens or creates the test_results.db in the given directory.
func OpenResultsDB(dir string) (*ResultsDB, error) {
	dbPath := filepath.Join(dir, "test_results.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(resultsSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &ResultsDB{db: db}, nil
}

// InitGlobalResults initializes the shared results database.
func InitGlobalResults(dir string) {
	globalResultsMu.Lock()
	defer globalResultsMu.Unlock()
	if globalResults != nil {
		return
	}
	rdb, err := OpenResultsDB(dir)
	if err != nil {
		// Non-fatal: tests still run, just no result persistence
		return
	}
	globalResults = rdb
}

// CloseGlobalResults closes the shared results database.
func CloseGlobalResults() {
	globalResultsMu.Lock()
	defer globalResultsMu.Unlock()
	if globalResults != nil {
		globalResults.db.Close()
		globalResults = nil
	}
}

// Record logs a test result. Call via defer in each test.
func Record(t *testing.T, start time.Time, reqData, respData interface{}) {
	globalResultsMu.Lock()
	rdb := globalResults
	globalResultsMu.Unlock()
	if rdb == nil {
		return
	}

	duration := time.Since(start).Milliseconds()

	status := "pass"
	if t.Failed() {
		status = "fail"
	}
	if t.Skipped() {
		status = "skip"
	}

	var reqJSON, respJSON string
	if reqData != nil {
		b, _ := json.Marshal(reqData)
		reqJSON = string(b)
	}
	if respData != nil {
		b, _ := json.Marshal(respData)
		respJSON = string(b)
	}

	rdb.mu.Lock()
	defer rdb.mu.Unlock()
	rdb.db.Exec(`INSERT INTO test_results (test_name, status, duration_ms, request, response)
		VALUES (?, ?, ?, ?, ?)`, t.Name(), status, duration, reqJSON, respJSON)
}

// ResultsPath returns the path to the test results database.
func ResultsPath() string {
	dir, _ := os.Getwd()
	return filepath.Join(dir, "test_results.db")
}
