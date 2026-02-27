// CLAUDE:SUMMARY Resolution API — generate LLM resolutions for proof trees, batch resolution, render, and model listing
package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

// BatchResolutionRateLimiter is the rate limiter for POST /api/resolution/batch (10 req/3600s).
var BatchResolutionRateLimiter = NewRateLimiter(10, 3600*time.Second)

// RegisterResolutionRoutes adds Resolution-related API endpoints.
func (a *API) RegisterResolutionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/resolution/batch", RateLimitMiddleware(BatchResolutionRateLimiter, a.handleBatchResolution))
	mux.HandleFunc("POST /api/resolution/{id}", a.handleGenerateResolution)
	mux.HandleFunc("GET /api/resolution/{id}/unresolved", a.handleGetUnresolved)
	mux.HandleFunc("GET /api/resolution/{id}/models", a.handleGetResolutionModels)
	mux.HandleFunc("GET /api/resolution/{id}", a.handleGetResolution)
	mux.HandleFunc("POST /api/render/{id}", a.handleRenderResolution)
	mux.HandleFunc("GET /api/renders/{id}", a.handleGetRenders)
}

// SetResolutionEngine injects the LLM resolution engine.
func (a *API) SetResolutionEngine(engine *llm.ResolutionEngine) {
	a.resEngine = engine
}

// SetChallengeRunner injects the adversarial challenge runner.
func (a *API) SetChallengeRunner(runner *llm.ChallengeRunner) {
	a.challengeRunner = runner
}

func (a *API) handleGenerateResolution(w http.ResponseWriter, r *http.Request) {
	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured — resolution generation unavailable", http.StatusServiceUnavailable)
		return
	}

	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Get the full tree
	tree, err := a.db.GetTree(nodeID, 100)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "node not found", http.StatusNotFound)
			return
		}
		slog.Error("getting tree", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate resolution
	result, err := a.resEngine.GenerateResolution(r.Context(), tree, req.Provider, req.Model)
	if err != nil {
		slog.Error("generating resolution", "error", err)
		jsonError(w, "resolution generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store as a claim node with is_resolution metadata
	resNode, err := a.db.CreateNode(db.CreateNodeInput{
		ParentID: &nodeID,
		NodeType: "claim",
		Body:     result.Content,
		AuthorID: claims.UserID,
		ModelID:  &result.Model,
		Metadata: mustJSON(map[string]interface{}{
			"is_resolution": true,
			"provider":      result.Provider,
			"tokens_in":     result.TokensIn,
			"tokens_out":    result.TokensOut,
			"latency_ms":    result.LatencyMs,
		}),
	})
	if err != nil {
		slog.Error("storing resolution", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Upsert into resolutions table for tracking per provider/model
	a.db.Exec(`INSERT INTO resolutions (id, node_id, provider, model, content, tokens_in, tokens_out, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, provider, model)
		DO UPDATE SET content=excluded.content, tokens_in=excluded.tokens_in, tokens_out=excluded.tokens_out, latency_ms=excluded.latency_ms, updated_at=datetime('now')`,
		db.NewID(), nodeID, result.Provider, result.Model, result.Content, result.TokensIn, result.TokensOut, result.LatencyMs)

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"resolution": resNode,
		"generation": result,
	})
}

func (a *API) handleGetResolution(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	// Find resolution nodes for this tree
	nodes, err := a.db.GetNodesByRoot(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var resolutions []*db.Node
	for _, n := range nodes {
		if strings.Contains(n.Metadata, `"is_resolution"`) {
			resolutions = append(resolutions, n)
		}
	}

	if len(resolutions) == 0 {
		jsonError(w, "no resolution found for this tree", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"resolutions": resolutions,
		"count":       len(resolutions),
	})
}

func (a *API) handleRenderResolution(w http.ResponseWriter, r *http.Request) {
	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured", http.StatusServiceUnavailable)
		return
	}

	resolutionID := r.PathValue("id")
	if resolutionID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Format   string `json:"format"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Format == "" {
		jsonError(w, "format is required (article, faq, thread, summary)", http.StatusBadRequest)
		return
	}

	// Get the resolution node
	resNode, err := a.db.GetNode(resolutionID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "resolution not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Render
	result, err := a.resEngine.RenderResolution(r.Context(), resNode.Body, req.Format, req.Provider, req.Model)
	if err != nil {
		slog.Error("rendering resolution", "error", err)
		jsonError(w, "render failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store in renders table
	renderID := db.NewID()
	a.db.Exec(`INSERT INTO renders (id, resolution_id, format, model_id, content, fidelity_score)
		VALUES (?, ?, ?, ?, ?, NULL)`, renderID, resolutionID, req.Format, result.Model, result.Content)

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"render_id": renderID,
		"render":    result,
	})
}

func (a *API) handleGetRenders(w http.ResponseWriter, r *http.Request) {
	resolutionID := r.PathValue("id")
	if resolutionID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	rows, err := a.db.Query(`
		SELECT id, resolution_id, format, model_id, content, fidelity_score, created_at
		FROM renders WHERE resolution_id = ?
		ORDER BY created_at DESC`, resolutionID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Render struct {
		ID            string  `json:"id"`
		ResolutionID  string  `json:"resolution_id"`
		Format        string  `json:"format"`
		ModelID       *string `json:"model_id,omitempty"`
		Content       *string `json:"content,omitempty"`
		FidelityScore *int    `json:"fidelity_score,omitempty"`
		CreatedAt     string  `json:"created_at"`
	}

	var renders []Render
	for rows.Next() {
		var rd Render
		var modelID, content sql.NullString
		var score sql.NullInt64
		if err := rows.Scan(&rd.ID, &rd.ResolutionID, &rd.Format, &modelID, &content, &score, &rd.CreatedAt); err != nil {
			continue
		}
		if modelID.Valid {
			rd.ModelID = &modelID.String
		}
		if content.Valid {
			rd.Content = &content.String
		}
		if score.Valid {
			s := int(score.Int64)
			rd.FidelityScore = &s
		}
		renders = append(renders, rd)
	}

	jsonResp(w, http.StatusOK, renders)
}

func (a *API) handleGetResolutionModels(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	rows, err := a.db.Query(`
		SELECT provider, model, COUNT(*) as cnt
		FROM resolutions
		WHERE node_id = ?
		GROUP BY provider, model
		ORDER BY provider, model`, nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	models := make(map[string]interface{})
	for rows.Next() {
		var provider, model string
		var cnt int
		if err := rows.Scan(&provider, &model, &cnt); err != nil {
			continue
		}
		key := provider
		if model != "" {
			key += "/" + model
		}
		if key == "" {
			key = "default"
		}
		models[key] = cnt
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"count":  len(models),
		"models": models,
	})
}

func (a *API) handleGetUnresolved(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")
	if provider == "" || model == "" {
		jsonError(w, "provider and model query parameters are required", http.StatusBadRequest)
		return
	}

	// Get all child nodes of this tree
	allNodes, err := a.db.GetNodesByRoot(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Filter: nodes that don't have a resolution for this provider/model
	var unresolved []*db.Node
	for _, n := range allNodes {
		if strings.Contains(n.Metadata, `"is_resolution"`) {
			continue
		}
		var count int
		a.db.QueryRow("SELECT COUNT(*) FROM resolutions WHERE node_id = ? AND provider = ? AND model = ?",
			n.ID, provider, model).Scan(&count)
		if count == 0 {
			unresolved = append(unresolved, n)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"nodes": unresolved,
		"count": len(unresolved),
	})
}

func (a *API) handleBatchResolution(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024) // 64KB max for batch payload

	var req struct {
		NodeIDs  []string `json:"node_ids"`
		Provider string   `json:"provider"`
		Model    string   `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(err.Error(), "too large") || strings.Contains(err.Error(), "http: request body too large") {
			jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.NodeIDs) == 0 {
		jsonError(w, "node_ids is required and must not be empty", http.StatusBadRequest)
		return
	}
	if len(req.NodeIDs) > 50 {
		jsonError(w, "maximum 50 nodes per batch", http.StatusBadRequest)
		return
	}

	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured — resolution generation unavailable", http.StatusServiceUnavailable)
		return
	}

	var succeeded, failed int
	var results []map[string]interface{}

	for _, nodeID := range req.NodeIDs {
		tree, err := a.db.GetTree(nodeID, 100)
		if err != nil {
			failed++
			results = append(results, map[string]interface{}{
				"node_id": nodeID,
				"status":  "failed",
				"error":   "node not found",
			})
			continue
		}

		result, err := a.resEngine.GenerateResolution(r.Context(), tree, req.Provider, req.Model)
		if err != nil {
			failed++
			results = append(results, map[string]interface{}{
				"node_id": nodeID,
				"status":  "failed",
				"error":   err.Error(),
			})
			continue
		}

		// Store resolution as claim node with is_resolution metadata
		_, storeErr := a.db.CreateNode(db.CreateNodeInput{
			ParentID: &nodeID,
			NodeType: "claim",
			Body:     result.Content,
			AuthorID: claims.UserID,
			ModelID:  &result.Model,
			Metadata: mustJSON(map[string]interface{}{
				"is_resolution": true,
				"provider":      result.Provider,
				"tokens_in":     result.TokensIn,
				"tokens_out":    result.TokensOut,
				"latency_ms":    result.LatencyMs,
			}),
		})
		if storeErr != nil {
			slog.Error("storing batch resolution", "error", storeErr)
		}

		// Upsert into resolutions table
		a.db.Exec(`INSERT INTO resolutions (id, node_id, provider, model, content, tokens_in, tokens_out, latency_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_id, provider, model)
			DO UPDATE SET content=excluded.content, tokens_in=excluded.tokens_in, tokens_out=excluded.tokens_out, latency_ms=excluded.latency_ms, updated_at=datetime('now')`,
			db.NewID(), nodeID, result.Provider, result.Model, result.Content, result.TokensIn, result.TokensOut, result.LatencyMs)

		succeeded++
		results = append(results, map[string]interface{}{
			"node_id": nodeID,
			"status":  "completed",
		})
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"total":     len(req.NodeIDs),
		"succeeded": succeeded,
		"failed":    failed,
		"results":   results,
	})
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
