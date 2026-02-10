package mcp

import (
	"database/sql"
	"log/slog"
)

// SeedDefaultTools inserts default dynamic MCP tools into the registry if empty.
// These are flight-control SQL tools that let MCP clients introspect the instance.
func SeedDefaultTools(db *sql.DB) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mcp_tools_registry").Scan(&count); err != nil {
		slog.Warn("seed: cannot check registry", "error", err)
		return
	}
	if count > 0 {
		return // already seeded
	}

	tools := []struct {
		name, category, desc, schema, handlerType, config string
	}{
		{
			name:        "instance_stats",
			category:    "observability",
			desc:        "Get instance statistics: node count, user count, question count, active bounties",
			schema:      `{"type":"object","properties":{}}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT (SELECT COUNT(*) FROM nodes) AS total_nodes, (SELECT COUNT(*) FROM nodes WHERE node_type='question') AS questions, (SELECT COUNT(*) FROM users) AS users, (SELECT COUNT(*) FROM bounties WHERE status='active') AS active_bounties, (SELECT COUNT(*) FROM nodes WHERE temperature IN ('hot','critical')) AS hot_nodes",
				"result_format": "object"
			}`,
		},
		{
			name:        "recent_activity",
			category:    "observability",
			desc:        "Get recent node activity (last N nodes created)",
			schema:      `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results","default":10}},"required":[]}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT id, node_type, substr(body, 1, 100) AS body_preview, author_id, temperature, score, created_at FROM nodes ORDER BY created_at DESC LIMIT ?",
				"params": ["limit"],
				"result_format": "array"
			}`,
		},
		{
			name:        "audit_recent",
			category:    "observability",
			desc:        "Get recent audit log entries",
			schema:      `{"type":"object","properties":{"limit":{"type":"integer","description":"Max entries","default":20}},"required":[]}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT entry_id, action, transport, user_id, status, duration_ms, timestamp FROM audit_log ORDER BY timestamp DESC LIMIT ?",
				"params": ["limit"],
				"result_format": "array"
			}`,
		},
		{
			name:        "slow_queries",
			category:    "observability",
			desc:        "Get SQL queries slower than 100ms",
			schema:      `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results","default":10}},"required":[]}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT op, query, duration_us, error, timestamp FROM sql_traces WHERE duration_us > 100000 ORDER BY duration_us DESC LIMIT ?",
				"params": ["limit"],
				"result_format": "array"
			}`,
		},
		{
			name:        "temperature_distribution",
			category:    "analytics",
			desc:        "Get distribution of node temperatures",
			schema:      `{"type":"object","properties":{}}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT temperature, COUNT(*) AS count FROM nodes GROUP BY temperature ORDER BY count DESC",
				"result_format": "array"
			}`,
		},
		{
			name:        "top_contributors",
			category:    "analytics",
			desc:        "Get top contributors by node count",
			schema:      `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results","default":10}},"required":[]}`,
			handlerType: "sql_query",
			config: `{
				"query": "SELECT u.handle, u.reputation, COUNT(n.id) AS node_count FROM users u LEFT JOIN nodes n ON n.author_id = u.id GROUP BY u.id ORDER BY node_count DESC LIMIT ?",
				"params": ["limit"],
				"result_format": "array"
			}`,
		},
	}

	for _, t := range tools {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO mcp_tools_registry
				(tool_name, tool_category, description, input_schema, handler_type, handler_config)
			VALUES (?, ?, ?, ?, ?, ?)`,
			t.name, t.category, t.desc, t.schema, t.handlerType, t.config)
		if err != nil {
			slog.Warn("seed: insert tool", "tool", t.name, "error", err)
		}
	}

	slog.Info("seeded default dynamic MCP tools", "count", len(tools))
}
