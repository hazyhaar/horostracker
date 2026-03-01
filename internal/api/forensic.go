// CLAUDE:SUMMARY Forensic API — query LLM call logs, inspect model usage, and download raw flows/metrics SQLite databases
package api

import (
	"net/http"
	"strconv"
)

func (a *API) RegisterForensicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/forensic/calls", a.handleForensicCalls)
	mux.HandleFunc("GET /api/forensic/model/{modelID}", a.handleForensicModel)
	mux.HandleFunc("GET /api/forensic/flows.db", a.handleForensicFlowsDB)
	mux.HandleFunc("GET /api/forensic/metrics.db", a.handleForensicMetricsDB)
}

func (a *API) handleForensicCalls(w http.ResponseWriter, r *http.Request) {
	if a.flowsDB == nil {
		jsonError(w, "flows database not configured", http.StatusServiceUnavailable)
		return
	}

	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")
	from := r.URL.Query().Get("from")
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	query := `SELECT id, flow_id, step_index, COALESCE(node_id,''), model_id, provider,
		prompt, COALESCE(system_prompt,''), COALESCE(response_raw,''),
		COALESCE(tokens_in,0), COALESCE(tokens_out,0), COALESCE(latency_ms,0),
		COALESCE(finish_reason,''), COALESCE(error,''), created_at
		FROM flow_steps WHERE 1=1`
	args := []interface{}{}

	if provider != "" {
		query += ` AND provider = ?`
		args = append(args, provider)
	}
	if model != "" {
		query += ` AND model_id = ?`
		args = append(args, model)
	}
	if from != "" {
		query += ` AND created_at >= ?`
		args = append(args, from)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := a.flowsDB.Query(query, args...)
	if err != nil {
		jsonError(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type call struct {
		ID           string `json:"id"`
		FlowID       string `json:"flow_id"`
		StepIndex    int    `json:"step_index"`
		NodeID       string `json:"node_id,omitempty"`
		ModelID      string `json:"model_id"`
		Provider     string `json:"provider"`
		Prompt       string `json:"prompt"`
		SystemPrompt string `json:"system_prompt,omitempty"`
		Response     string `json:"response"`
		TokensIn     int    `json:"tokens_in"`
		TokensOut    int    `json:"tokens_out"`
		LatencyMs    int    `json:"latency_ms"`
		FinishReason string `json:"finish_reason,omitempty"`
		Error        string `json:"error,omitempty"`
		CreatedAt    string `json:"created_at"`
	}

	var calls []call
	for rows.Next() {
		var c call
		if err := rows.Scan(&c.ID, &c.FlowID, &c.StepIndex, &c.NodeID, &c.ModelID, &c.Provider,
			&c.Prompt, &c.SystemPrompt, &c.Response,
			&c.TokensIn, &c.TokensOut, &c.LatencyMs,
			&c.FinishReason, &c.Error, &c.CreatedAt); err != nil {
			continue
		}
		calls = append(calls, c)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"calls": calls,
		"count": len(calls),
	})
}

func (a *API) handleForensicModel(w http.ResponseWriter, r *http.Request) {
	if a.flowsDB == nil {
		jsonError(w, "flows database not configured", http.StatusServiceUnavailable)
		return
	}

	modelID := r.PathValue("modelID")

	row := a.flowsDB.QueryRow(`
		SELECT
			COUNT(*) as total_calls,
			COALESCE(SUM(tokens_in), 0) as total_tokens_in,
			COALESCE(SUM(tokens_out), 0) as total_tokens_out,
			COALESCE(AVG(latency_ms), 0) as avg_latency,
			COUNT(CASE WHEN error IS NOT NULL AND error != '' THEN 1 END) as error_count
		FROM flow_steps WHERE model_id = ?`, modelID)

	var totalCalls, totalTokensIn, totalTokensOut, errorCount int
	var avgLatency float64
	if err := row.Scan(&totalCalls, &totalTokensIn, &totalTokensOut, &avgLatency, &errorCount); err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}

	// Get P50/P95/P99 latencies
	latencies := struct {
		P50 int `json:"p50"`
		P95 int `json:"p95"`
		P99 int `json:"p99"`
	}{}

	if totalCalls > 0 {
		_ = a.flowsDB.QueryRow(`SELECT latency_ms FROM flow_steps WHERE model_id = ? AND latency_ms > 0 ORDER BY latency_ms LIMIT 1 OFFSET ?`,
			modelID, totalCalls/2).Scan(&latencies.P50)
		_ = a.flowsDB.QueryRow(`SELECT latency_ms FROM flow_steps WHERE model_id = ? AND latency_ms > 0 ORDER BY latency_ms LIMIT 1 OFFSET ?`,
			modelID, totalCalls*95/100).Scan(&latencies.P95)
		_ = a.flowsDB.QueryRow(`SELECT latency_ms FROM flow_steps WHERE model_id = ? AND latency_ms > 0 ORDER BY latency_ms LIMIT 1 OFFSET ?`,
			modelID, totalCalls*99/100).Scan(&latencies.P99)
	}

	// Flow distribution
	flowRows, err := a.flowsDB.Query(`
		SELECT COALESCE(
			(SELECT fs2.node_id FROM flow_steps fs2 WHERE fs2.flow_id = flow_steps.flow_id AND fs2.step_index = 0 LIMIT 1),
			'unknown'
		) as flow_type, COUNT(*) as cnt
		FROM flow_steps WHERE model_id = ? GROUP BY flow_id ORDER BY cnt DESC LIMIT 10`, modelID)

	var flowDist []map[string]interface{}
	if err == nil {
		defer flowRows.Close()
		for flowRows.Next() {
			var ft string
			var cnt int
			if flowRows.Scan(&ft, &cnt) == nil {
				flowDist = append(flowDist, map[string]interface{}{"flow_type": ft, "count": cnt})
			}
		}
	}

	errorRate := float64(0)
	if totalCalls > 0 {
		errorRate = float64(errorCount) / float64(totalCalls)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"model_id":        modelID,
		"total_calls":     totalCalls,
		"total_tokens_in": totalTokensIn,
		"total_tokens_out": totalTokensOut,
		"avg_latency_ms":  int(avgLatency),
		"latency_percentiles": latencies,
		"error_count":     errorCount,
		"error_rate":      errorRate,
		"flow_distribution": flowDist,
	})
}

func (a *API) handleForensicFlowsDB(w http.ResponseWriter, r *http.Request) {
	if a.flowsDB == nil {
		jsonError(w, "flows database not available", http.StatusServiceUnavailable)
		return
	}

	// Check config flag (open-data mode)
	if a.fedConfig != nil && !a.fedConfig.Enabled {
		jsonError(w, "database download disabled (enable federation for open-data mode)", http.StatusForbidden)
		return
	}

	dbPath := a.flowsDBPath
	if dbPath == "" {
		jsonError(w, "flows database path not configured", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="flows.db"`)
	w.Header().Set("Content-Type", "application/x-sqlite3")
	http.ServeFile(w, r, dbPath)
}

func (a *API) handleForensicMetricsDB(w http.ResponseWriter, r *http.Request) {
	if a.metricsDB == nil {
		jsonError(w, "metrics database not available", http.StatusServiceUnavailable)
		return
	}

	if a.fedConfig != nil && !a.fedConfig.Enabled {
		jsonError(w, "database download disabled (enable federation for open-data mode)", http.StatusForbidden)
		return
	}

	dbPath := a.metricsDBPath
	if dbPath == "" {
		jsonError(w, "metrics database path not configured", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="metrics.db"`)
	w.Header().Set("Content-Type", "application/x-sqlite3")
	http.ServeFile(w, r, dbPath)
}
