// CLAUDE:SUMMARY Main SQLite database wrapper — open with WAL/FK pragmas, auto-migrate schema, safe ALTER helpers
package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
		path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return db, nil
}

func (db *DB) migrate() error {
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}
	return db.safeAlter()
}

// safeAlter applies idempotent ALTER TABLE statements for columns that
// cannot be expressed in CREATE TABLE IF NOT EXISTS (pre-existing DBs).
func (db *DB) safeAlter() error {
	alters := []string{
		`ALTER TABLE nodes ADD COLUMN visibility TEXT DEFAULT 'public'`,
		`ALTER TABLE nodes ADD COLUMN deleted_at DATETIME`,
		`ALTER TABLE nodes ADD COLUMN decomposed_from TEXT REFERENCES nodes(id)`,
		`ALTER TABLE sources ADD COLUMN content_text TEXT`,
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil {
			// "duplicate column name" is expected on subsequent runs
			if !isDuplicateColumn(err) {
				return err
			}
		}
	}

	// Migrate CHECK constraint on node_type to piece/claim
	db.migrateNodeTypeConstraint()

	// Migrate node_clones table to drop legacy question_id column
	db.migrateNodeClones()

	// Seed visibility strata (idempotent via INSERT OR IGNORE)
	strata := []struct{ id, role string; ord int }{
		{"public", "anon", 0},
		{"research", "researcher", 1},
		{"provider", "provider", 2},
		{"instance", "operator", 3},
	}
	for _, s := range strata {
		_, _ = db.Exec("INSERT OR IGNORE INTO visibility_strata (id, min_role, ordinal) VALUES (?, ?, ?)",
			s.id, s.role, s.ord)
	}

	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate column") ||
		strings.Contains(err.Error(), "already exists"))
}

// migrateNodeTypeToPieceClaim rebuilds the nodes table to use the piece/claim
// ontology instead of the legacy 10-type system. It maps evidence → piece and
// all other legacy types → claim. This is idempotent: it only runs when the
// CHECK constraint does not already contain 'piece'.
func (db *DB) migrateNodeTypeConstraint() {
	var ddl string
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='nodes'").Scan(&ddl)
	if ddl == "" || strings.Contains(ddl, "'piece'") {
		return // already migrated or fresh DB
	}
	slog.Info("migrating nodes table: converting legacy types to piece/claim ontology")
	// Disable FK checks before transaction — SQLite requires this outside tx
	_, _ = db.Exec("PRAGMA foreign_keys = OFF")
	defer func() { _, _ = db.Exec("PRAGMA foreign_keys = ON") }()

	tx, err := db.Begin()
	if err != nil {
		slog.Error("migration failed", "phase", "begin", "error", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Rebuild table with new CHECK constraint and remap types in one step.
	// The old CHECK constraint prevents in-place UPDATE, so we remap via
	// CASE WHEN during the INSERT into the new table.
	steps := []string{
		`ALTER TABLE nodes RENAME TO _nodes_old`,
		`CREATE TABLE nodes (
			id              TEXT PRIMARY KEY,
			parent_id       TEXT REFERENCES nodes(id),
			root_id         TEXT NOT NULL,
			slug            TEXT UNIQUE,
			node_type       TEXT NOT NULL CHECK(node_type IN ('piece','claim')),
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
			updated_at      DATETIME DEFAULT (datetime('now')),
			visibility      TEXT DEFAULT 'public',
			deleted_at      DATETIME,
			decomposed_from TEXT REFERENCES nodes(id)
		)`,
		`INSERT INTO nodes SELECT
			id, parent_id, root_id, slug,
			CASE WHEN node_type = 'evidence' THEN 'piece' ELSE 'claim' END,
			body, author_id, model_id, score, temperature, status, metadata,
			is_accepted, is_critical, child_count, view_count, depth,
			origin_instance, signature, binary_hash, created_at, updated_at,
			visibility, deleted_at, decomposed_from
		FROM _nodes_old`,
		`DROP TABLE _nodes_old`,
	}
	for _, s := range steps {
		if _, err := tx.Exec(s); err != nil {
			slog.Error("migration failed", "statement", s, "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("migration commit failed", "error", err)
		return
	}
	slog.Info("migration complete: node_type constraint updated to piece/claim")
}

// migrateNodeClones rebuilds the node_clones table to drop the legacy
// question_id column. Idempotent: only runs when question_id exists.
func (db *DB) migrateNodeClones() {
	var ddl string
	_ = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='node_clones'").Scan(&ddl)
	if ddl == "" || !strings.Contains(ddl, "question_id") {
		return // already migrated or fresh DB
	}
	slog.Info("migrating node_clones table: dropping legacy question_id column")
	_, _ = db.Exec("PRAGMA foreign_keys = OFF")
	defer func() { _, _ = db.Exec("PRAGMA foreign_keys = ON") }()

	tx, err := db.Begin()
	if err != nil {
		slog.Error("node_clones migration failed", "phase", "begin", "error", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	steps := []string{
		`ALTER TABLE node_clones RENAME TO _node_clones_old`,
		`CREATE TABLE node_clones (
			source_id  TEXT NOT NULL,
			clone_id   TEXT NOT NULL,
			created_at DATETIME DEFAULT (datetime('now')),
			PRIMARY KEY (source_id, clone_id)
		)`,
		`INSERT INTO node_clones (source_id, clone_id, created_at)
			SELECT source_id, clone_id, cloned_at FROM _node_clones_old`,
		`DROP TABLE _node_clones_old`,
	}
	for _, s := range steps {
		if _, err := tx.Exec(s); err != nil {
			slog.Error("node_clones migration failed", "statement", s, "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("node_clones migration commit failed", "error", err)
		return
	}
	slog.Info("migration complete: node_clones table updated")
}
