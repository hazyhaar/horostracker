// CLAUDE:SUMMARY Benchmark API endpoints — create, list, get, export, and replay multi-model benchmark runs
package api

import (
	"encoding/json"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
)

func (a *API) RegisterBenchmarkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/benchmark/create", a.handleCreateBenchmark)
	mux.HandleFunc("GET /api/benchmarks", a.handleListBenchmarks)
	mux.HandleFunc("GET /api/benchmark/{id}", a.handleGetBenchmark)
	mux.HandleFunc("GET /api/benchmark/{id}/export", a.handleExportBenchmark)
	mux.HandleFunc("POST /api/benchmark/{id}/replay", a.handleReplayBenchmark)
}

func (a *API) handleCreateBenchmark(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Models         []string `json:"models"`
		FilterTags     []string `json:"filter_tags"`
		FilterMinScore int      `json:"filter_min_score"`
		FlowName       string   `json:"flow_name"`
		Metrics        []string `json:"metrics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.FlowName == "" || len(req.Models) == 0 {
		jsonError(w, "name, flow_name, and models are required", http.StatusBadRequest)
		return
	}

	if len(req.Metrics) == 0 {
		req.Metrics = []string{"fidelity", "hallucination_count", "source_accuracy", "latency"}
	}

	id := db.NewID()
	modelsJSON, _ := json.Marshal(req.Models)
	tagsJSON, _ := json.Marshal(req.FilterTags)
	metricsJSON, _ := json.Marshal(req.Metrics)

	_, err := a.db.Exec(`INSERT INTO benchmarks (id, name, description, models, filter_tags, filter_min_score, flow_name, metrics, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Description, string(modelsJSON), string(tagsJSON),
		req.FilterMinScore, req.FlowName, string(metricsJSON), claims.UserID)
	if err != nil {
		jsonError(w, "creation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]string{"id": id, "name": req.Name, "status": "pending"})
}

func (a *API) handleListBenchmarks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, COALESCE(description,''), models, flow_name, status, created_by, created_at, completed_at
		FROM benchmarks ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type bmk struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Description string  `json:"description,omitempty"`
		Models      string  `json:"models"`
		FlowName    string  `json:"flow_name"`
		Status      string  `json:"status"`
		CreatedBy   string  `json:"created_by"`
		CreatedAt   string  `json:"created_at"`
		CompletedAt *string `json:"completed_at,omitempty"`
	}

	var benchmarks []bmk
	for rows.Next() {
		var b bmk
		if rows.Scan(&b.ID, &b.Name, &b.Description, &b.Models, &b.FlowName, &b.Status, &b.CreatedBy, &b.CreatedAt, &b.CompletedAt) == nil {
			benchmarks = append(benchmarks, b)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{"benchmarks": benchmarks, "count": len(benchmarks)})
}

func (a *API) handleGetBenchmark(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row := a.db.QueryRow(`SELECT id, name, COALESCE(description,''), models, filter_tags, filter_min_score,
		flow_name, metrics, status, COALESCE(results,'{}'), created_by, created_at, completed_at
		FROM benchmarks WHERE id = ?`, id)

	var name, description, models, filterTags, flowName, metrics, status, results, createdBy, createdAt string
	var filterMinScore int
	var completedAt *string
	if err := row.Scan(&id, &name, &description, &models, &filterTags, &filterMinScore,
		&flowName, &metrics, &status, &results, &createdBy, &createdAt, &completedAt); err != nil {
		jsonError(w, "benchmark not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"id": id, "name": name, "description": description,
		"models": models, "filter_tags": filterTags, "filter_min_score": filterMinScore,
		"flow_name": flowName, "metrics": metrics, "status": status,
		"results": results, "created_by": createdBy, "created_at": createdAt, "completed_at": completedAt,
	})
}

func (a *API) handleExportBenchmark(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var results string
	if err := a.db.QueryRow(`SELECT COALESCE(results,'{}') FROM benchmarks WHERE id = ?`, id).Scan(&results); err != nil {
		jsonError(w, "benchmark not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="benchmark-`+id+`.json"`)
	_, _ = w.Write([]byte(results))
}

func (a *API) handleReplayBenchmark(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")

	// Load original benchmark config
	var name, models, flowName, metrics string
	var filterMinScore int
	if err := a.db.QueryRow(`SELECT name, models, flow_name, metrics, filter_min_score FROM benchmarks WHERE id = ?`, id).
		Scan(&name, &models, &flowName, &metrics, &filterMinScore); err != nil {
		jsonError(w, "benchmark not found", http.StatusNotFound)
		return
	}

	var req struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Models) == 0 {
		jsonError(w, "models required for replay", http.StatusBadRequest)
		return
	}

	newID := db.NewID()
	newModels, _ := json.Marshal(req.Models)
	_, _ = a.db.Exec(`INSERT INTO benchmarks (id, name, description, models, filter_min_score, flow_name, metrics, replay_from, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, "replay:"+name, "Replay of "+id, string(newModels), filterMinScore, flowName, metrics, id, claims.UserID)

	jsonResp(w, http.StatusCreated, map[string]string{"id": newID, "replay_from": id, "status": "pending"})
}
