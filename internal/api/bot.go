package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
)

// RegisterBotRoutes adds bot-related API endpoints.
func (a *API) RegisterBotRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/bot/answer/{nodeID}", a.handleBotAnswer)
	mux.HandleFunc("GET /api/bot/status", a.handleBotStatus)
}

// handleBotAnswer triggers the bot to generate an LLM answer for a node.
func (a *API) handleBotAnswer(w http.ResponseWriter, r *http.Request) {
	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured — bot unavailable", http.StatusServiceUnavailable)
		return
	}
	if a.botUserID == "" {
		jsonError(w, "bot not configured", http.StatusServiceUnavailable)
		return
	}

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
		Provider string `json:"provider"`
		Model    string `json:"model"`
		FlowName string `json:"flow_name"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Get tree for context
	tree, err := a.db.GetTree(nodeID, 50)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}

	// Debit bot credits (1 credit per answer)
	if err := a.db.DebitCredits(a.botUserID, 1, "bot_answer", "node", nodeID); err != nil {
		jsonError(w, "bot credit limit reached", http.StatusTooManyRequests)
		return
	}

	// If a specific flow is requested, run it via challenge runner
	if req.FlowName != "" && a.challengeRunner != nil {
		challenge, err := a.db.CreateChallenge(nodeID, req.FlowName, a.botUserID, &req.Provider, &req.Model)
		if err != nil {
			jsonError(w, "internal error", http.StatusInternalServerError)
			return
		}
		result, err := a.challengeRunner.RunChallenge(r.Context(), challenge)
		if err != nil {
			log.Printf("bot challenge error: %v", err)
			jsonError(w, "challenge failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, http.StatusCreated, map[string]interface{}{
			"type":      "challenge",
			"challenge": result,
		})
		return
	}

	// Default: generate a synthesis answer
	result, err := a.resEngine.GenerateResolution(r.Context(), tree, req.Provider, req.Model)
	if err != nil {
		log.Printf("bot answer error: %v", err)
		jsonError(w, "generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store as an LLM node
	modelID := result.Model
	node, err := a.db.CreateNode(db.CreateNodeInput{
		ParentID: &nodeID,
		NodeType: "llm",
		Body:     result.Content,
		AuthorID: a.botUserID,
		ModelID:  &modelID,
		Metadata: mustJSON(map[string]interface{}{
			"provider":   result.Provider,
			"tokens_in":  result.TokensIn,
			"tokens_out": result.TokensOut,
			"latency_ms": result.LatencyMs,
			"bot":        true,
		}),
	})
	if err != nil {
		log.Printf("bot node creation error: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"type":       "answer",
		"node":       node,
		"generation": result,
	})
}

func (a *API) handleBotStatus(w http.ResponseWriter, r *http.Request) {
	if a.botUserID == "" {
		jsonResp(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
		})
		return
	}

	user, err := a.db.GetUserByID(a.botUserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	hasLLM := a.resEngine != nil

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
		"handle":  user.Handle,
		"credits": user.Credits,
		"has_llm": hasLLM,
	})
}
