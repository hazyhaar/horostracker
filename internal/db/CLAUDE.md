# db

Responsabilite: Couche base de donnees SQLite — 3 databases separees (nodes.db, flows.db, metrics.db) avec schema DDL, migrations idempotentes, CRUD pour nodes/users/votes/bounties/envelopes/challenges/workflows/groups/safety.
Depend de: `modernc.org/sqlite`, `github.com/hazyhaar/pkg/idgen`
Dependants: `internal/api`, `internal/llm`, `internal/mcp`, `internal/export`
Point d'entree: `db.go` (Open), `flows.go` (OpenFlows), `metrics.go` (OpenMetrics)
Types cles: `DB`, `FlowsDB`, `MetricsDB`, `Node`, `User`, `Envelope`, `Challenge`, `Workflow`, `OperatorGroup`, `SafetyResult`
Invariants: WAL mode + foreign_keys=ON + busy_timeout=5000. Les migrations dans safeAlter() sont idempotentes (ignorent "duplicate column"). NewID() utilise hazyhaar/pkg/idgen pour des IDs base-36 de 12 chars.
NE PAS: Utiliser mattn/go-sqlite3. Modifier le schema sans ajouter la migration correspondante dans safeAlter(). Oublier COALESCE(visibility,'public') dans les SELECT sur nodes.
