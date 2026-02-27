// CLAUDE:SUMMARY Adversarial challenge API endpoints — create/run challenges, moderation scores, leaderboard, flow listing
package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/hazyhaar/horostracker/internal/llm"
)

// RegisterChallengeRoutes adds adversarial challenge API endpoints.
func (a *API) RegisterChallengeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/challenge/{nodeID}", a.handleCreateChallenge)
	mux.HandleFunc("POST /api/challenge/{id}/run", a.handleRunChallenge)
	mux.HandleFunc("GET /api/challenges/{nodeID}", a.handleGetChallenges)
	mux.HandleFunc("GET /api/challenge/{id}", a.handleGetChallenge)
	mux.HandleFunc("GET /api/moderation/{nodeID}", a.handleGetModeration)
	mux.HandleFunc("GET /api/leaderboard/adversarial", a.handleChallengeLeaderboard)
	mux.HandleFunc("GET /api/flows", a.handleListFlows)
}

func (a *API) handleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		jsonError(w, "nodeID is required", http.StatusBadRequest)
		return
	}

	var req struct {
		FlowName       string  `json:"flow_name"`
		TargetProvider *string `json:"target_provider"`
		TargetModel    *string `json:"target_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.FlowName == "" {
		jsonError(w, "flow_name is required", http.StatusBadRequest)
		return
	}
	if !llm.IsValidFlow(req.FlowName) {
		jsonError(w, "invalid flow_name — valid: "+joinFlows(), http.StatusBadRequest)
		return
	}

	// Verify node exists
	if _, err := a.db.GetNode(nodeID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "node not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	challenge, err := a.db.CreateChallenge(nodeID, req.FlowName, claims.UserID, req.TargetProvider, req.TargetModel)
	if err != nil {
		slog.Error("creating challenge", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, challenge)
}

func (a *API) handleRunChallenge(w http.ResponseWriter, r *http.Request) {
	if a.challengeRunner == nil {
		jsonError(w, "no LLM providers configured — challenges unavailable", http.StatusServiceUnavailable)
		return
	}

	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	challengeID := r.PathValue("id")
	if challengeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	challenge, err := a.db.GetChallenge(challengeID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "challenge not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if challenge.Status != "pending" {
		jsonError(w, "challenge already "+challenge.Status, http.StatusConflict)
		return
	}

	// Only the challenge creator can run it
	if challenge.RequestedBy != claims.UserID {
		jsonError(w, "only the challenge creator can run it", http.StatusForbidden)
		return
	}

	result, err := a.challengeRunner.RunChallenge(r.Context(), challenge)
	if err != nil {
		slog.Error("running challenge", "error", err)
		jsonError(w, "challenge execution failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, result)
}

func (a *API) handleGetChallenges(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		jsonError(w, "nodeID is required", http.StatusBadRequest)
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	challenges, err := a.db.GetChallengesForNode(nodeID, limit)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"challenges": challenges,
		"count":      len(challenges),
	})
}

func (a *API) handleGetChallenge(w http.ResponseWriter, r *http.Request) {
	challengeID := r.PathValue("id")
	if challengeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	challenge, err := a.db.GetChallenge(challengeID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "challenge not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, challenge)
}

func (a *API) handleGetModeration(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		jsonError(w, "nodeID is required", http.StatusBadRequest)
		return
	}

	scores, err := a.db.GetModerationScores(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"scores": scores,
		"count":  len(scores),
	})
}

func (a *API) handleChallengeLeaderboard(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	entries, err := a.db.GetChallengeLeaderboard(limit)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"leaderboard": entries,
		"count":       len(entries),
	})
}

func (a *API) handleListFlows(w http.ResponseWriter, r *http.Request) {
	flows := llm.CoreFlows()
	type flowInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		StepCount   int    `json:"step_count"`
	}
	var result []flowInfo
	for _, f := range flows {
		result = append(result, flowInfo{
			Name:        f.Name,
			Description: f.Description,
			StepCount:   len(f.Steps),
		})
	}
	jsonResp(w, http.StatusOK, result)
}

func joinFlows() string {
	flows := llm.ValidFlows()
	result := ""
	for i, f := range flows {
		if i > 0 {
			result += ", "
		}
		result += f
	}
	return result
}
