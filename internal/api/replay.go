// CLAUDE:SUMMARY Replay API — re-run individual or bulk LLM flow steps with different models and compare diffs
package api

import (
	"encoding/json"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
)

func (a *API) RegisterReplayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/replay/step/{stepID}", a.handleReplayStep)
	mux.HandleFunc("POST /api/replay/bulk", a.handleReplayBulk)
	mux.HandleFunc("GET /api/replay/diff/{originalID}/{replayID}", a.handleReplayDiff)
}

func (a *API) handleReplayStep(w http.ResponseWriter, r *http.Request) {
	if a.replayEngine == nil {
		jsonError(w, "replay engine not configured", http.StatusServiceUnavailable)
		return
	}

	stepID := r.PathValue("stepID")
	if stepID == "" {
		jsonError(w, "step ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		ModelID  string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ModelID == "" {
		jsonError(w, "model_id is required", http.StatusBadRequest)
		return
	}

	result, err := a.replayEngine.ReplayStep(r.Context(), stepID, req.Provider, req.ModelID)
	if err != nil {
		jsonError(w, "replay failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, result)
}

func (a *API) handleReplayBulk(w http.ResponseWriter, r *http.Request) {
	if a.replayEngine == nil {
		jsonError(w, "replay engine not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		FilterModel  string `json:"filter_model"`
		ReplayModel  string `json:"replay_model"`
		Provider     string `json:"provider"`
		FilterTag    string `json:"filter_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ReplayModel == "" {
		jsonError(w, "replay_model is required", http.StatusBadRequest)
		return
	}

	batchID := db.NewID()

	// Run async
	go func() {
		a.replayEngine.ReplayBulk(r.Context(), batchID, req.FilterModel, req.Provider, req.ReplayModel, req.FilterTag)
	}()

	jsonResp(w, http.StatusAccepted, map[string]string{
		"batch_id": batchID,
		"status":   "running",
	})
}

func (a *API) handleReplayDiff(w http.ResponseWriter, r *http.Request) {
	if a.flowsDB == nil {
		jsonError(w, "flows database not configured", http.StatusServiceUnavailable)
		return
	}

	originalID := r.PathValue("originalID")
	replayID := r.PathValue("replayID")

	type stepInfo struct {
		ID           string  `json:"id"`
		ModelID      string  `json:"model_id"`
		Provider     string  `json:"provider"`
		Content      string  `json:"content"`
		TokensIn     int     `json:"tokens_in"`
		TokensOut    int     `json:"tokens_out"`
		LatencyMs    int     `json:"latency_ms"`
		EvalScore    *float64 `json:"eval_score,omitempty"`
	}

	loadStep := func(id string) (*stepInfo, error) {
		row := a.flowsDB.QueryRow(`
			SELECT id, model_id, provider, COALESCE(response_raw,''), COALESCE(tokens_in,0), COALESCE(tokens_out,0), COALESCE(latency_ms,0), eval_score
			FROM flow_steps WHERE id = ?`, id)
		var s stepInfo
		err := row.Scan(&s.ID, &s.ModelID, &s.Provider, &s.Content, &s.TokensIn, &s.TokensOut, &s.LatencyMs, &s.EvalScore)
		return &s, err
	}

	orig, err := loadStep(originalID)
	if err != nil {
		jsonError(w, "original step not found", http.StatusNotFound)
		return
	}
	replay, err := loadStep(replayID)
	if err != nil {
		jsonError(w, "replay step not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"original": orig,
		"replay":   replay,
		"delta_tokens": map[string]int{
			"tokens_in":  replay.TokensIn - orig.TokensIn,
			"tokens_out": replay.TokensOut - orig.TokensOut,
		},
		"delta_latency_ms": replay.LatencyMs - orig.LatencyMs,
	})
}
