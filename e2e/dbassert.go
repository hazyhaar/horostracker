// CLAUDE:SUMMARY Direct SQLite assertion helpers for E2E tests — persistent connections to nodes, flows, and metrics DBs
package e2e

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// DBAssert provides direct SQLite assertions on the databases.
// It keeps persistent connections to avoid file descriptor exhaustion.
type DBAssert struct {
	nodesPath   string
	flowsPath   string
	metricsPath string

	mu        sync.Mutex
	nodesConn *sql.DB
	flowsConn *sql.DB
}

// NewDBAssert creates assertion helpers for direct database verification.
func NewDBAssert(nodesDB, flowsDB, metricsDB string) *DBAssert {
	return &DBAssert{
		nodesPath:   nodesDB,
		flowsPath:   flowsDB,
		metricsPath: metricsDB,
	}
}

// Close releases persistent connections.
func (d *DBAssert) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.nodesConn != nil {
		d.nodesConn.Close()
		d.nodesConn = nil
	}
	if d.flowsConn != nil {
		d.flowsConn.Close()
		d.flowsConn = nil
	}
}

func (d *DBAssert) nodes() (*sql.DB, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.nodesConn != nil {
		return d.nodesConn, nil
	}
	db, err := sql.Open("sqlite", "file:"+d.nodesPath+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d.nodesConn = db
	return db, nil
}

func (d *DBAssert) flows() (*sql.DB, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.flowsConn != nil {
		return d.flowsConn, nil
	}
	db, err := sql.Open("sqlite", "file:"+d.flowsPath+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d.flowsConn = db
	return db, nil
}

// AssertNodeExists verifies that a node with the given ID exists in nodes.db.
func (d *DBAssert) AssertNodeExists(t *testing.T, nodeID string) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = ?", nodeID).Scan(&count)
	if err != nil {
		t.Fatalf("querying node %s: %v", nodeID, err)
	}
	if count == 0 {
		t.Errorf("node %s does not exist in nodes.db", nodeID)
	}
}

// AssertNodeField verifies a specific field value for a node.
func (d *DBAssert) AssertNodeField(t *testing.T, nodeID, field string, expected interface{}) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	var actual interface{}
	const qField = `SELECT %s FROM nodes WHERE id = ?`
	err = db.QueryRow(fmt.Sprintf(qField, field), nodeID).Scan(&actual)
	if err != nil {
		t.Fatalf("querying node %s field %s: %v", nodeID, field, err)
	}
	if fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expected) {
		t.Errorf("node %s.%s = %v, want %v", nodeID, field, actual, expected)
	}
}

// AssertNodeFieldGTE verifies a numeric field is >= threshold.
func (d *DBAssert) AssertNodeFieldGTE(t *testing.T, nodeID, field string, threshold int) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	var actual int
	const qCoalesce = `SELECT COALESCE(%s, 0) FROM nodes WHERE id = ?`
	err = db.QueryRow(fmt.Sprintf(qCoalesce, field), nodeID).Scan(&actual)
	if err != nil {
		t.Fatalf("querying node %s field %s: %v", nodeID, field, err)
	}
	if actual < threshold {
		t.Errorf("node %s.%s = %d, want >= %d", nodeID, field, actual, threshold)
	}
}

// AssertRowCount verifies the number of rows matching a condition.
func (d *DBAssert) AssertRowCount(t *testing.T, table, where string, args []interface{}, expected int) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	q := countQuery(table, where)
	var count int
	err = db.QueryRow(q, args...).Scan(&count)
	if err != nil {
		t.Fatalf("counting %s rows: %v", table, err)
	}
	if count != expected {
		t.Errorf("table %s (where %s): count = %d, want %d", table, where, count, expected)
	}
}

// AssertRowCountGTE verifies the number of rows is >= threshold.
func (d *DBAssert) AssertRowCountGTE(t *testing.T, table, where string, args []interface{}, threshold int) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	q := countQuery(table, where)
	var count int
	err = db.QueryRow(q, args...).Scan(&count)
	if err != nil {
		t.Fatalf("counting %s rows: %v", table, err)
	}
	if count < threshold {
		t.Errorf("table %s (where %s): count = %d, want >= %d", table, where, count, threshold)
	}
}

// countQuery builds a COUNT(*) query for a table with optional WHERE clause.
func countQuery(table, where string) string {
	const qCount = `SELECT COUNT(*) FROM %s`
	q := fmt.Sprintf(qCount, table)
	if where != "" {
		q += " WHERE " + where
	}
	return q
}

// AssertTemperature verifies the temperature of a node.
func (d *DBAssert) AssertTemperature(t *testing.T, nodeID, expected string) {
	t.Helper()
	d.AssertNodeField(t, nodeID, "temperature", expected)
}

// QueryFlowSteps returns flow steps from flows.db for a given flow_id.
func (d *DBAssert) QueryFlowSteps(t *testing.T, flowID string) []map[string]interface{} {
	t.Helper()
	db, err := d.flows()
	if err != nil {
		t.Fatalf("opening flows.db: %v", err)
	}

	rows, err := db.Query(`
		SELECT step_index, model_id, provider, prompt, response_raw, tokens_in, tokens_out, latency_ms
		FROM flow_steps WHERE flow_id = ? ORDER BY step_index`, flowID)
	if err != nil {
		t.Fatalf("querying flow_steps for %s: %v", flowID, err)
	}
	defer rows.Close()

	var steps []map[string]interface{}
	for rows.Next() {
		var stepIndex int
		var modelID, provider, prompt, responseRaw sql.NullString
		var tokensIn, tokensOut, latencyMs sql.NullInt64

		if err := rows.Scan(&stepIndex, &modelID, &provider, &prompt, &responseRaw, &tokensIn, &tokensOut, &latencyMs); err != nil {
			t.Fatalf("scanning flow step: %v", err)
		}
		step := map[string]interface{}{
			"step_index":   stepIndex,
			"model_id":     modelID.String,
			"provider":     provider.String,
			"prompt":       prompt.String,
			"response_raw": responseRaw.String,
			"tokens_in":    tokensIn.Int64,
			"tokens_out":   tokensOut.Int64,
			"latency_ms":   latencyMs.Int64,
		}
		steps = append(steps, step)
	}
	return steps
}

// CountFlowSteps returns the number of flow steps for a flow_id.
func (d *DBAssert) CountFlowSteps(t *testing.T, flowID string) int {
	t.Helper()
	db, err := d.flows()
	if err != nil {
		t.Fatalf("opening flows.db: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM flow_steps WHERE flow_id = ?", flowID).Scan(&count)
	if err != nil {
		t.Fatalf("counting flow steps for %s: %v", flowID, err)
	}
	return count
}

// QueryScalar runs a single-value query on nodes.db and returns it.
func (d *DBAssert) QueryScalar(t *testing.T, query string, args ...interface{}) interface{} {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	var result interface{}
	err = db.QueryRow(query, args...).Scan(&result)
	if err != nil {
		t.Fatalf("scalar query: %v", err)
	}
	return result
}

// QueryScalarInt runs a single integer query on nodes.db.
func (d *DBAssert) QueryScalarInt(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}

	var result int
	err = db.QueryRow(query, args...).Scan(&result)
	if err != nil {
		t.Fatalf("scalar int query: %v", err)
	}
	return result
}

// openAndQuery is a helper that queries a single string value from nodes.db.
func (d *DBAssert) openAndQuery(t *testing.T, query string, dest *string) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	db.QueryRow(query).Scan(dest)
}

// AssertNodeVisibility verifies the visibility of a node.
func (d *DBAssert) AssertNodeVisibility(t *testing.T, nodeID, expected string) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	var vis string
	err = db.QueryRow("SELECT COALESCE(visibility, 'public') FROM nodes WHERE id = ?", nodeID).Scan(&vis)
	if err != nil {
		t.Fatalf("querying visibility for %s: %v", nodeID, err)
	}
	if vis != expected {
		t.Errorf("node %s visibility = %q, want %q", nodeID, vis, expected)
	}
}

// AssertNodeVisibilityNot verifies the visibility of a node is NOT a given value.
func (d *DBAssert) AssertNodeVisibilityNot(t *testing.T, nodeID, notExpected string) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	var vis string
	err = db.QueryRow("SELECT COALESCE(visibility, 'public') FROM nodes WHERE id = ?", nodeID).Scan(&vis)
	if err != nil {
		t.Fatalf("querying visibility for %s: %v", nodeID, err)
	}
	if vis == notExpected {
		t.Errorf("node %s visibility should not be %q", nodeID, notExpected)
	}
}

// SetNodeVisibility sets the visibility of a node directly in nodes.db.
func (d *DBAssert) SetNodeVisibility(t *testing.T, nodeID, visibility string) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	_, err = db.Exec("UPDATE nodes SET visibility = ? WHERE id = ?", visibility, nodeID)
	if err != nil {
		t.Fatalf("setting visibility for %s: %v", nodeID, err)
	}
}

// QueryStrata returns a map of stratum id to min_role from the visibility_strata table.
func (d *DBAssert) QueryStrata(t *testing.T) map[string]string {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	rows, err := db.Query("SELECT id, min_role FROM visibility_strata")
	if err != nil {
		t.Fatalf("querying visibility_strata: %v", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var id, minRole string
		if err := rows.Scan(&id, &minRole); err != nil {
			t.Fatalf("scanning stratum: %v", err)
		}
		result[id] = minRole
	}
	return result
}

// QueryCloneExists checks if a clone exists for a given source node ID.
func (d *DBAssert) QueryCloneExists(t *testing.T, sourceID string) (string, bool) {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	var cloneID string
	err = db.QueryRow("SELECT clone_id FROM node_clones WHERE source_id = ?", sourceID).Scan(&cloneID)
	if err != nil {
		return "", false
	}
	return cloneID, true
}

// QueryCloneVisibility returns the visibility of a clone node.
func (d *DBAssert) QueryCloneVisibility(t *testing.T, cloneID string) string {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	var vis string
	err = db.QueryRow("SELECT COALESCE(visibility, 'public') FROM nodes WHERE id = ?", cloneID).Scan(&vis)
	if err != nil {
		t.Fatalf("querying clone visibility for %s: %v", cloneID, err)
	}
	return vis
}

// QueryCloneParent returns the parent_id of a clone node.
func (d *DBAssert) QueryCloneParent(t *testing.T, cloneID string) string {
	t.Helper()
	db, err := d.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	var parentID string
	err = db.QueryRow("SELECT COALESCE(parent_id, '') FROM nodes WHERE id = ?", cloneID).Scan(&parentID)
	if err != nil {
		t.Fatalf("querying clone parent for %s: %v", cloneID, err)
	}
	return parentID
}
