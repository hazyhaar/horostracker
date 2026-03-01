// CLAUDE:SUMMARY Dataset profile API — CRUD for export profiles, run dataset generation, export preferences/adversarial/moderation
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
)

func (a *API) RegisterDatasetRoutes(mux *http.ServeMux) {
	// Dataset profiles CRUD
	mux.HandleFunc("POST /api/dataset/profiles", a.handleCreateDatasetProfile)
	mux.HandleFunc("GET /api/dataset/profiles", a.handleListDatasetProfiles)
	mux.HandleFunc("GET /api/dataset/profiles/{id}", a.handleGetDatasetProfile)
	mux.HandleFunc("DELETE /api/dataset/profiles/{id}", a.handleDeleteDatasetProfile)
	mux.HandleFunc("POST /api/dataset/profiles/{id}/run", a.handleRunDatasetProfile)
	mux.HandleFunc("GET /api/dataset/runs/{id}", a.handleGetDatasetRun)

	// Specialized exports
	mux.HandleFunc("GET /api/export/preferences", a.handleExportPreferences)
	mux.HandleFunc("GET /api/export/adversarial", a.handleExportAdversarial)
	mux.HandleFunc("GET /api/export/moderation", a.handleExportModeration)
}

func (a *API) handleCreateDatasetProfile(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name          string          `json:"name"`
		FilterTags    json.RawMessage `json:"filter_tags"`
		FilterTypes   json.RawMessage `json:"filter_types"`
		FilterModels  json.RawMessage `json:"filter_models"`
		FilterMinTemp string          `json:"filter_min_temp"`
		IncludeFlows  bool            `json:"include_flows"`
		IncludeReplays bool           `json:"include_replays"`
		Format        string          `json:"format"`
		SplitRatio    string          `json:"split_ratio"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Format == "" {
		req.Format = "jsonl"
	}

	id := db.NewID()
	filtersJSON, _ := json.Marshal(map[string]interface{}{
		"tags":     req.FilterTags,
		"types":    req.FilterTypes,
		"models":   req.FilterModels,
		"min_temp": req.FilterMinTemp,
	})
	optionsJSON, _ := json.Marshal(map[string]interface{}{
		"include_flows":   req.IncludeFlows,
		"include_replays": req.IncludeReplays,
	})

	_, err := a.db.Exec(`INSERT INTO dataset_profiles (id, name, filters, options, format, split_ratio, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, string(filtersJSON), string(optionsJSON), req.Format, req.SplitRatio, claims.UserID)
	if err != nil {
		jsonError(w, "creation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]string{"id": id, "name": req.Name})
}

func (a *API) handleListDatasetProfiles(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, filters, options, format, COALESCE(split_ratio,''), created_by, created_at
		FROM dataset_profiles ORDER BY created_at DESC`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type profile struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Filters    string `json:"filters"`
		Options    string `json:"options"`
		Format     string `json:"format"`
		SplitRatio string `json:"split_ratio,omitempty"`
		CreatedBy  string `json:"created_by"`
		CreatedAt  string `json:"created_at"`
	}

	var profiles []profile
	for rows.Next() {
		var p profile
		if rows.Scan(&p.ID, &p.Name, &p.Filters, &p.Options, &p.Format, &p.SplitRatio, &p.CreatedBy, &p.CreatedAt) == nil {
			profiles = append(profiles, p)
		}
	}

	jsonResp(w, http.StatusOK, profiles)
}

func (a *API) handleGetDatasetProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row := a.db.QueryRow(`SELECT id, name, filters, options, format, COALESCE(split_ratio,''), created_by, created_at
		FROM dataset_profiles WHERE id = ?`, id)

	var name, filters, options, format, splitRatio, createdBy, createdAt string
	if err := row.Scan(&id, &name, &filters, &options, &format, &splitRatio, &createdBy, &createdAt); err != nil {
		jsonError(w, "profile not found", http.StatusNotFound)
		return
	}

	// Count runs
	var runCount int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM dataset_runs WHERE profile_id = ?`, id).Scan(&runCount)

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"id": id, "name": name, "filters": filters, "options": options,
		"format": format, "split_ratio": splitRatio, "created_by": createdBy,
		"created_at": createdAt, "run_count": runCount,
	})
}

func (a *API) handleDeleteDatasetProfile(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	res, err := a.db.Exec(`DELETE FROM dataset_profiles WHERE id = ?`, id)
	if err != nil {
		jsonError(w, "delete failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "profile not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) handleRunDatasetProfile(w http.ResponseWriter, r *http.Request) {
	profileID := r.PathValue("id")

	// Verify profile exists
	var name string
	if err := a.db.QueryRow(`SELECT name FROM dataset_profiles WHERE id = ?`, profileID).Scan(&name); err != nil {
		jsonError(w, "profile not found", http.StatusNotFound)
		return
	}

	runID := db.NewID()
	_, _ = a.db.Exec(`INSERT INTO dataset_runs (id, profile_id, status) VALUES (?, ?, 'running')`, runID, profileID)

	// Count matching nodes (simplified: all nodes for now)
	var nodeCount int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&nodeCount)

	// Mark completed immediately for sync export
	_, _ = a.db.Exec(`UPDATE dataset_runs SET status = 'completed', row_count = ?, completed_at = datetime('now') WHERE id = ?`,
		nodeCount, runID)

	jsonResp(w, http.StatusAccepted, map[string]interface{}{
		"run_id":     runID,
		"profile_id": profileID,
		"status":     "completed",
		"row_count":  nodeCount,
	})
}

func (a *API) handleGetDatasetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row := a.db.QueryRow(`SELECT id, profile_id, status, COALESCE(row_count,0), COALESCE(file_size,0), created_at, completed_at
		FROM dataset_runs WHERE id = ?`, id)

	var profileID, status, createdAt string
	var rowCount, fileSize int
	var completedAt *string
	if err := row.Scan(&id, &profileID, &status, &rowCount, &fileSize, &createdAt, &completedAt); err != nil {
		jsonError(w, "run not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"id": id, "profile_id": profileID, "status": status,
		"row_count": rowCount, "file_size": fileSize,
		"created_at": createdAt, "completed_at": completedAt,
	})
}

// --- Specialized exports ---

func (a *API) handleExportPreferences(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, question_id, chosen_node_id, rejected_node_id,
		signal_source, COALESCE(chosen_model,''), COALESCE(rejected_model,''), COALESCE(margin,0), created_at
		FROM preference_pairs ORDER BY created_at DESC LIMIT 1000`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type pair struct {
		ID             string  `json:"id"`
		QuestionID     string  `json:"question_id"`
		ChosenNodeID   string  `json:"chosen_node_id"`
		RejectedNodeID string  `json:"rejected_node_id"`
		SignalSource   string  `json:"signal_source"`
		ChosenModel    string  `json:"chosen_model,omitempty"`
		RejectedModel  string  `json:"rejected_model,omitempty"`
		Margin         float64 `json:"margin"`
		CreatedAt      string  `json:"created_at"`
	}

	var pairs []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.ID, &p.QuestionID, &p.ChosenNodeID, &p.RejectedNodeID,
			&p.SignalSource, &p.ChosenModel, &p.RejectedModel, &p.Margin, &p.CreatedAt) == nil {
			pairs = append(pairs, p)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{"pairs": pairs, "count": len(pairs)})
}

func (a *API) handleExportAdversarial(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT c.id, c.node_id, c.flow_name, c.status, c.score, COALESCE(c.summary,''),
		c.created_at, COALESCE(c.completed_at,'')
		FROM challenges c WHERE c.status = 'completed' ORDER BY c.created_at DESC LIMIT 500`)
	if err != nil {
		slog.Error("export adversarial failed", "error", err)
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type challenge struct {
		ID          string   `json:"id"`
		NodeID      string   `json:"node_id"`
		FlowName    string   `json:"flow_name"`
		Status      string   `json:"status"`
		Score       *float64 `json:"score"`
		Summary     string   `json:"summary"`
		CreatedAt   string   `json:"created_at"`
		CompletedAt string   `json:"completed_at"`
	}

	var challenges []challenge
	for rows.Next() {
		var c challenge
		if rows.Scan(&c.ID, &c.NodeID, &c.FlowName, &c.Status, &c.Score, &c.Summary,
			&c.CreatedAt, &c.CompletedAt) == nil {
			challenges = append(challenges, c)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{"challenges": challenges, "count": len(challenges)})
}

func (a *API) handleExportModeration(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, node_id, evaluator, eval_source,
		factual_score, source_score, argument_score, civility_score, overall_score,
		flags, COALESCE(notes,''), COALESCE(challenge_id,''), created_at
		FROM moderation_scores ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type modScore struct {
		ID            string   `json:"id"`
		NodeID        string   `json:"node_id"`
		Evaluator     string   `json:"evaluator"`
		EvalSource    string   `json:"eval_source"`
		FactualScore  *float64 `json:"factual_score"`
		SourceScore   *float64 `json:"source_score"`
		ArgumentScore *float64 `json:"argument_score"`
		CivilityScore *float64 `json:"civility_score"`
		OverallScore  *float64 `json:"overall_score"`
		Flags         string   `json:"flags"`
		Notes         string   `json:"notes,omitempty"`
		ChallengeID   string   `json:"challenge_id,omitempty"`
		CreatedAt     string   `json:"created_at"`
	}

	var scores []modScore
	for rows.Next() {
		var s modScore
		if rows.Scan(&s.ID, &s.NodeID, &s.Evaluator, &s.EvalSource,
			&s.FactualScore, &s.SourceScore, &s.ArgumentScore, &s.CivilityScore, &s.OverallScore,
			&s.Flags, &s.Notes, &s.ChallengeID, &s.CreatedAt) == nil {
			scores = append(scores, s)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{"scores": scores, "count": len(scores)})
}
